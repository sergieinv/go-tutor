// Package sqlite — хранилище заказов на SQLite: database/sql с драйвером
// modernc.org/sqlite (чистый Go, без cgo) и код, сгенерированный sqlc
// (пакет sqlcgen, запросы — query.sql).
//
// Основная база «Заказов» — PostgreSQL; SQLite остаётся для тестов
// (контрактный набор, сквозной тест саги) и запуска без Docker.
//
// Отличия от PostgreSQL, которые видны здесь и в query.sql:
//   - RETURNING есть (с SQLite 3.35), как в PostgreSQL;
//   - нет SEQUENCE: номера заказов выдаёт таблица order_ids с AUTOINCREMENT;
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
	"encoding/json"
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

// Store реализует orders.Store и outbox.Store.
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

// NextOrderID выдаёт номер заказа: вставка в order_ids с AUTOINCREMENT.
func (s *Store) NextOrderID(ctx context.Context) (int64, error) {
	id, err := s.q.NextOrderID(ctx)
	if err != nil {
		return 0, fmt.Errorf("next order id: %w", err)
	}
	return id, nil
}

// Order читает заказ и его позиции.
func (s *Store) Order(ctx context.Context, id int64) (orders.Order, error) {
	row, err := s.q.GetOrder(ctx, id)
	if err != nil {
		return orders.Order{}, notFound(err, "order", id)
	}
	return withItems(ctx, s.q, row)
}

// ListOrders читает страницу заказов и позиции всех её заказов двумя
// запросами (позиции — WHERE order_id IN (?, ?, …)).
func (s *Store) ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]orders.Order, error) {
	rows, err := s.q.ListCustomerOrders(ctx, sqlcgen.ListCustomerOrdersParams{
		CustomerID: customerID,
		BeforeID:   beforeID,
		RowLimit:   int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	items, err := s.q.ListItemsByOrderIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}
	byOrder := make(map[int64][]orders.Item, len(rows))
	for _, it := range items {
		byOrder[it.OrderID] = append(byOrder[it.OrderID], orders.Item{
			ProductID: it.ProductID, Quantity: int(it.Quantity), PriceCents: it.PriceCents,
		})
	}
	list := make([]orders.Order, len(rows))
	for i, r := range rows {
		list[i] = toOrder(r, byOrder[r.ID])
	}
	return list, nil
}

// ProductsByIDs читает товары одним запросом WHERE id IN (?, ?, …).
func (s *Store) ProductsByIDs(ctx context.Context, ids []int64) ([]orders.Product, error) {
	rows, err := s.q.ListProductsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	products := make([]orders.Product, len(rows))
	for i, r := range rows {
		products[i] = orders.Product{ID: r.ID, Name: r.Name, PriceCents: r.PriceCents}
	}
	return products, nil
}

// CreateProduct добавляет товар в каталог (RETURNING id — как в PostgreSQL).
func (s *Store) CreateProduct(ctx context.Context, p orders.NewProduct) (orders.Product, error) {
	id, err := s.q.CreateProduct(ctx, sqlcgen.CreateProductParams{Name: p.Name, PriceCents: p.PriceCents})
	if err != nil {
		return orders.Product{}, fmt.Errorf("create product: %w", err)
	}
	return orders.Product{ID: id, Name: p.Name, PriceCents: p.PriceCents}, nil
}

// PublishPending — один такт relay (см. postgres.Store.PublishPending).
//
// В отличие от PostgreSQL — без транзакции на время отправки: транзакция
// SQLite держит блокировку на запись всей базы (BEGIN IMMEDIATE), и пока
// идёт отправка в Kafka, ни один заказ не смог бы записаться. Advisory-
// блокировка тоже не нужна: SQLite — это один процесс, один relay.
// Гарантия та же: отметка — только после подтверждения брокера.
func (s *Store) PublishPending(ctx context.Context, limit int, publish func(context.Context, []orders.Event) error) (int, error) {
	rows, err := s.q.ListPendingEvents(ctx, int64(limit))
	if err != nil {
		return 0, fmt.Errorf("list pending events: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	events := make([]orders.Event, len(rows))
	ids := make([]int64, len(rows))
	for i, r := range rows {
		var tc map[string]string
		if err := json.Unmarshal([]byte(r.TraceContext), &tc); err != nil {
			return 0, fmt.Errorf("event %d trace context: %w", r.ID, err)
		}
		events[i] = orders.Event{
			ID: r.ID, EventID: r.EventID, Type: r.EventType, OrderID: r.AggregateID,
			Payload: r.Payload, TraceContext: tc, CreatedAt: r.CreatedAt.UTC(),
		}
		ids[i] = r.ID
	}
	if err := publish(ctx, events); err != nil {
		return 0, err
	}
	err = s.q.MarkEventsPublished(ctx, sqlcgen.MarkEventsPublishedParams{
		PublishedAt: sql.NullTime{Time: time.Now().UTC(), Valid: true},
		Ids:         ids,
	})
	if err != nil {
		return 0, fmt.Errorf("mark events published: %w", err)
	}
	return len(events), nil
}

// PendingStats — отставание outbox (см. postgres.Store.PendingStats).
func (s *Store) PendingStats(ctx context.Context) (int64, time.Time, error) {
	n, err := s.q.CountPendingEvents(ctx)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("count pending events: %w", err)
	}
	if n == 0 {
		return 0, time.Time{}, nil
	}
	oldest, err := s.q.OldestPendingEvent(ctx)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("oldest pending event: %w", err)
	}
	if len(oldest) == 0 { // успели опубликовать между запросами
		return 0, time.Time{}, nil
	}
	return n, oldest[0].UTC(), nil
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

// CreateOrder вставляет заказ с выданным заранее id и его позиции.
func (t *Tx) CreateOrder(ctx context.Context, o orders.Order) error {
	err := t.q.CreateOrder(ctx, sqlcgen.CreateOrderParams{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	for _, it := range o.Items {
		err := t.q.CreateOrderItem(ctx, sqlcgen.CreateOrderItemParams{
			OrderID:    o.ID,
			ProductID:  it.ProductID,
			Quantity:   int64(it.Quantity),
			PriceCents: it.PriceCents,
		})
		if err != nil {
			return fmt.Errorf("insert order item: %w", err)
		}
	}
	return nil
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

// AddEvent пишет событие в outbox. Контекст трассы — JSON в TEXT.
func (t *Tx) AddEvent(ctx context.Context, e orders.Event) error {
	tc := e.TraceContext
	if tc == nil {
		tc = map[string]string{}
	}
	tcJSON, err := json.Marshal(tc)
	if err != nil {
		return fmt.Errorf("marshal trace context: %w", err)
	}
	err = t.q.CreateOutboxEvent(ctx, sqlcgen.CreateOutboxEventParams{
		EventID:      e.EventID,
		EventType:    e.Type,
		AggregateID:  e.OrderID,
		Payload:      e.Payload,
		TraceContext: string(tcJSON),
		CreatedAt:    e.CreatedAt,
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
	items := make([]orders.Item, len(rows))
	for i, r := range rows {
		items[i] = orders.Item{ProductID: r.ProductID, Quantity: int(r.Quantity), PriceCents: r.PriceCents}
	}
	return toOrder(row, items), nil
}

func toOrder(row sqlcgen.Order, items []orders.Item) orders.Order {
	return orders.Order{
		ID:         row.ID,
		CustomerID: row.CustomerID,
		Status:     orders.Status(row.Status),
		Items:      items,
		TotalCents: row.TotalCents,
		CreatedAt:  row.CreatedAt.UTC(),
		UpdatedAt:  row.UpdatedAt.UTC(),
	}
}

// notFound переводит sql.ErrNoRows в доменную ErrNotFound.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %d: %w", what, id, orders.ErrNotFound)
	}
	return fmt.Errorf("get %s %d: %w", what, id, err)
}
