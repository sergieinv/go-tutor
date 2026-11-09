// Package mysql — хранилище склада на MySQL: database/sql, драйвер
// go-sql-driver/mysql, запросы руками (их немного, и каждый виден целиком).
//
// Главное в пакете — InTx: транзакция с повторами. Две транзакции с одним
// order_id (Reserve и Release наперегонки, клиент повторил запрос, не
// дождавшись ответа) конкурируют за одни и те же строки, и InnoDB может
// откатить одну из них с ошибкой 1213 (deadlock): например, обе взяли
// разделяемую блокировку строки и обе хотят эксклюзивную. Это не авария, а
// штатный исход конкуренции — транзакция повторяется целиком и во второй
// раз видит уже зафиксированный результат соседки. Ошибка 1062 (duplicate
// key на вставке) — тот же класс гонок, повторяется так же.
//
// Чтобы гонки оставались редкими, запросы построены без блокировок гэпа на
// пустом месте: резерв «занимается» вставкой (ClaimReservation), а не
// чтением FOR UPDATE несуществующего ключа.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/platform/resilience"
	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/migrations"
)

// Коды ошибок сервера MySQL, после которых транзакцию повторяют.
const (
	errDeadlock     = 1213 // ER_LOCK_DEADLOCK
	errDuplicateKey = 1062 // ER_DUP_ENTRY
)

// maxAttempts — всего попыток транзакции, включая первую.
const maxAttempts = 5

// Store реализует stock.Store.
type Store struct {
	db      *sql.DB
	retry   resilience.Retry
	retries *prometheus.CounterVec
}

var _ stock.Store = (*Store)(nil)

// Open открывает пул соединений и проверяет связь с базой. dsn — в
// формате драйвера: user:pass@tcp(host:3306)/db. reg — куда
// зарегистрировать метрику повторов; nil — без метрики.
func Open(ctx context.Context, dsn string, reg prometheus.Registerer) (*Store, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn: %w", err)
	}
	// Без ParseTime драйвер отдаёт DATETIME как []byte. Loc — зона, в
	// которой драйвер пишет и читает DATETIME (столбец зоны не хранит).
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	// RowsAffected должен считать изменённые строки, а не найденные: на
	// этом построен ClaimReservation.
	cfg.ClientFoundRows = false
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second // установка TCP-соединения
	}

	// NewConnector, а не sql.Open(строка): настройки — полями структуры,
	// без сборки и повторного разбора строки DSN.
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("mysql connector: %w", err)
	}
	db := sql.OpenDB(connector)
	// Пул database/sql. Лимит соединений — защита базы: max_connections у
	// MySQL общий для всех реплик всех сервисов. ConnMaxLifetime меньше
	// wait_timeout сервера и таймаутов балансировщиков: соединение
	// закрываем раньше, чем его оборвут посреди запроса.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	s := &Store{db: db}
	s.retries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "warehouse_tx_retries_total",
		Help: "MySQL transactions retried after deadlock (1213) or duplicate key (1062).",
	}, []string{"reason"})
	s.retries.WithLabelValues("deadlock")
	s.retries.WithLabelValues("duplicate")
	if reg != nil {
		if err := reg.Register(s.retries); err != nil {
			db.Close()
			return nil, fmt.Errorf("register metrics: %w", err)
		}
	}
	s.retry = resilience.Retry{
		MaxAttempts: maxAttempts,
		// Паузы короткие и случайные (full jitter): соперники по deadlock
		// не должны снова прийти одновременно, но и ждать долго незачем —
		// конфликт длится одну транзакцию.
		BaseDelay: 2 * time.Millisecond,
		MaxDelay:  20 * time.Millisecond,
		Retryable: func(err error) bool { return retryReason(err) != "" },
		OnRetry: func(_ int, _ time.Duration, err error) {
			s.retries.WithLabelValues(retryReason(err)).Inc()
		},
	}
	return s, nil
}

// retryReason — метка причины повтора или "" для ошибок, которые не
// повторяют. Ошибки домена (OUT_OF_STOCK…) сюда не попадают — это не
// *mysql.MySQLError.
func retryReason(err error) string {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return ""
	}
	switch me.Number {
	case errDeadlock:
		return "deadlock"
	case errDuplicateKey:
		return "duplicate"
	}
	return ""
}

// Close закрывает пул.
func (s *Store) Close() error { return s.db.Close() }

// Ping проверяет связь с базой (проба готовности).
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Migrate применяет встроенные миграции goose.
//
// У goose нет блокировки сессии (session locker) для MySQL — только для
// PostgreSQL. Две реплики, стартовавшие одновременно, могут начать одну и ту
// же миграцию дважды; в проде миграции запускают отдельным шагом деплоя
// (Job перед выкаткой), а не из каждой реплики.
func (s *Store) Migrate(ctx context.Context) error {
	p, err := goose.NewProvider(goose.DialectMySQL, s.db, migrations.FS)
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate mysql: %w", err)
	}
	return nil
}

// InTx выполняет fn в транзакции и повторяет её целиком после deadlock или
// duplicate key — до maxAttempts попыток. Уровень изоляции — по умолчанию
// (REPEATABLE READ). Решения принимаются по блокирующим чтениям (FOR
// UPDATE) и по результату вставки: такие операции в InnoDB видят последнюю
// зафиксированную версию строки, а не снимок начала транзакции.
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context, tx stock.Tx) error) error {
	return s.retry.Do(ctx, func(ctx context.Context) error {
		return s.inTxOnce(ctx, fn)
	})
}

func (s *Store) inTxOnce(ctx context.Context, fn func(ctx context.Context, tx stock.Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// После Commit Rollback вернёт sql.ErrTxDone — игнорируем: это
	// идиома «откатить, если не дошли до Commit» (аналог using + Dispose
	// у DbTransaction в .NET).
	defer sqlTx.Rollback()

	if err := fn(ctx, &tx{tx: sqlTx}); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Level возвращает остаток; неизвестный товар — нулевой. Обычное
// (неблокирующее) чтение: GetStock не участвует в резервировании.
func (s *Store) Level(ctx context.Context, productID int64) (stock.Level, error) {
	l := stock.Level{ProductID: productID}
	err := s.db.QueryRowContext(ctx,
		`SELECT available, reserved FROM stock WHERE product_id = ?`, productID,
	).Scan(&l.Available, &l.Reserved)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return stock.Level{}, fmt.Errorf("select stock: %w", err)
	}
	return l, nil
}

// tx реализует stock.Tx поверх *sql.Tx. *sql.Tx уже привязана к ctx из
// BeginTx: при отмене контекста драйвер откатит её сам.
type tx struct {
	tx *sql.Tx
}

func (t *tx) LockLevel(ctx context.Context, productID int64) (stock.Level, bool, error) {
	l := stock.Level{ProductID: productID}
	err := t.tx.QueryRowContext(ctx,
		`SELECT available, reserved FROM stock WHERE product_id = ? FOR UPDATE`, productID,
	).Scan(&l.Available, &l.Reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return stock.Level{ProductID: productID}, false, nil
	}
	if err != nil {
		return stock.Level{}, false, fmt.Errorf("select stock for update: %w", err)
	}
	return l, true, nil
}

func (t *tx) AddLevel(ctx context.Context, productID int64, dAvailable, dReserved int32, at time.Time) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE stock SET available = available + ?, reserved = reserved + ?, updated_at = ? WHERE product_id = ?`,
		dAvailable, dReserved, at, productID)
	if err != nil {
		return fmt.Errorf("update stock: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update stock: %w", err)
	} else if n != 1 {
		return fmt.Errorf("update stock of product %d: %d rows affected", productID, n)
	}
	return nil
}

// ClaimReservation: INSERT … ON DUPLICATE KEY UPDATE order_id = order_id.
// Нет строки — вставка (1 затронутая строка), есть — «обновление без
// изменений» (0 строк), которое берёт эксклюзивную блокировку существующей
// строки. Ни в одном случае не берётся блокировка гэпа на пустом месте,
// как у SELECT … FOR UPDATE несуществующего ключа (см. stock.Tx).
//
// Счёт «0 или 1» верен, пока у соединения нет флага CLIENT_FOUND_ROWS: с
// ним MySQL считал бы найденные строки, а не изменённые. Open его снимает.
func (t *tx) ClaimReservation(ctx context.Context, r stock.Reservation, at time.Time) (stock.Reservation, bool, error) {
	res, err := t.tx.ExecContext(ctx,
		`INSERT INTO reservations (order_id, status, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE order_id = order_id`,
		r.OrderID, string(r.Status), at, at)
	if err != nil {
		return stock.Reservation{}, false, fmt.Errorf("insert reservation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return stock.Reservation{}, false, fmt.Errorf("insert reservation: %w", err)
	}
	if n == 0 {
		existing, err := t.lockReservation(ctx, r.OrderID)
		return existing, false, err
	}
	if err := t.insertItems(ctx, r); err != nil {
		return stock.Reservation{}, false, err
	}
	return stock.Reservation{}, true, nil
}

func (t *tx) insertItems(ctx context.Context, r stock.Reservation) error {
	if len(r.Items) == 0 {
		return nil // «надгробие»
	}
	// Все позиции одним INSERT … VALUES (?, ?, ?), (?, ?, ?)…: значения —
	// только плейсхолдерами, из кода в текст запроса попадают лишь «?».
	var b strings.Builder
	b.WriteString(`INSERT INTO reservation_items (order_id, product_id, quantity) VALUES `)
	args := make([]any, 0, 3*len(r.Items))
	for i, it := range r.Items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(?, ?, ?)")
		args = append(args, r.OrderID, it.ProductID, it.Quantity)
	}
	if _, err := t.tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("insert reservation items: %w", err)
	}
	return nil
}

// lockReservation читает резерв, строку которого ClaimReservation только что
// заблокировала.
func (t *tx) lockReservation(ctx context.Context, orderID int64) (stock.Reservation, error) {
	r := stock.Reservation{OrderID: orderID}
	var status string
	if err := t.tx.QueryRowContext(ctx,
		`SELECT status FROM reservations WHERE order_id = ? FOR UPDATE`, orderID,
	).Scan(&status); err != nil {
		return stock.Reservation{}, fmt.Errorf("select reservation: %w", err)
	}
	r.Status = stock.Status(status)

	// Позиции — обычным (неблокирующим) чтением, и это корректно: позиции
	// не меняются после вставки, а вставившая их транзакция уже
	// зафиксирована — иначе мы бы не получили блокировку строки резерва.
	// Снимок REPEATABLE READ создаётся первым неблокирующим чтением в
	// транзакции, то есть здесь, уже после блокировки. Блокирующее чтение
	// диапазона (order_id = ?) взяло бы next-key блокировки вместе с гэпом
	// после последней позиции и мешало бы вставлять позиции следующих заказов.
	rows, err := t.tx.QueryContext(ctx,
		`SELECT product_id, quantity FROM reservation_items WHERE order_id = ? ORDER BY product_id`, orderID)
	if err != nil {
		return stock.Reservation{}, fmt.Errorf("select reservation items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it stock.Item
		if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
			return stock.Reservation{}, fmt.Errorf("scan reservation item: %w", err)
		}
		r.Items = append(r.Items, it)
	}
	if err := rows.Err(); err != nil {
		return stock.Reservation{}, fmt.Errorf("select reservation items: %w", err)
	}
	return r, nil
}

func (t *tx) SetReservationStatus(ctx context.Context, orderID int64, s stock.Status, at time.Time) error {
	if _, err := t.tx.ExecContext(ctx,
		`UPDATE reservations SET status = ?, updated_at = ? WHERE order_id = ?`,
		string(s), at, orderID); err != nil {
		return fmt.Errorf("update reservation: %w", err)
	}
	return nil
}
