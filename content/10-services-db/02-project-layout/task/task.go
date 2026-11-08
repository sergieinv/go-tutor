package task

import (
	"errors"
	"time"
)

// Config — конфигурация сервиса заказов.
type Config struct {
	Addr        string
	DSN         string
	MaxConns    int
	ReadTimeout time.Duration
	Debug       bool
}

// ErrInvalidConfig — общая причина всех ошибок конфигурации.
var ErrInvalidConfig = errors.New("invalid config")

// FieldError — проблема с одной настройкой. Key — имя переменной
// окружения настройки, Problem — что не так (без значения).
type FieldError struct {
	Key     string
	Problem string
}

func (e *FieldError) Error() string { return e.Key + ": " + e.Problem }

func (e *FieldError) Is(target error) bool { return target == ErrInvalidConfig }

// LoadConfig собирает конфигурацию: значения по умолчанию, поверх
// них окружение, поверх него флаги; затем проверяет результат.
func LoadConfig(args []string, getenv func(string) string) (Config, error) {
	// TODO: реализуйте функцию
	return Config{}, nil
}

// String печатает конфигурацию без секретов.
func (c Config) String() string {
	// TODO: реализуйте метод
	return ""
}
