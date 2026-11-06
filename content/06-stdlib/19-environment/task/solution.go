package task

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config — настройки сервиса.
type Config struct {
	Addr    string        // APP_ADDR, по умолчанию ":8080"
	Debug   bool          // APP_DEBUG, по умолчанию false
	Timeout time.Duration // APP_TIMEOUT, по умолчанию 5s
	Workers int           // APP_WORKERS, по умолчанию 4
	Tags    []string      // APP_TAGS, по умолчанию []string{"default"}
}

// value возвращает значение переменной или def, если она не задана или пустая.
func value(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load читает конфигурацию из окружения.
func Load() (Config, error) {
	cfg := Config{Addr: value("APP_ADDR", ":8080")}

	var err error
	if cfg.Debug, err = strconv.ParseBool(value("APP_DEBUG", "false")); err != nil {
		return Config{}, fmt.Errorf("APP_DEBUG: %w", err)
	}
	if cfg.Timeout, err = time.ParseDuration(value("APP_TIMEOUT", "5s")); err != nil {
		return Config{}, fmt.Errorf("APP_TIMEOUT: %w", err)
	}
	if cfg.Timeout <= 0 {
		return Config{}, fmt.Errorf("APP_TIMEOUT: должен быть больше нуля, получено %v", cfg.Timeout)
	}
	if cfg.Workers, err = strconv.Atoi(value("APP_WORKERS", "4")); err != nil {
		return Config{}, fmt.Errorf("APP_WORKERS: %w", err)
	}
	if cfg.Workers <= 0 {
		return Config{}, fmt.Errorf("APP_WORKERS: должен быть больше нуля, получено %d", cfg.Workers)
	}

	// Здесь пустое значение и отсутствие различаются — нужен LookupEnv.
	raw, ok := os.LookupEnv("APP_TAGS")
	if !ok {
		cfg.Tags = []string{"default"}
	} else {
		for _, tag := range strings.Split(raw, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				cfg.Tags = append(cfg.Tags, tag)
			}
		}
	}
	return cfg, nil
}
