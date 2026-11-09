package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"gotutor/labs/warehouse/warehousetest"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var minimal = map[string]string{
	"ORDERS_DB":             "sqlite",
	"ORDERS_DSN":            "orders.db",
	"ORDERS_WAREHOUSE_ADDR": "dns:///warehouse:9090",
	"ORDERS_KAFKA_BROKERS":  "kafka:9092",
}

func with(base map[string]string, kv ...string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(minimal))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.httpAddr != ":8080" || cfg.grpcAddr != ":9090" || cfg.opsAddr != ":8081" ||
		cfg.kafkaTopic != "orders.events" || cfg.kafkaPartitions != 3 || cfg.kafkaReplication != 1 ||
		cfg.outboxInterval != 500*time.Millisecond || cfg.drainDelay != 0 || cfg.reflection {
		t.Errorf("defaults = %+v", cfg)
	}
	cfg, err = loadConfig(env(with(minimal, "ORDERS_KAFKA_BROKERS", " a:1, b:2 ,", "ORDERS_GRPC_REFLECTION", "true", "ORDERS_DRAIN_DELAY", "5s")))
	if err != nil || len(cfg.kafkaBrokers) != 2 || cfg.kafkaBrokers[1] != "b:2" || !cfg.reflection || cfg.drainDelay != 5*time.Second {
		t.Errorf("cfg = %+v, %v", cfg, err)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr []string
	}{
		{"empty", map[string]string{}, []string{"ORDERS_DB is required", "ORDERS_DSN is required", "ORDERS_WAREHOUSE_ADDR is required", "ORDERS_KAFKA_BROKERS is required"}},
		{"bad values", with(minimal, "ORDERS_DB", "mysql", "ORDERS_HTTP_ADDR", "8080", "ORDERS_KAFKA_PARTITIONS", "0", "ORDERS_LOG_LEVEL", "loud", "ORDERS_DRAIN_DELAY", "soon"),
			[]string{`ORDERS_DB="mysql"`, `ORDERS_HTTP_ADDR="8080"`, "ORDERS_KAFKA_PARTITIONS", "ORDERS_LOG_LEVEL", "ORDERS_DRAIN_DELAY"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(env(tt.env))
			if err == nil {
				t.Fatal("want error")
			}
			for _, s := range tt.wantErr {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not mention %q", err, s)
				}
			}
		})
	}
}

func TestConfigDoesNotLeakDSN(t *testing.T) {
	const password = "s3cr3t-pa55"
	_, err := loadConfig(env(with(minimal, "ORDERS_DB", "oracle", "ORDERS_DSN", "postgres://u:"+password+"@h/db")))
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

// TestRun — дымовой тест всей сборки main: SQLite с демо-каталогом,
// настоящий «Склад» в памяти на loopback, Kafka — kfake. Создаём заказ по
// REST, ждём событие в Kafka, проверяем пробы и метрики, мягко
// останавливаем отменой контекста (как по SIGTERM).
func TestRun(t *testing.T) {
	// Демо-каталог «Заказов»: 1 — клавиатура, 2 — мышь, 3 — монитор.
	wh := warehousetest.New(map[int64]int32{1: 10, 2: 25, 3: 3})
	whLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	wh.Serve(t, whLis)
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stdout := &syncBuffer{}
	getenv := env(map[string]string{
		"ORDERS_DB":              "sqlite",
		"ORDERS_DSN":             filepath.Join(t.TempDir(), "orders.db"),
		"ORDERS_HTTP_ADDR":       "127.0.0.1:0", // свободные порты: адреса — из журнала
		"ORDERS_GRPC_ADDR":       "127.0.0.1:0",
		"ORDERS_OPS_ADDR":        "127.0.0.1:0",
		"ORDERS_WAREHOUSE_ADDR":  "passthrough:///" + whLis.Addr().String(),
		"ORDERS_KAFKA_BROKERS":   strings.Join(cluster.ListenAddrs(), ","),
		"ORDERS_OUTBOX_INTERVAL": "20ms",
	})
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"orders"}, getenv, stdout) }()
	addrs := waitListening(t, stdout, done, "http", "ops", "grpc")

	resp, err := http.Post("http://"+addrs["http"]+"/v1/orders", "application/json",
		strings.NewReader(`{"customer_id": 42, "items": [{"product_id": 3, "quantity": 2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Request-ID") == "" {
		t.Fatalf("POST /v1/orders = %d %s", resp.StatusCode, body)
	}
	if a, r := wh.Stock(3); a != 1 || r != 2 {
		t.Errorf("warehouse stock = %d/%d, want 1/2", a, r)
	}

	// Relay публикует событие в топик, созданный сервисом при старте.
	consumer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumeTopics("orders.events"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	pollCtx, cancelPoll := context.WithTimeout(ctx, 15*time.Second)
	defer cancelPoll()
	if recs := consumer.PollFetches(pollCtx).Records(); len(recs) == 0 {
		t.Fatalf("no event in Kafka:\n%s", stdout)
	}

	for _, probe := range []string{"http://" + addrs["http"] + "/readyz", "http://" + addrs["ops"] + "/readyz"} {
		if code := get(t, probe, nil); code != http.StatusOK {
			t.Errorf("%s = %d", probe, code)
		}
	}
	var metrics string
	get(t, "http://"+addrs["ops"]+"/metrics", &metrics)
	for _, m := range []string{"orders_outbox_published_total", "grpc_server_handled_total", "http_server_requests_total", "grpc_client_handled_total"} {
		if !strings.Contains(metrics, m) {
			t.Errorf("/metrics lacks %s", m)
		}
	}
	// /metrics — только на служебном порту.
	if code := get(t, "http://"+addrs["http"]+"/metrics", nil); code == http.StatusOK {
		t.Error("/metrics is exposed on the public REST port")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
	if !strings.Contains(stdout.String(), `"msg":"stopped"`) {
		t.Errorf("no graceful stop in log:\n%s", stdout)
	}
}

func get(t *testing.T, url string, body *string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if body != nil {
		*body = string(b)
	}
	return resp.StatusCode
}

// waitListening ждёт строки журнала "listening" для всех серверов.
func waitListening(t *testing.T, stdout *syncBuffer, done <-chan error, servers ...string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
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
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("service did not start:\n%s", stdout)
	return nil
}

func TestRunRejectsBadConfig(t *testing.T) {
	err := run(t.Context(), []string{"orders"}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "ORDERS_DB is required") {
		t.Fatalf("run with empty env = %v, want config error", err)
	}
}
