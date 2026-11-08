package task

import (
	"context"
	"database/sql"
	"errors"
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
	// TODO
	return nil, nil
}

// Classify переводит ошибки MySQL в ошибки уровня приложения,
// сохраняя исходную ошибку в цепочке.
func Classify(err error) error {
	// TODO
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
	// TODO
	return nil
}
