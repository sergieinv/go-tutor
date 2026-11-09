// Package postgres — хранилище заказов на PostgreSQL: пул pgxpool и код,
// сгенерированный sqlc (пакет sqlcgen, запросы — query.sql).
//
// pgx используется напрямую, без database/sql: так доступны нативные типы
// PostgreSQL (массивы, jsonb) и чуть меньше накладных расходов. database/sql
// нужен только goose для миграций — его даёт адаптер stdlib поверх того же пула.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/postgres/sqlcgen"
	"gotutor/labs/orders/migrations"
)

// Store реализует orders.Store и outbox.Store.
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

// OpenReplica создаёт пул к реплике, не проверяя связь: недоступная при
// старте реплика не мешает сервису (история читается с основной базы, см.
// internal/replica), а пул подключится, когда реплика поднимется.
// Миграции на реплике не применяются — она только для чтения и получает
// схему с основной базы вместе с WAL.
func OpenReplica(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse replica dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create replica pool: %w", err)
	}
	return &Store{pool: pool, q: sqlcgen.New(pool)}, nil
}

// WALPosition — текущая позиция записи WAL основной базы
// (pg_current_wal_lsn). Вызванная после COMMIT, она не меньше позиции
// этого коммита: реплика, применившая её, видит транзакцию.
func (s *Store) WALPosition(ctx context.Context) (uint64, error) {
	var lsn string
	if err := s.pool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsn); err != nil {
		return 0, fmt.Errorf("current wal lsn: %w", err)
	}
	return parseLSN(lsn)
}

// ReplayPosition — до какой позиции WAL реплика применила изменения
// (pg_last_wal_replay_lsn). На основной базе функция возвращает NULL —
// это ошибка: значит, DSN реплики указывает не на реплику.
func (s *Store) ReplayPosition(ctx context.Context) (uint64, error) {
	var lsn *string
	if err := s.pool.QueryRow(ctx, "SELECT pg_last_wal_replay_lsn()::text").Scan(&lsn); err != nil {
		return 0, fmt.Errorf("last wal replay lsn: %w", err)
	}
	if lsn == nil {
		return 0, errors.New("last wal replay lsn: server is not in recovery (not a replica)")
	}
	return parseLSN(*lsn)
}

// parseLSN разбирает текстовую позицию WAL «16/B374D848» (две половины
// 64-битного числа в шестнадцатеричном виде).
func parseLSN(s string) (uint64, error) {
	hi, lo, ok := strings.Cut(s, "/")
	if !ok {
		return 0, fmt.Errorf("malformed lsn %q", s)
	}
	h, err1 := strconv.ParseUint(hi, 16, 32)
	l, err2 := strconv.ParseUint(lo, 16, 32)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("malformed lsn %q", s)
	}
	return h<<32 | l, nil
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
// COMMITTED). Его достаточно: отмена блокирует строку заказа FOR UPDATE,
// смена статуса проверяет старое значение.
func (s *Store) Begin(ctx context.Context) (orders.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	return &Tx{tx: tx, q: s.q.WithTx(tx)}, nil
}

// NextOrderID выдаёт номер заказа из последовательности.
func (s *Store) NextOrderID(ctx context.Context) (int64, error) {
	id, err := s.q.NextOrderID(ctx)
	if err != nil {
		return 0, fmt.Errorf("next order id: %w", err)
	}
	return id, nil
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

// ListOrders читает страницу заказов и позиции всех её заказов — двумя
// запросами независимо от размера страницы.
func (s *Store) ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]orders.Order, error) {
	rows, err := s.q.ListCustomerOrders(ctx, sqlcgen.ListCustomerOrdersParams{
		CustomerID: customerID,
		BeforeID:   beforeID,
		RowLimit:   int32(min(limit, math.MaxInt32)),
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

// ProductsByIDs читает товары одним запросом: срез уходит в PostgreSQL
// одним параметром-массивом (WHERE id = ANY($1)).
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

// CreateProduct добавляет товар в каталог. RETURNING id: PostgreSQL
// возвращает сгенерированный ключ прямо из INSERT.
func (s *Store) CreateProduct(ctx context.Context, p orders.NewProduct) (orders.Product, error) {
	id, err := s.q.CreateProduct(ctx, sqlcgen.CreateProductParams{Name: p.Name, PriceCents: p.PriceCents})
	if err != nil {
		return orders.Product{}, fmt.Errorf("create product: %w", err)
	}
	return orders.Product{ID: id, Name: p.Name, PriceCents: p.PriceCents}, nil
}

// outboxLockKey — ключ advisory-блокировки relay. Произвольное число,
// уникальное в пределах базы.
const outboxLockKey = 0x6f7574626f78 // "outbox"

// PublishPending — один такт relay: в транзакции берёт до limit
// неопубликованных событий по порядку, отдаёт их publish и, если тот
// вернул nil (брокер подтвердил запись), отмечает опубликованными.
//
// Транзакция держится, пока идёт отправка, — это и есть «отметка после
// подтверждения». Упал relay между подтверждением Kafka и COMMIT — события
// уйдут ещё раз: доставка at-least-once, потребители дедуплицируют по
// event_id.
func (s *Store) PublishPending(ctx context.Context, limit int, publish func(context.Context, []orders.Event) error) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.TryLockOutbox(ctx, outboxLockKey)
		if err != nil {
			return fmt.Errorf("lock outbox: %w", err)
		}
		if !locked {
			return nil // публикует другая реплика
		}
		rows, err := q.ListPendingEvents(ctx, int32(min(limit, math.MaxInt32)))
		if err != nil {
			return fmt.Errorf("list pending events: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		events := make([]orders.Event, len(rows))
		ids := make([]int64, len(rows))
		for i, r := range rows {
			var tc map[string]string
			if err := json.Unmarshal(r.TraceContext, &tc); err != nil {
				return fmt.Errorf("event %d trace context: %w", r.ID, err)
			}
			events[i] = orders.Event{
				ID: r.ID, EventID: r.EventID, Type: r.EventType, OrderID: r.AggregateID,
				Payload: r.Payload, TraceContext: tc, CreatedAt: r.CreatedAt.UTC(),
			}
			ids[i] = r.ID
		}
		if err := publish(ctx, events); err != nil {
			return err
		}
		err = q.MarkEventsPublished(ctx, sqlcgen.MarkEventsPublishedParams{
			PublishedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
			Ids:         ids,
		})
		if err != nil {
			return fmt.Errorf("mark events published: %w", err)
		}
		n = len(events)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// PendingStats — отставание outbox: число неопубликованных событий и время
// создания самого старого (нулевое, если ждущих нет).
func (s *Store) PendingStats(ctx context.Context) (int64, time.Time, error) {
	row, err := s.q.OutboxStats(ctx)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("outbox stats: %w", err)
	}
	if row.Pending == 0 {
		return 0, time.Time{}, nil
	}
	return row.Pending, row.Oldest.UTC(), nil
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
	// Позиций не больше orders.MaxItems — по INSERT на позицию приемлемо.
	// Для тысяч строк в PostgreSQL есть COPY (в sqlc — аннотация :copyfrom).
	for _, it := range o.Items {
		err := t.q.CreateOrderItem(ctx, sqlcgen.CreateOrderItemParams{
			OrderID:    o.ID,
			ProductID:  it.ProductID,
			Quantity:   int32(it.Quantity),
			PriceCents: it.PriceCents,
		})
		if err != nil {
			return fmt.Errorf("insert order item: %w", err)
		}
	}
	return nil
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
	tc, err := json.Marshal(nonNil(e.TraceContext))
	if err != nil {
		return fmt.Errorf("marshal trace context: %w", err)
	}
	err = t.q.CreateOutboxEvent(ctx, sqlcgen.CreateOutboxEventParams{
		EventID:      e.EventID,
		EventType:    e.Type,
		AggregateID:  e.OrderID,
		Payload:      e.Payload,
		TraceContext: tc,
		CreatedAt:    e.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// nonNil заменяет nil-map пустой: в jsonb пишется {}, а не null.
func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// withItems дочитывает позиции заказа одним запросом и собирает доменный заказ.
// q — пул или транзакция: sqlc-код работает с обоими через интерфейс DBTX.
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
		// pgx возвращает timestamptz в локальной зоне процесса; домен
		// работает в UTC.
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

// notFound переводит «нет строк» драйвера в доменную ErrNotFound.
// В pgx это pgx.ErrNoRows, в database/sql — sql.ErrNoRows.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s %d: %w", what, id, orders.ErrNotFound)
	}
	return fmt.Errorf("get %s %d: %w", what, id, err)
}
