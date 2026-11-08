// Package sqlite — хранилище заказов на SQLite: database/sql с драйвером
// modernc.org/sqlite (чистый Go, без cgo) и код, сгенерированный sqlc
// (пакет sqlcgen, запросы — query.sql).
//
// Отличия от PostgreSQL и MySQL, которые видны здесь и в query.sql:
//   - RETURNING есть (с SQLite 3.35), как в PostgreSQL;
//   - нет параметров-массивов: sqlc.slice разворачивается в IN (?, ?, …);
//   - нет SELECT … FOR UPDATE и блокировок строк: писать в базу в каждый
//     момент может только одна транзакция. Транзакции начинаются с
//     BEGIN IMMEDIATE (_txlock=immediate): блокировка на запись берётся
//     сразу, а не при первом UPDATE, — иначе две транзакции, начавшие
//     с чтения, упираются друг в друга и одна получает SQLITE_BUSY;
//   - внешние ключи выключены, пока не выполнен PRAGMA foreign_keys = ON;
//     PRAGMA действует на соединение, поэтому задаётся в DSN и выполняется
//     для каждого нового соединения пула;
//   - ожидание блокировки — busy_timeout: без него конкурирующая
//     транзакция сразу получает ошибку «database is locked».
//   - query.sql — только ASCII: парсер sqlite в sqlc v1.31 путает смещения
//     на многобайтных символах. Пояснения к запросам поэтому здесь.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // регистрирует драйвер "sqlite" в database/sql

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/sqlite/sqlcgen"
	"gotutor/labs/orders/migrations"
)

// Store реализует orders.Store.
type Store struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

var _ orders.Store = (*Store)(nil)

// Open открывает (или создаёт) базу в файле path и проверяет её.
//
// Принимает путь, а не DSN: параметры соединения, без которых хранилище
// работает неправильно (внешние ключи, busy_timeout, BEGIN IMMEDIATE),
// задаёт сам. База в памяти (":memory:") не поддерживается: у каждого
// соединения пула она была бы своя.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") || strings.Contains(path, ":memory:") {
		return nil, fmt.Errorf("sqlite: want a plain file path, got %q", path)
	}
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(10000)")
	// WAL: читатели не блокируют писателя и наоборот.
	params.Add("_pragma", "journal_mode(WAL)")
	params.Set("_txlock", "immediate")

	db, err := sql.Open("sqlite", "file:"+path+"?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &Store{db: db, q: sqlcgen.New(db)}, nil
}

// Close закрывает базу.
func (s *Store) Close() { s.db.Close() }

// Migrate применяет миграции goose из migrations/sqlite.
func (s *Store) Migrate(ctx context.Context) error {
	fsys, err := fs.Sub(migrations.FS, "sqlite")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, s.db, fsys)
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

// Ping проверяет связь с базой.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Begin открывает транзакцию (BEGIN IMMEDIATE — см. описание пакета).
// Уровень изоляции в SQLite фактически SERIALIZABLE.
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
	if p.Available < 0 {
		return orders.Product{}, &orders.ValidationError{Field: "available", Message: "out of range"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return orders.Product{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)
	// RETURNING id — как в PostgreSQL.
	id, err := q.CreateProduct(ctx, sqlcgen.CreateProductParams{Name: p.Name, PriceCents: p.PriceCents})
	if err != nil {
		return orders.Product{}, fmt.Errorf("insert product: %w", err)
	}
	if err := q.CreateStock(ctx, sqlcgen.CreateStockParams{ProductID: id, Available: int64(p.Available)}); err != nil {
		return orders.Product{}, fmt.Errorf("insert stock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return orders.Product{}, fmt.Errorf("commit: %w", err)
	}
	return orders.Product{ID: id, Name: p.Name, PriceCents: p.PriceCents, Available: p.Available}, nil
}

// PendingEvents возвращает неопубликованные события outbox.
func (s *Store) PendingEvents(ctx context.Context, limit int) ([]orders.Event, error) {
	rows, err := s.q.ListPendingEvents(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list pending events: %w", err)
	}
	events := make([]orders.Event, len(rows))
	for i, r := range rows {
		events[i] = orders.Event{
			ID:        r.ID,
			Type:      r.EventType,
			OrderID:   r.AggregateID,
			Payload:   []byte(r.Payload), // JSON в SQLite — TEXT
			CreatedAt: r.CreatedAt.UTC(),
		}
	}
	return events, nil
}

// Tx реализует orders.Tx поверх *sql.Tx.
type Tx struct {
	tx *sql.Tx
	q  *sqlcgen.Queries
}

// Commit фиксирует транзакцию.
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
// Именованный параметр quantity встречается в запросе дважды, но SQLite,
// в отличие от MySQL, принимает его один раз.
func (t *Tx) ReserveStock(ctx context.Context, productID int64, qty int) error {
	n, err := t.q.ReserveStock(ctx, sqlcgen.ReserveStockParams{Quantity: int64(qty), ProductID: productID})
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
	n, err := t.q.ReleaseStock(ctx, sqlcgen.ReleaseStockParams{Quantity: int64(qty), ProductID: productID})
	if err != nil {
		return fmt.Errorf("release stock: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("stock of product %d: %w", productID, orders.ErrNotFound)
	}
	return nil
}

// CreateOrder вставляет заказ (id — из RETURNING) и его позиции.
func (t *Tx) CreateOrder(ctx context.Context, o orders.Order) (int64, error) {
	id, err := t.q.CreateOrder(ctx, sqlcgen.CreateOrderParams{
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt,
	})
	if err != nil {
		return 0, fmt.Errorf("insert order: %w", err)
	}
	for _, it := range o.Items {
		err := t.q.CreateOrderItem(ctx, sqlcgen.CreateOrderItemParams{
			OrderID:    id,
			ProductID:  it.ProductID,
			Quantity:   int64(it.Quantity),
			PriceCents: it.PriceCents,
		})
		if err != nil {
			return 0, fmt.Errorf("insert order item: %w", err)
		}
	}
	return id, nil
}

// LockOrder читает заказ. FOR UPDATE в SQLite нет и не нужно: транзакция
// уже держит блокировку на запись всей базы (BEGIN IMMEDIATE), параллельная
// отмена ждёт в Begin.
func (t *Tx) LockOrder(ctx context.Context, id int64) (orders.Order, error) {
	row, err := t.q.GetOrder(ctx, id)
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
		Payload:     string(e.Payload),
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
		CreatedAt:  row.CreatedAt.UTC(),
		UpdatedAt:  row.UpdatedAt.UTC(),
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
