package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Settings — настройки подключения из конфигурации сервиса.
type Settings struct {
	Host           string
	Port           int // 0 — 3306
	User           string
	Password       string
	Database       string
	Location       *time.Location // nil — time.UTC
	ConnectTimeout time.Duration  // 0 — 5 с
	IOTimeout      time.Duration  // 0 — 30 с
}

var (
	ErrInvalidSettings = errors.New("invalid mysql settings")
	ErrDuplicate       = errors.New("duplicate key")
	ErrRetryable       = errors.New("retryable conflict")
)

// Config собирает конфигурацию драйвера из настроек.
func Config(s Settings) (*mysql.Config, error) {
	switch {
	case s.Host == "":
		return nil, fmt.Errorf("%w: host is empty", ErrInvalidSettings)
	case s.User == "":
		return nil, fmt.Errorf("%w: user is empty", ErrInvalidSettings)
	case s.Database == "":
		return nil, fmt.Errorf("%w: database is empty", ErrInvalidSettings)
	case s.Port < 0 || s.Port > 65535:
		return nil, fmt.Errorf("%w: port %d out of range", ErrInvalidSettings, s.Port)
	case s.ConnectTimeout < 0 || s.IOTimeout < 0:
		return nil, fmt.Errorf("%w: negative timeout", ErrInvalidSettings)
	}
	port := s.Port
	if port == 0 {
		port = 3306
	}
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	// JoinHostPort ставит квадратные скобки вокруг IPv6.
	cfg.Addr = net.JoinHostPort(s.Host, strconv.Itoa(port))
	cfg.User, cfg.Passwd, cfg.DBName = s.User, s.Password, s.Database
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if s.Location != nil {
		cfg.Loc = s.Location
	}
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.Timeout = orDefault(s.ConnectTimeout, 5*time.Second)
	cfg.ReadTimeout = orDefault(s.IOTimeout, 30*time.Second)
	cfg.WriteTimeout = cfg.ReadTimeout
	// Задание оставляет TLS по умолчанию (без шифрования). В продакшне
	// добавьте cfg.TLSConfig = "true" или имя из mysql.RegisterTLSConfig.
	return cfg, nil
}

// orDefault возвращает d или значение по умолчанию, если d нулевое.
func orDefault(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

// Classify переводит ошибки MySQL в ошибки уровня приложения,
// сохраняя исходную ошибку в цепочке.
func Classify(err error) error {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return err
	}
	switch me.Number {
	case 1062: // ER_DUP_ENTRY
		return fmt.Errorf("%w: %w", ErrDuplicate, err)
	case 1213, 1205: // ER_LOCK_DEADLOCK, ER_LOCK_WAIT_TIMEOUT
		return fmt.Errorf("%w: %w", ErrRetryable, err)
	}
	return err
}

// Retry — политика повтора транзакции.
type Retry struct {
	Attempts int                                          // всего попыток; меньше 1 — одна
	Wait     func(ctx context.Context, attempt int) error // пауза перед попыткой attempt (2, 3, …); nil — без паузы
}

// InTx выполняет fn в транзакции и повторяет её целиком при
// дедлоке и истёкшем ожидании блокировки.
func (r Retry) InTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) error {
	attempts := max(r.Attempts, 1)
	for attempt := 1; ; attempt++ {
		began, err := once(ctx, db, fn)
		if err == nil || !began {
			return err
		}
		err = Classify(err)
		if !errors.Is(err, ErrRetryable) || attempt == attempts {
			return err
		}
		if r.Wait != nil {
			if err := r.Wait(ctx, attempt+1); err != nil {
				return err
			}
		}
	}
}

// once — одна попытка. began == false — транзакция не началась.
func once(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) (began bool, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	// Откат после Commit ничего не делает; при ошибке и панике
	// в fn — возвращает соединение в пул.
	defer tx.Rollback()
	if err := fn(ctx, tx); err != nil {
		return true, err
	}
	return true, tx.Commit()
}
