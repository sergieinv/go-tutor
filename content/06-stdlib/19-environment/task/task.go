package task

import "time"

// Config — настройки сервиса.
type Config struct {
	Addr    string        // APP_ADDR, по умолчанию ":8080"
	Debug   bool          // APP_DEBUG, по умолчанию false
	Timeout time.Duration // APP_TIMEOUT, по умолчанию 5s
	Workers int           // APP_WORKERS, по умолчанию 4
	Tags    []string      // APP_TAGS, по умолчанию []string{"default"}
}

// Load читает конфигурацию из окружения.
func Load() (Config, error) {
	// TODO: реализуйте функцию
	return Config{}, nil
}
