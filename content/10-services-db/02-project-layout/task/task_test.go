package task

import (
	"errors"
	"testing"
	"time"
)

// env превращает карту в getenv: тесты не трогают окружение процесса.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const dsn = "postgres://app:s3cret@db:5432/orders"

func TestDefaults(t *testing.T) {
	cfg, err := LoadConfig(nil, env(map[string]string{"ORDERS_DSN": dsn}))
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	want := Config{Addr: ":8080", DSN: dsn, MaxConns: 10, ReadTimeout: 5 * time.Second}
	if cfg != want {
		t.Errorf("LoadConfig = %#v, want %#v", cfg, want)
	}
}

func TestEnvAndFlags(t *testing.T) {
	e := env(map[string]string{
		"ORDERS_ADDR":         "127.0.0.1:9000",
		"ORDERS_DSN":          dsn,
		"ORDERS_MAX_CONNS":    "20",
		"ORDERS_READ_TIMEOUT": "2s",
		"ORDERS_DEBUG":        "true",
	})
	cfg, err := LoadConfig([]string{"-max-conns", "30", "-debug=false"}, e)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	want := Config{Addr: "127.0.0.1:9000", DSN: dsn, MaxConns: 30, ReadTimeout: 2 * time.Second, Debug: false}
	if cfg != want {
		t.Errorf("LoadConfig = %#v, want %#v — флаги должны перекрывать окружение", cfg, want)
	}
}

func TestMissingDSN(t *testing.T) {
	cfg, err := LoadConfig(nil, env(nil))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LoadConfig без DSN: error = %v, want ErrInvalidConfig", err)
	}
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Key != "ORDERS_DSN" {
		t.Errorf("LoadConfig без DSN: FieldError = %+v, want Key ORDERS_DSN", fe)
	}
	if cfg != (Config{}) {
		t.Errorf("при ошибке Config = %#v, want нулевой", cfg)
	}
}

func TestString(t *testing.T) {
	cfg := Config{Addr: ":8080", DSN: dsn, MaxConns: 10, ReadTimeout: 5 * time.Second}
	want := "addr=:8080 dsn=postgres://app:xxxxx@db:5432/orders max_conns=10 read_timeout=5s debug=false"
	if got := cfg.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
