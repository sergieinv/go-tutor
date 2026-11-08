package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config
		wantErr []string // подстроки текста ошибки
	}{
		{
			name: "defaults",
			env:  map[string]string{"ORDERS_DB": "sqlite", "ORDERS_DSN": "orders.db"},
			want: config{db: "sqlite", dsn: "orders.db", addr: ":8080"},
		},
		{
			name: "all set",
			env:  map[string]string{"ORDERS_DB": "postgres", "ORDERS_DSN": "postgres://u:p@h/db", "ORDERS_ADDR": "127.0.0.1:9000"},
			want: config{db: "postgres", dsn: "postgres://u:p@h/db", addr: "127.0.0.1:9000"},
		},
		{
			name:    "empty",
			env:     map[string]string{},
			wantErr: []string{"ORDERS_DB is required", "ORDERS_DSN is required"},
		},
		{
			name:    "unknown db and bad addr",
			env:     map[string]string{"ORDERS_DB": "oracle", "ORDERS_DSN": "x", "ORDERS_ADDR": "8080"},
			wantErr: []string{`ORDERS_DB="oracle"`, `ORDERS_ADDR="8080"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadConfig(env(tt.env))
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("loadConfig: %v", err)
				}
				if got != tt.want {
					t.Errorf("loadConfig = %+v, want %+v", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("loadConfig = %+v, want error", got)
			}
			for _, s := range tt.wantErr {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not mention %q", err, s)
				}
			}
		})
	}
}

func TestConfigErrorDoesNotLeakDSN(t *testing.T) {
	const password = "s3cr3t-pa55"
	_, err := loadConfig(env(map[string]string{"ORDERS_DB": "oracle", "ORDERS_DSN": "postgres://u:" + password + "@h/db"}))
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("loadConfig error = %v; want error without the password", err)
	}
}

// syncBuffer — stdout сервиса: run пишет журнал из своих горутин.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestRun — дымовой тест всей сборки: старт на SQLite, миграции,
// запрос к демо-каталогу, мягкая остановка по отмене контекста (как по SIGTERM).
func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stdout := &syncBuffer{}
	getenv := env(map[string]string{
		"ORDERS_DB":   "sqlite",
		"ORDERS_DSN":  filepath.Join(t.TempDir(), "orders.db"),
		"ORDERS_ADDR": "127.0.0.1:0", // свободный порт: адрес узнаём из журнала
	})

	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"orders"}, getenv, stdout) }()

	addr := waitListening(t, stdout, done)

	resp, err := http.Get("http://" + addr + "/products/1")
	if err != nil {
		t.Fatalf("GET /products/1: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Клавиатура") {
		t.Errorf("GET /products/1: status %d, body %s; want the demo catalog", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
	if !strings.Contains(stdout.String(), `"msg":"stopped"`) {
		t.Errorf("no graceful stop in log:\n%s", stdout)
	}
}

// waitListening ждёт строку журнала "listening" и возвращает адрес.
func waitListening(t *testing.T, stdout *syncBuffer, done <-chan error) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("run exited early: %v\n%s", err, stdout)
		default:
		}
		sc := bufio.NewScanner(strings.NewReader(stdout.String()))
		for sc.Scan() {
			var line struct {
				Msg  string `json:"msg"`
				Addr string `json:"addr"`
			}
			if json.Unmarshal(sc.Bytes(), &line) == nil && line.Msg == "listening" {
				return line.Addr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("service did not start:\n%s", stdout)
	return ""
}

func TestRunRejectsBadConfig(t *testing.T) {
	err := run(t.Context(), []string{"orders"}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ORDERS_DB is required") {
		t.Fatalf("run with empty env = %v, want config error", err)
	}
}
