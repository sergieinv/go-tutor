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

	"github.com/alicebob/miniredis/v2"
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

func TestLoadConfigModule14(t *testing.T) {
	cfg, err := loadConfig(env(minimal))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.createMaxInFlight != 64 || cfg.createMaxQueue != 64 || cfg.createQueueTimeout != 100*time.Millisecond ||
		cfg.catalogTTL != 5*time.Minute || cfg.catalogNegativeTTL != 30*time.Second ||
		cfg.valkeyAddr != "" || cfg.replicaDSN != "" || cfg.adminFeatures ||
		len(cfg.shardDSNs) != 1 || cfg.shardDSNs[0] != cfg.dsn {
		t.Errorf("module 14 defaults = %+v", cfg)
	}

	pg := with(minimal, "ORDERS_DB", "postgres", "ORDERS_DSN", "")
	cfg, err = loadConfig(env(with(pg, "ORDERS_SHARD_DSNS", "postgres://a/orders, postgres://b/orders",
		"ORDERS_REPLICA_DSN", "postgres://r/orders", "ORDERS_VALKEY_ADDR", "valkey:6379",
		"ORDERS_CREATE_MAX_IN_FLIGHT", "0", "ORDERS_ADMIN_FEATURES", "true")))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.shardDSNs) != 2 || cfg.dsn != "postgres://a/orders" || cfg.createMaxInFlight != 0 || !cfg.adminFeatures {
		t.Errorf("sharded cfg = %+v", cfg)
	}

	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"shards on sqlite":  {with(minimal, "ORDERS_SHARD_DSNS", "a.db,b.db"), "requires ORDERS_DB=postgres"},
		"dsn != shard 0":    {with(pg, "ORDERS_DSN", "postgres://x/orders", "ORDERS_SHARD_DSNS", "postgres://a/orders,postgres://b/orders"), "equal to the first"},
		"replica on sqlite": {with(minimal, "ORDERS_REPLICA_DSN", "postgres://r/orders"), "ORDERS_REPLICA_DSN requires"},
		"bad valkey":        {with(minimal, "ORDERS_VALKEY_ADDR", "valkey"), "ORDERS_VALKEY_ADDR"},
		"bad limits":        {with(minimal, "ORDERS_CREATE_MAX_IN_FLIGHT", "-1", "ORDERS_CREATE_QUEUE_TIMEOUT", "soon"), "ORDERS_CREATE_QUEUE_TIMEOUT"},
		"bad admin flag":    {with(minimal, "ORDERS_ADMIN_FEATURES", "yes please"), "ORDERS_ADMIN_FEATURES"},
	} {
		if _, err := loadConfig(env(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want mention of %q", name, err, tc.want)
		}
	}

	// Пароли шардов и реплики не попадают в ошибки.
	const password = "sh4rd-pa55"
	_, err = loadConfig(env(with(minimal, "ORDERS_SHARD_DSNS", "postgres://u:"+password+"@a/db", "ORDERS_REPLICA_DSN", "postgres://u:"+password+"@r/db")))
	if err == nil || strings.Contains(err.Error(), password) {
		t.Errorf("error = %v; want error without the password", err)
	}
}

// TestRunModule14 — сборка main с кешем каталога (miniredis) и
// переключателями: GetProduct через REST идёт в базу один раз, затем из
// кеша; /admin/features на служебном порту выключает кеш без перезапуска.
func TestRunModule14(t *testing.T) {
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
	mr := miniredis.RunT(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stdout := &syncBuffer{}
	getenv := env(map[string]string{
		"ORDERS_DB":             "sqlite",
		"ORDERS_DSN":            filepath.Join(t.TempDir(), "orders.db"),
		"ORDERS_HTTP_ADDR":      "127.0.0.1:0",
		"ORDERS_GRPC_ADDR":      "127.0.0.1:0",
		"ORDERS_OPS_ADDR":       "127.0.0.1:0",
		"ORDERS_WAREHOUSE_ADDR": "passthrough:///" + whLis.Addr().String(),
		"ORDERS_KAFKA_BROKERS":  strings.Join(cluster.ListenAddrs(), ","),
		"ORDERS_VALKEY_ADDR":    mr.Addr(),
		"ORDERS_ADMIN_FEATURES": "true",
	})
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"orders"}, getenv, stdout) }()
	addrs := waitListening(t, stdout, done, "http", "ops", "grpc")
	api, ops := "http://"+addrs["http"], "http://"+addrs["ops"]

	dbQueries := func() string {
		var m string
		get(t, ops+"/metrics", &m)
		for _, line := range strings.Split(m, "\n") {
			if strings.HasPrefix(line, "orders_catalog_db_queries_total ") {
				return strings.TrimPrefix(line, "orders_catalog_db_queries_total ")
			}
		}
		return "absent"
	}

	var body string
	for range 3 {
		if code := get(t, api+"/v1/products/1", &body); code != http.StatusOK || !strings.Contains(body, "Клавиатура") {
			t.Fatalf("GET /v1/products/1 = %d %s", code, body)
		}
	}
	if q := dbQueries(); q != "1" {
		t.Errorf("db queries with cache = %s, want 1", q)
	}
	if code := get(t, api+"/v1/products/404", nil); code != http.StatusNotFound {
		t.Errorf("missing product = %d, want 404", code)
	}

	// Переключатели — только на служебном порту.
	if code := get(t, api+"/admin/features", nil); code == http.StatusOK {
		t.Error("/admin/features is exposed on the public REST port")
	}
	resp, err := http.Post(ops+"/admin/features", "application/json", strings.NewReader(`{"catalog_cache": false}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /admin/features = %d", resp.StatusCode)
	}
	get(t, api+"/v1/products/1", nil)
	get(t, api+"/v1/products/1", nil)
	if q := dbQueries(); q != "4" { // 1 + 404 + два чтения без кеша
		t.Errorf("db queries with cache off = %s, want 4", q)
	}
	// Реплика не настроена — включить нельзя.
	resp, err = http.Post(ops+"/admin/features", "application/json", strings.NewReader(`{"replica_reads": true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("enable replica without DSN = %d, want 409", resp.StatusCode)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}
