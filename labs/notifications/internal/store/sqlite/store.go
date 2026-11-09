// Package sqlite — хранилище «Уведомлений» на SQLite: database/sql с
// драйвером modernc.org/sqlite (чистый Go, без cgo — образ собирается с
// CGO_ENABLED=0) и миграциями goose, встроенными в бинарник.
//
// Почему дедупликация (processed_events) лежит в той же базе, что и
// результат (notifications): тогда «отметить событие обработанным» и
// «сохранить уведомление» — одна локальная транзакция. Либо есть оба, либо
// ни одного. Храни мы отметки в Redis, а уведомления в базе, сбой между
// двумя записями давал бы либо потерю (отметка есть, уведомления нет),
// либо дубль — и понадобилась бы распределённая транзакция.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // регистрирует драйвер "sqlite" в database/sql

	"gotutor/labs/notifications/internal/notify"
	"gotutor/labs/notifications/migrations"
)

// Store реализует notify.Store.
type Store struct {
	db *sql.DB
}

var _ notify.Store = (*Store)(nil)

// Open открывает (или создаёт) базу в файле path и проверяет её.
//
// Принимает путь, а не DSN: параметры соединения, без которых хранилище
// работает неправильно, задаёт само:
//   - journal_mode(WAL) — читатели (пинг готовности, ручной sqlite3 при
//     отладке) не блокируют писателя и наоборот;
//   - busy_timeout — ждать чужую блокировку, а не сразу получать
//     SQLITE_BUSY («database is locked»);
//   - foreign_keys(1) — в SQLite внешние ключи по умолчанию выключены, и
//     PRAGMA действует на соединение, поэтому — в DSN, для каждого;
//   - _txlock=immediate — транзакция берёт блокировку на запись сразу
//     (BEGIN IMMEDIATE), а не на первом INSERT.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") || strings.Contains(path, ":memory:") {
		return nil, fmt.Errorf("sqlite: want a plain file path, got %q", path)
	}
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Set("_txlock", "immediate")

	db, err := sql.Open("sqlite", "file:"+path+"?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Одно соединение. Писатель в SQLite в каждый момент один на всю базу;
	// несколько соединений пула только соревновались бы за блокировку
	// файла и ловили SQLITE_BUSY друг от друга. Консьюмер и так пишет
	// последовательно, а пинг готовности подождёт конца транзакции (она
	// короткая). Заодно пул не «размножает» соединения, у каждого из
	// которых свой кеш страниц. Нужно больше читателей — заводят второй
	// *sql.DB только для чтения (mode=ro).
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// Migrate применяет миграции goose.
func (s *Store) Migrate(ctx context.Context) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, s.db, migrations.FS)
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

// Ping проверяет связь с базой (проверка готовности).
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// SaveOnce реализует notify.Store: отметка события и уведомление — в одной
// транзакции.
func (s *Store) SaveOnce(ctx context.Context, n notify.Notification) (saved bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	// Rollback после Commit — пустая операция (sql.ErrTxDone), поэтому
	// defer безопасен на всех путях: аналог using над транзакцией в C#.
	defer tx.Rollback()

	// ON CONFLICT DO NOTHING вместо «SELECT, потом INSERT»: проверку и
	// вставку делает база атомарно. 0 строк — событие уже обработано.
	res, err := tx.ExecContext(ctx,
		`INSERT INTO processed_events (event_id, processed_at) VALUES (?, ?)
		 ON CONFLICT (event_id) DO NOTHING`,
		n.EventID, n.CreatedAt.UnixMilli())
	if err != nil {
		return false, fmt.Errorf("insert processed event: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return false, nil // повтор; Rollback из defer ничего не меняет
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO notifications (event_id, order_id, customer_id, kind, text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		n.EventID, n.OrderID, n.CustomerID, string(n.Kind), n.Text, n.CreatedAt.UnixMilli()); err != nil {
		return false, fmt.Errorf("insert notification: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// Notifications возвращает уведомления по заказу в порядке создания. Нужен
// тестам и отладке; API чтения у сервиса нет.
func (s *Store) Notifications(ctx context.Context, orderID int64) ([]notify.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_id, order_id, customer_id, kind, text, created_at
		 FROM notifications WHERE order_id = ? ORDER BY id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()
	var out []notify.Notification
	for rows.Next() {
		var (
			n         notify.Notification
			kind      string
			createdAt int64
		)
		if err := rows.Scan(&n.EventID, &n.OrderID, &n.CustomerID, &kind, &n.Text, &createdAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		n.Kind = notify.Kind(kind)
		n.CreatedAt = time.UnixMilli(createdAt).UTC()
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	return out, nil
}
