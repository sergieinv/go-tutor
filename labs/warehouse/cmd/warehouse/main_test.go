package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/grpcx"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"WAREHOUSE_MYSQL_DSN": "u:p@tcp(h:3306)/db"}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	want := config{dsn: "u:p@tcp(h:3306)/db", httpAddr: ":8080", grpcAddr: ":9090", opsAddr: ":8081", logLevel: slog.LevelInfo}
	if cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}

	cfg, err = loadConfig(env(map[string]string{
		"WAREHOUSE_MYSQL_DSN":       "x",
		"WAREHOUSE_LOG_LEVEL":       "debug",
		"WAREHOUSE_DRAIN_DELAY":     "5s",
		"WAREHOUSE_GRPC_REFLECTION": "true",
		"WAREHOUSE_OTLP_ENDPOINT":   "jaeger:4317",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.logLevel != slog.LevelDebug || cfg.drainDelay != 5*time.Second || !cfg.reflection || cfg.otlpEndpoint != "jaeger:4317" {
		t.Errorf("loadConfig = %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	_, err := loadConfig(env(map[string]string{
		"WAREHOUSE_HTTP_ADDR":       "8080",
		"WAREHOUSE_LOG_LEVEL":       "loud",
		"WAREHOUSE_DRAIN_DELAY":     "-1s",
		"WAREHOUSE_GRPC_REFLECTION": "maybe",
		"WAREHOUSE_OTLP_ENDPOINT":   "jaeger",
	}))
	if err == nil {
		t.Fatal("loadConfig: want error")
	}
	// errors.Join: все проблемы сразу.
	for _, s := range []string{
		"WAREHOUSE_MYSQL_DSN is required",
		`WAREHOUSE_HTTP_ADDR="8080"`,
		`WAREHOUSE_LOG_LEVEL="loud"`,
		`WAREHOUSE_DRAIN_DELAY="-1s"`,
		`WAREHOUSE_GRPC_REFLECTION="maybe"`,
		`WAREHOUSE_OTLP_ENDPOINT="jaeger"`,
	} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}
}

func TestDSNIsNotLeaked(t *testing.T) {
	const password = "s3cr3t-pa55"
	stdout := &syncBuffer{}
	// Битый адрес в конфиге: ошибка конфигурации не должна показать DSN.
	err := run(t.Context(), []string{"warehouse"}, env(map[string]string{
		"WAREHOUSE_MYSQL_DSN": "u:" + password + "@tcp(127.0.0.1:1)/db",
		"WAREHOUSE_GRPC_ADDR": "bad",
	}), stdout)
	if err == nil || strings.Contains(err.Error(), password) || strings.Contains(stdout.String(), password) {
		t.Fatalf("run error = %v, log %q; want error without the password", err, stdout)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	err := run(t.Context(), []string{"warehouse"}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WAREHOUSE_MYSQL_DSN is required") {
		t.Fatalf("run with empty env = %v, want config error", err)
	}
	if err := run(t.Context(), []string{"warehouse", "extra"}, env(nil), io.Discard); err == nil {
		t.Fatal("run with extra argument: want error")
	}
}

func TestRunFailsFastWithoutDatabase(t *testing.T) {
	// Порт 1 на loopback закрыт: Open должен вернуть ошибку, а не висеть.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	err := run(ctx, []string{"warehouse"}, env(map[string]string{
		"WAREHOUSE_MYSQL_DSN": "u:p@tcp(127.0.0.1:1)/db",
		"WAREHOUSE_HTTP_ADDR": "127.0.0.1:0",
		"WAREHOUSE_GRPC_ADDR": "127.0.0.1:0",
		"WAREHOUSE_OPS_ADDR":  "127.0.0.1:0",
	}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ping mysql") {
		t.Fatalf("run = %v, want ping error", err)
	}
}

func TestDialAddr(t *testing.T) {
	for in, want := range map[string]string{
		"[::]:9090":      "127.0.0.1:9090",
		"0.0.0.0:9090":   "127.0.0.1:9090",
		"10.1.2.3:9090":  "10.1.2.3:9090",
		"127.0.0.1:5000": "127.0.0.1:5000",
	} {
		a, err := net.ResolveTCPAddr("tcp", in)
		if err != nil {
			t.Fatal(err)
		}
		if got := dialAddr(a); got != want {
			t.Errorf("dialAddr(%s) = %s, want %s", in, got, want)
		}
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

// TestRun — дымовой тест всей сборки на настоящем MySQL: старт, миграции,
// готовность, REST и gRPC, мягкая остановка по отмене контекста (как по
// SIGTERM). Хранилища в памяти у сервиса нет, поэтому без
// WAREHOUSE_TEST_MYSQL тест пропускается.
func TestRun(t *testing.T) {
	adminDSN := os.Getenv("WAREHOUSE_TEST_MYSQL")
	if adminDSN == "" {
		t.Skip("WAREHOUSE_TEST_MYSQL is not set")
	}
	dsn := createTestDatabase(t, adminDSN)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stdout := &syncBuffer{}
	getenv := env(map[string]string{
		"WAREHOUSE_MYSQL_DSN": dsn,
		// Свободные порты: адреса узнаём из журнала.
		"WAREHOUSE_HTTP_ADDR": "127.0.0.1:0",
		"WAREHOUSE_GRPC_ADDR": "127.0.0.1:0",
		"WAREHOUSE_OPS_ADDR":  "127.0.0.1:0",
	})
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"warehouse"}, getenv, stdout) }()

	addrs := waitListening(t, stdout, done, "grpc", "http", "ops")

	// /readyz — 200, когда база доступна.
	waitStatus(t, "http://"+addrs["ops"]+"/readyz", http.StatusOK)

	// REST: демо-остаток монитора из миграции.
	resp, err := http.Get("http://" + addrs["http"] + "/v1/stock/3")
	if err != nil {
		t.Fatalf("GET /v1/stock/3: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"available":3`) {
		t.Errorf("GET /v1/stock/3: status %d, body %s", resp.StatusCode, body)
	}

	// gRPC: резерв сверх остатка и gRPC health.
	conn, err := grpcx.NewClient("passthrough:///"+addrs["grpc"], grpcx.ClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = warehousev1.NewWarehouseServiceClient(conn).Reserve(ctx, &warehousev1.ReserveRequest{
		OrderId: 1, Items: []*warehousev1.ReserveItem{{ProductId: 3, Quantity: 4}},
	})
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Errorf("Reserve over stock: %v, want FailedPrecondition", err)
	}
	// Статус gRPC health выставляет фоновый SyncGRPC — ждём его.
	hc := healthpb.NewHealthClient(conn)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		hr, err := hc.Check(ctx, &healthpb.HealthCheckRequest{Service: "warehouse.v1.WarehouseService"})
		if err == nil && hr.GetStatus() == healthpb.HealthCheckResponse_SERVING {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health = %v, %v; want SERVING", hr, err)
		}
	}

	// Метрики: свои и RED.
	resp, err = http.Get("http://" + addrs["ops"] + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, m := range []string{`warehouse_reservations_total{result="out_of_stock"} 1`, "grpc_server_handled_total", "http_server_requests_total", "warehouse_tx_retries_total"} {
		if !strings.Contains(string(body), m) {
			t.Errorf("/metrics lacks %s", m)
		}
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

// createTestDatabase создаёт базу warehouse_test_<случайное>, удаляет её в
// t.Cleanup и возвращает DSN для неё.
func createTestDatabase(t *testing.T, adminDSN string) string {
	t.Helper()
	cfg, err := mysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal("WAREHOUSE_TEST_MYSQL is not a valid DSN")
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := "warehouse_test_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE `"+name+"`"); err != nil {
		admin.Close()
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		defer admin.Close()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	cfg.DBName = name
	return cfg.FormatDSN()
}

// waitListening ждёт строки журнала "listening" для всех серверов и
// возвращает их адреса.
func waitListening(t *testing.T, stdout *syncBuffer, done <-chan error, servers ...string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("run exited early: %v\n%s", err, stdout)
		default:
		}
		addrs := map[string]string{}
		sc := bufio.NewScanner(strings.NewReader(stdout.String()))
		for sc.Scan() {
			var line struct {
				Msg    string `json:"msg"`
				Server string `json:"server"`
				Addr   string `json:"addr"`
			}
			if json.Unmarshal(sc.Bytes(), &line) == nil && line.Msg == "listening" {
				addrs[line.Server] = line.Addr
			}
		}
		if len(addrs) == len(servers) {
			return addrs
		}
		time.Sleep(10 * time.Millisecond) // опрос условия, не синхронизация «на глаз»
	}
	t.Fatalf("service did not start:\n%s", stdout)
	return nil
}

// waitStatus опрашивает url, пока не получит want (под общим таймаутом).
func waitStatus(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	last := 0
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			last = resp.StatusCode
			resp.Body.Close()
			if last == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("GET %s: status %d, want %d", url, last, want)
}
