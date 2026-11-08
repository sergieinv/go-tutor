// Package postgres — хранилище заказов на PostgreSQL: пул pgxpool и код,
// сгенерированный sqlc (пакет sqlcgen, запросы — query.sql).
//
// pgx используется напрямую, без database/sql: так доступны нативные типы
// PostgreSQL (массивы, jsonb) и чуть меньше накладных расходов. database/sql
// нужен только goose для миграций — его даёт адаптер stdlib поверх того же пула.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/postgres/sqlcgen"
	"gotutor/labs/orders/migrations"
)

// Store реализует orders.Store.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

// Проверка на этапе компиляции: Store удовлетворяет интерфейсу домена.
var _ orders.Store = (*Store)(nil)

// Open создаёт пул соединений и проверяет связь с базой.
//
// dsn — URL (postgres://user:pass@host:5432/db?sslmode=disable) или строка
// key=value. Размер пула и время жизни соединений настраиваются там же:
// pool_max_conns, pool_max_conn_lifetime и т.д.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// pgx не включает пароль в текст ошибки разбора, но и мы dsn
		// в ошибку не добавляем.
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	// NewWithConfig соединений не открывает: проверяем базу сразу, чтобы
	// сервис с неверным адресом падал при старте, а не на первом запросе.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool, q: sqlcgen.New(pool)}, nil
}

// Close закрывает пул, дожидаясь возврата соединений.
func (s *Store) Close() { s.pool.Close() }

// Migrate применяет миграции goose из migrations/postgres.
//
// Advisory-блокировка (session locker) не даёт двум репликам сервиса,
// стартующим одновременно, применять миграции наперегонки.
func (s *Store) Migrate(ctx context.Context) error {
	fsys, err := fs.Sub(migrations.FS, "postgres")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close() // закрывает адаптер, а не пул
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate postgres: %w", err)
	}
	return nil
}

// Ping проверяет связь с базой.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Begin открывает транзакцию с уровнем изоляции по умолчанию (READ
// COMMITTED). Его достаточно: остатки меняются атомарным UPDATE с условием,
// заказ блокируется FOR UPDATE.
func (s *Store) Begin(ctx context.Context) (orders.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	return &Tx{tx: tx, q: s.q.WithTx(tx)}, nil
}

// Order читает заказ и его позиции — двумя запросами, без транзакции:
// позиции после создания заказа не меняются.
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

// CreateProduct добавляет товар и остаток в одной транзакции. pgx.BeginFunc
// делает Commit/Rollback сам — удобная форма для коротких транзакций внутри
// хранилища.
func (s *Store) CreateProduct(ctx context.Context, p orders.NewProduct) (orders.Product, error) {
	if p.Available < 0 || p.Available > math.MaxInt32 {
		return orders.Product{}, &orders.ValidationError{Field: "available", Message: "out of range"}
	}
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		// RETURNING id: PostgreSQL возвращает сгенерированный ключ прямо из INSERT.
		id, err = q.CreateProduct(ctx, sqlcgen.CreateProductParams{Name: p.Name, PriceCents: p.PriceCents})
		if err != nil {
			return err
		}
		return q.CreateStock(ctx, sqlcgen.CreateStockParams{ProductID: id, Available: int32(p.Available)})
	})
	if err != nil {
		return orders.Product{}, fmt.Errorf("create product: %w", err)
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
			CreatedAt: r.CreatedAt.UTC(),
		}
	}
	return events, nil
}

// Tx реализует orders.Tx поверх pgx.Tx.
type Tx struct {
	tx pgx.Tx
	q  *sqlcgen.Queries
}

// Commit фиксирует транзакцию.
func (t *Tx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

// Rollback откатывает транзакцию; после Commit возвращает pgx.ErrTxClosed.
func (t *Tx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// ProductsByIDs читает товары одним запросом: срез уходит в PostgreSQL
// одним параметром-массивом (WHERE id = ANY($1)).
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

// ReserveStock списывает остаток атомарным UPDATE … WHERE available >= qty.
// Параллельные транзакции на той же строке ждут блокировку, а затем
// PostgreSQL перепроверяет условие на новой версии строки — продать больше
// остатка невозможно.
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
	// Позиций не больше orders.MaxItems — по INSERT на позицию приемлемо.
	// Для тысяч строк в PostgreSQL есть COPY (в sqlc — аннотация :copyfrom).
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

// withItems дочитывает позиции заказа одним запросом и собирает доменный заказ.
// q — пул или транзакция: sqlc-код работает с обоими через интерфейс DBTX.
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
		// pgx возвращает timestamptz в локальной зоне процесса; домен
		// работает в UTC.
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
	for i, r := range rows {
		o.Items[i] = orders.Item{ProductID: r.ProductID, Quantity: int(r.Quantity), PriceCents: r.PriceCents}
	}
	return o, nil
}

// notFound переводит «нет строк» драйвера в доменную ErrNotFound.
// В pgx это pgx.ErrNoRows, в database/sql — sql.ErrNoRows.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s %d: %w", what, id, orders.ErrNotFound)
	}
	return fmt.Errorf("get %s %d: %w", what, id, err)
}
