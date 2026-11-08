package task

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
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
	cfg := Config{Addr: ":8080", MaxConns: 10, ReadTimeout: 5 * time.Second}
	var errs []error
	bad := func(key, problem string) { errs = append(errs, &FieldError{Key: key, Problem: problem}) }

	// Окружение. Неразборчивое значение — ошибка, даже если флаг
	// потом его перекроет: сломанное окружение — баг деплоя.
	if v := getenv("ORDERS_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("ORDERS_DSN"); v != "" {
		cfg.DSN = v
	}
	if v := getenv("ORDERS_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err != nil {
			bad("ORDERS_MAX_CONNS", "not an integer")
		} else {
			cfg.MaxConns = n
		}
	}
	if v := getenv("ORDERS_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err != nil {
			bad("ORDERS_READ_TIMEOUT", "not a duration")
		} else {
			cfg.ReadTimeout = d
		}
	}
	if v := getenv("ORDERS_DEBUG"); v != "" {
		if b, err := strconv.ParseBool(v); err != nil {
			bad("ORDERS_DEBUG", "not a bool")
		} else {
			cfg.Debug = b
		}
	}

	fs := flag.NewFlagSet("orders", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP address")
	fs.StringVar(&cfg.DSN, "dsn", cfg.DSN, "PostgreSQL DSN")
	fs.IntVar(&cfg.MaxConns, "max-conns", cfg.MaxConns, "pool size")
	fs.DurationVar(&cfg.ReadTimeout, "read-timeout", cfg.ReadTimeout, "HTTP read timeout")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "debug mode")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, err
		}
		return Config{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("%w: unexpected argument %q", ErrInvalidConfig, fs.Arg(0))
	}

	// Проверка итоговых значений.
	if _, port, err := net.SplitHostPort(cfg.Addr); err != nil {
		bad("ORDERS_ADDR", "want host:port")
	} else if p, err := strconv.Atoi(port); err != nil || p < 0 || p > 65535 {
		bad("ORDERS_ADDR", "port must be 0..65535")
	}
	if cfg.DSN == "" {
		bad("ORDERS_DSN", "required")
	} else if u, err := url.Parse(cfg.DSN); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		bad("ORDERS_DSN", "want postgres://host/db") // само значение не печатаем: в нём пароль
	}
	if cfg.MaxConns < 1 || cfg.MaxConns > 100 {
		bad("ORDERS_MAX_CONNS", "must be 1..100")
	}
	if cfg.ReadTimeout <= 0 || cfg.ReadTimeout > time.Minute {
		bad("ORDERS_READ_TIMEOUT", "must be in (0, 1m]")
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// String печатает конфигурацию без секретов: пароль в DSN заменён
// на xxxxx. Receiver — значение, чтобы метод срабатывал и для
// Config, и для *Config.
func (c Config) String() string {
	dsn := c.DSN
	if dsn != "" {
		u, err := url.Parse(dsn)
		if err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
			// Redacted прячет только пароль в userinfo, а libpq и pgx
			// принимают его и параметром: ?password=...
			if q := u.Query(); q.Has("password") {
				q.Set("password", "xxxxx")
				u.RawQuery = q.Encode()
			}
			dsn = u.Redacted()
		} else {
			dsn = "***" // не URL — не знаем, где в строке пароль
		}
	}
	return fmt.Sprintf("addr=%s dsn=%s max_conns=%d read_timeout=%s debug=%t", c.Addr, dsn, c.MaxConns, c.ReadTimeout, c.Debug)
}
