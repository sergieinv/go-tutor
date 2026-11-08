// Package mysql — хранилище заказов на MySQL: database/sql с драйвером
// go-sql-driver/mysql и код, сгенерированный sqlc (пакет sqlcgen,
// запросы — query.sql).
//
// Отличия от PostgreSQL, которые видны в этом пакете и в query.sql:
//   - нет RETURNING: id новой строки — из LastInsertId (:execlastid в sqlc);
//   - плейсхолдеры только «?»: параметр, нужный в запросе дважды, передаётся
//     дважды (посмотрите ReserveStock в sqlcgen/query.sql.go);
//   - нет параметров-массивов: sqlc.slice разворачивается в IN (?, ?, …);
//   - время — DATETIME без зоны: драйвер должен разбирать его в time.Time
//     (ParseTime) и в одной зоне с записью (Loc = UTC), это задаёт Open;
//   - RowsAffected у UPDATE по умолчанию считает изменённые строки, а не
//     найденные (флаг clientFoundRows меняет это). Наши UPDATE всегда меняют
//     найденную строку, поэтому разницы нет, но о ней стоит помнить.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/mysql/sqlcgen"
	"gotutor/labs/orders/migrations"
)

// Store реализует orders.Store.
type Store struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

var _ orders.Store = (*Store)(nil)

// Open открывает пул соединений и проверяет связь с базой.
// dsn — в формате драйвера: user:pass@tcp(host:3306)/db.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn: %w", err)
	}
	// Без ParseTime драйвер отдаёт DATETIME как []byte, и Scan в time.Time
	// падает. Loc — зона, в которой драйвер пишет и читает DATETIME.
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("mysql connector: %w", err)
	}
	db := sql.OpenDB(connector)
	// Настройки пула database/sql. ConnMaxLifetime меньше wait_timeout
	// сервера (по умолчанию 8 часов) и таймаутов балансировщиков между
	// сервисом и базой: соединение закрываем раньше, чем его оборвут.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return &Store{db: db, q: sqlcgen.New(db)}, nil
}

// Close закрывает пул.
func (s *Store) Close() { s.db.Close() }

// Migrate применяет миграции goose из migrations/mysql.
//
// В отличие от PostgreSQL, у goose нет готовой блокировки для MySQL: если
// реплик несколько, миграции лучше запускать отдельным шагом деплоя.
func (s *Store) Migrate(ctx context.Context) error {
	fsys, err := fs.Sub(migrations.FS, "mysql")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectMySQL, s.db, fsys)
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate mysql: %w", err)
	}
	return nil
}

// Ping проверяет связь с базой.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Begin открывает транзакцию с уровнем изоляции по умолчанию (в InnoDB —
// REPEATABLE READ). UPDATE и SELECT … FOR UPDATE в InnoDB читают последнюю
// закоммиченную версию строки («текущее чтение»), поэтому резервирование
// и отмена корректны и на этом уровне.
//
// Транзакция database/sql привязана к ctx: при его отмене драйвер сам
// откатит её.
func (s *Store) Begin(ctx context.Context) (orders.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	return &Tx{tx: tx, q: s.q.WithTx(tx)}, nil
}

// Order читает заказ и его позиции.
func (s *Store) Order(ctx context.Context, id int64) (orders.Order, error) {
	row, err := s.q.GetOrder(ctx, id)
	if err != nil {
		return orders.Order{}, notFound(err, "order", id)
	}
	return withItems(ctx, s.q, row)
}

// Product читает товар с остатком.
func (s *Store) Product(ctx context.Context, id int64) (orders.Product, error) {
	row, err := s.q.GetProduct(ctx, id)
	if err != nil {
		return orders.Product{}, notFound(err, "product", id)
	}
	return orders.Product{ID: row.ID, Name: row.Name, PriceCents: row.PriceCents, Available: int(row.Available)}, nil
}

// CreateProduct добавляет товар и остаток в одной транзакции.
func (s *Store) CreateProduct(ctx context.Context, p orders.NewProduct) (orders.Product, error) {
	if p.Available < 0 || p.Available > math.MaxInt32 {
		return orders.Product{}, &orders.ValidationError{Field: "available", Message: "out of range"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return orders.Product{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)
	// RETURNING нет: INSERT, затем LastInsertId из ответа сервера.
	id, err := q.CreateProduct(ctx, sqlcgen.CreateProductParams{Name: p.Name, PriceCents: p.PriceCents})
	if err != nil {
		return orders.Product{}, fmt.Errorf("insert product: %w", err)
	}
	if err := q.CreateStock(ctx, sqlcgen.CreateStockParams{ProductID: id, Available: int32(p.Available)}); err != nil {
		return orders.Product{}, fmt.Errorf("insert stock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return orders.Product{}, fmt.Errorf("commit: %w", err)
	}
	return orders.Product{ID: id, Name: p.Name, PriceCents: p.PriceCents, Available: p.Available}, nil
}

// PendingEvents возвращает неопубликованные события outbox.
func (s *Store) PendingEvents(ctx context.Context, limit int) ([]orders.Event, error) {
	rows, err := s.q.ListPendingEvents(ctx, int32(min(limit, math.MaxInt32)))
	if err != nil {
		return nil, fmt.Errorf("list pending events: %w", err)
	}
	events := make([]orders.Event, len(rows))
	for i, r := range rows {
		events[i] = orders.Event{
			ID:        r.ID,
			Type:      r.EventType,
			OrderID:   r.AggregateID,
			Payload:   r.Payload,
			CreatedAt: r.CreatedAt,
		}
	}
	return events, nil
}

// Tx реализует orders.Tx поверх *sql.Tx.
type Tx struct {
	tx *sql.Tx
	q  *sqlcgen.Queries
}

// Commit фиксирует транзакцию. ctx не нужен: *sql.Tx уже привязана к
// контексту из BeginTx.
func (t *Tx) Commit(context.Context) error { return t.tx.Commit() }

// Rollback откатывает транзакцию; после Commit возвращает sql.ErrTxDone.
func (t *Tx) Rollback(context.Context) error { return t.tx.Rollback() }

// ProductsByIDs читает товары одним запросом WHERE id IN (?, ?, …).
func (t *Tx) ProductsByIDs(ctx context.Context, ids []int64) ([]orders.Product, error) {
	rows, err := t.q.ListProductsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	products := make([]orders.Product, len(rows))
	for i, r := range rows {
		products[i] = orders.Product{ID: r.ID, Name: r.Name, PriceCents: r.PriceCents, Available: int(r.Available)}
	}
	return products, nil
}

// ReserveStock списывает остаток атомарным UPDATE … WHERE available >= ?.
// InnoDB берёт эксклюзивную блокировку строки, параллельные транзакции ждут
// и проверяют условие уже на новом значении.
func (t *Tx) ReserveStock(ctx context.Context, productID int64, qty int) error {
	n, err := t.q.ReserveStock(ctx, sqlcgen.ReserveStockParams{Quantity: int32(qty), ProductID: productID})
	if err != nil {
		return fmt.Errorf("reserve stock: %w", err)
	}
	if n == 0 {
		return orders.ErrOutOfStock
	}
	return nil
}

// ReleaseStock возвращает товар на склад.
func (t *Tx) ReleaseStock(ctx context.Context, productID int64, qty int) error {
	n, err := t.q.ReleaseStock(ctx, sqlcgen.ReleaseStockParams{Quantity: int32(qty), ProductID: productID})
	if err != nil {
		return fmt.Errorf("release stock: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("stock of product %d: %w", productID, orders.ErrNotFound)
	}
	return nil
}

// CreateOrder вставляет заказ (id — из LastInsertId) и его позиции.
func (t *Tx) CreateOrder(ctx context.Context, o orders.Order) (int64, error) {
	id, err := t.q.CreateOrder(ctx, sqlcgen.CreateOrderParams{
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt,
		// Именованных параметров нет: одно и то же время — два «?».
		UpdatedAt: o.CreatedAt,
	})
	if err != nil {
		return 0, fmt.Errorf("insert order: %w", err)
	}
	for _, it := range o.Items {
		err := t.q.CreateOrderItem(ctx, sqlcgen.CreateOrderItemParams{
			OrderID:    id,
			ProductID:  it.ProductID,
			Quantity:   int32(it.Quantity),
			PriceCents: it.PriceCents,
		})
		if err != nil {
			return 0, fmt.Errorf("insert order item: %w", err)
		}
	}
	return id, nil
}

// LockOrder читает заказ с блокировкой строки (SELECT … FOR UPDATE).
func (t *Tx) LockOrder(ctx context.Context, id int64) (orders.Order, error) {
	row, err := t.q.GetOrderForUpdate(ctx, id)
	if err != nil {
		return orders.Order{}, notFound(err, "order", id)
	}
	return withItems(ctx, t.q, row)
}

// UpdateOrderStatus меняет статус с проверкой старого значения.
func (t *Tx) UpdateOrderStatus(ctx context.Context, id int64, from, to orders.Status, at time.Time) error {
	n, err := t.q.UpdateOrderStatus(ctx, sqlcgen.UpdateOrderStatusParams{
		NewStatus: string(to),
		UpdatedAt: at,
		ID:        id,
		OldStatus: string(from),
	})
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("order %d is not in status %q: %w", id, from, orders.ErrConflict)
	}
	return nil
}

// AddEvent пишет событие в outbox.
func (t *Tx) AddEvent(ctx context.Context, e orders.Event) error {
	err := t.q.CreateOutboxEvent(ctx, sqlcgen.CreateOutboxEventParams{
		EventType:   e.Type,
		AggregateID: e.OrderID,
		Payload:     e.Payload,
		CreatedAt:   e.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// withItems дочитывает позиции заказа одним запросом.
func withItems(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Order) (orders.Order, error) {
	rows, err := q.ListOrderItems(ctx, row.ID)
	if err != nil {
		return orders.Order{}, fmt.Errorf("list order items: %w", err)
	}
	o := orders.Order{
		ID:         row.ID,
		CustomerID: row.CustomerID,
		Status:     orders.Status(row.Status),
		Items:      make([]orders.Item, len(rows)),
		TotalCents: row.TotalCents,
		CreatedAt:  row.CreatedAt, // уже UTC: Loc в Open
		UpdatedAt:  row.UpdatedAt,
	}
	for i, r := range rows {
		o.Items[i] = orders.Item{ProductID: r.ProductID, Quantity: int(r.Quantity), PriceCents: r.PriceCents}
	}
	return o, nil
}

// notFound переводит sql.ErrNoRows в доменную ErrNotFound.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %d: %w", what, id, orders.ErrNotFound)
	}
	return fmt.Errorf("get %s %d: %w", what, id, err)
}
