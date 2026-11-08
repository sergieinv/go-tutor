package task

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// keys собирает Key всех FieldError из дерева ошибки (errors.Join
// и обёртки) в порядке обхода.
func keys(err error) []string {
	var out []string
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if fe, ok := e.(*FieldError); ok {
			out = append(out, fe.Key)
			return
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, x := range u.Unwrap() {
				walk(x)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return out
}

func TestAllProblemsAtOnce(t *testing.T) {
	e := env(map[string]string{
		"ORDERS_ADDR":         "localhost",
		"ORDERS_MAX_CONNS":    "abc",
		"ORDERS_READ_TIMEOUT": "soon",
		"ORDERS_DEBUG":        "yes",
	})
	cfg, err := LoadConfig(nil, e)
	if !errors.Is(err, ErrInvalidConfig) || cfg != (Config{}) {
		t.Fatalf("LoadConfig = (%#v, %v), want (Config{}, ErrInvalidConfig)", cfg, err)
	}
	want := []string{"ORDERS_MAX_CONNS", "ORDERS_READ_TIMEOUT", "ORDERS_DEBUG", "ORDERS_ADDR", "ORDERS_DSN"}
	if got := keys(err); !slices.Equal(got, want) {
		t.Errorf("FieldError.Key = %q, want %q", got, want)
	}
}

func TestEnvErrorAndRange(t *testing.T) {
	e := env(map[string]string{"ORDERS_DSN": dsn, "ORDERS_MAX_CONNS": "abc"})
	_, err := LoadConfig([]string{"-max-conns", "500"}, e)
	if got, want := keys(err), []string{"ORDERS_MAX_CONNS", "ORDERS_MAX_CONNS"}; !slices.Equal(got, want) {
		t.Errorf("кривое окружение и -max-conns 500: keys = %q, want %q", got, want)
	}
	// Флаг не отменяет ошибку окружения.
	_, err = LoadConfig([]string{"-max-conns", "5"}, e)
	if got, want := keys(err), []string{"ORDERS_MAX_CONNS"}; !slices.Equal(got, want) {
		t.Errorf("кривое окружение и -max-conns 5: keys = %q, want %q", got, want)
	}
}

func TestRanges(t *testing.T) {
	tests := []struct {
		args []string
		bad  string // "" — конфигурация верна
	}{
		{[]string{"-max-conns", "1"}, ""},
		{[]string{"-max-conns", "100"}, ""},
		{[]string{"-max-conns", "0"}, "ORDERS_MAX_CONNS"},
		{[]string{"-max-conns", "101"}, "ORDERS_MAX_CONNS"},
		{[]string{"-read-timeout", "1m"}, ""},
		{[]string{"-read-timeout", "1ms"}, ""},
		{[]string{"-read-timeout", "0s"}, "ORDERS_READ_TIMEOUT"},
		{[]string{"-read-timeout", "-1s"}, "ORDERS_READ_TIMEOUT"},
		{[]string{"-read-timeout", "61s"}, "ORDERS_READ_TIMEOUT"},
		{[]string{"-addr", ":0"}, ""},
		{[]string{"-addr", "[::1]:8080"}, ""},
		{[]string{"-addr", "localhost:65535"}, ""},
		{[]string{"-addr", ""}, "ORDERS_ADDR"},
		{[]string{"-addr", "localhost:http"}, "ORDERS_ADDR"},
		{[]string{"-addr", ":70000"}, "ORDERS_ADDR"},
		{[]string{"-addr", "8080"}, "ORDERS_ADDR"},
		{[]string{"-dsn", "postgresql://db/orders"}, ""},
		{[]string{"-dsn", "mysql://db/orders"}, "ORDERS_DSN"},
		{[]string{"-dsn", "postgres:///orders"}, "ORDERS_DSN"},
		{[]string{"-dsn", "host=db dbname=orders"}, "ORDERS_DSN"},
	}
	for _, tt := range tests {
		_, err := LoadConfig(tt.args, env(map[string]string{"ORDERS_DSN": dsn}))
		var want []string
		if tt.bad != "" {
			want = []string{tt.bad}
		}
		if got := keys(err); !slices.Equal(got, want) {
			t.Errorf("LoadConfig(%q): keys = %q (err %v), want %q", tt.args, got, err, want)
		}
		if tt.bad != "" && !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("LoadConfig(%q): error = %v, want ErrInvalidConfig", tt.args, err)
		}
	}
}

func TestBoolAndEmptyEnv(t *testing.T) {
	cfg, err := LoadConfig(nil, env(map[string]string{"ORDERS_DSN": dsn, "ORDERS_DEBUG": "1", "ORDERS_ADDR": "", "ORDERS_MAX_CONNS": ""}))
	if err != nil || !cfg.Debug || cfg.Addr != ":8080" || cfg.MaxConns != 10 {
		t.Errorf("ORDERS_DEBUG=1, пустые ORDERS_ADDR и ORDERS_MAX_CONNS: (%#v, %v)", cfg, err)
	}
	cfg, err = LoadConfig([]string{"-debug"}, env(map[string]string{"ORDERS_DSN": dsn}))
	if err != nil || !cfg.Debug {
		t.Errorf("-debug: (%#v, %v), want Debug = true", cfg, err)
	}
}

func TestFlagErrors(t *testing.T) {
	e := env(map[string]string{"ORDERS_DSN": dsn})
	for _, args := range [][]string{{"-port", "1"}, {"-max-conns", "many"}, {"-read-timeout", "5"}, {"extra"}, {"-debug", "extra"}} {
		cfg, err := LoadConfig(args, e)
		if !errors.Is(err, ErrInvalidConfig) || cfg != (Config{}) {
			t.Errorf("LoadConfig(%q) = (%#v, %v), want (Config{}, ErrInvalidConfig)", args, cfg, err)
		}
	}
	if _, err := LoadConfig([]string{"-h"}, e); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("LoadConfig(-h): error = %v, want flag.ErrHelp", err)
	}
}

func TestSecretNotLeaked(t *testing.T) {
	// Невалидный DSN с паролем: ошибка его не повторяет.
	_, err := LoadConfig([]string{"-dsn", "postgres://app:hunter2@/orders"}, env(nil))
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("ошибка для DSN без хоста = %v: пароль в тексте ошибки", err)
	}
	_, err = LoadConfig(nil, env(map[string]string{"ORDERS_DSN": "host=db password=hunter2"}))
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("ошибка для DSN не-URL = %v: пароль в тексте ошибки", err)
	}

	for _, dsn := range []string{"postgres://app:hunter2@db/orders", "postgres://app@db/orders?sslmode=verify-full&password=hunter2"} {
		cfg := Config{Addr: ":1", DSN: dsn, MaxConns: 1, ReadTimeout: time.Minute}
		for _, s := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%v", &cfg)} {
			if strings.Contains(s, "hunter2") {
				t.Errorf("печать конфигурации раскрывает пароль: %q", s)
			}
		}
	}
}

func TestStringCases(t *testing.T) {
	tests := []struct {
		cfg  Config
		want string
	}{
		{Config{}, "addr= dsn= max_conns=0 read_timeout=0s debug=false"},
		{Config{Addr: ":1", DSN: "postgres://db/orders", MaxConns: 3, ReadTimeout: time.Minute, Debug: true},
			"addr=:1 dsn=postgres://db/orders max_conns=3 read_timeout=1m0s debug=true"},
		{Config{DSN: "host=db password=hunter2"}, "addr= dsn=*** max_conns=0 read_timeout=0s debug=false"},
		{Config{DSN: "mysql://u:hunter2@db/x"}, "addr= dsn=*** max_conns=0 read_timeout=0s debug=false"},
	}
	for _, tt := range tests {
		if got := tt.cfg.String(); got != tt.want {
			t.Errorf("%#v.String() = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}
