package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	"gotutor/labs/notifications/internal/store/sqlite"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got, err := loadConfig(env(map[string]string{"NOTIFICATIONS_KAFKA_BROKERS": "kafka:9092"}))
		if err != nil {
			t.Fatal(err)
		}
		want := config{
			brokers: []string{"kafka:9092"}, topic: "orders.events", group: "notifications",
			dlqTopic: "notifications.dlq", maxAttempts: 3, dbPath: "/data/notifications.db", opsAddr: ":8081",
		}
		if !equalConfig(got, want) {
			t.Errorf("loadConfig = %+v, want %+v", got, want)
		}
	})
	t.Run("all set", func(t *testing.T) {
		got, err := loadConfig(env(map[string]string{
			"NOTIFICATIONS_KAFKA_BROKERS": " a:1, b:2 ,",
			"NOTIFICATIONS_KAFKA_TOPIC":   "t",
			"NOTIFICATIONS_KAFKA_GROUP":   "g",
			"NOTIFICATIONS_DLQ_TOPIC":     "d",
			"NOTIFICATIONS_MAX_ATTEMPTS":  "5",
			"NOTIFICATIONS_DB_PATH":       "/tmp/n.db",
			"NOTIFICATIONS_OPS_ADDR":      "127.0.0.1:9000",
			"NOTIFICATIONS_OTLP_ENDPOINT": "jaeger:4317",
			"NOTIFICATIONS_LOG_LEVEL":     "debug",
			"NOTIFICATIONS_DRAIN_DELAY":   "5s",
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := config{
			brokers: []string{"a:1", "b:2"}, topic: "t", group: "g", dlqTopic: "d", maxAttempts: 5,
			dbPath: "/tmp/n.db", opsAddr: "127.0.0.1:9000", otlpEndpoint: "jaeger:4317",
			logLevel: -4, drainDelay: 5 * time.Second,
		}
		if !equalConfig(got, want) {
			t.Errorf("loadConfig = %+v, want %+v", got, want)
		}
	})
	t.Run("errors are joined", func(t *testing.T) {
		_, err := loadConfig(env(map[string]string{
			"NOTIFICATIONS_KAFKA_BROKERS": "kafka",
			"NOTIFICATIONS_DLQ_TOPIC":     "orders.events",
			"NOTIFICATIONS_MAX_ATTEMPTS":  "0",
			"NOTIFICATIONS_OPS_ADDR":      "8081",
			"NOTIFICATIONS_OTLP_ENDPOINT": "jaeger",
			"NOTIFICATIONS_LOG_LEVEL":     "loud",
			"NOTIFICATIONS_DRAIN_DELAY":   "-1s",
		}))
		if err == nil {
			t.Fatal("want error")
		}
		for _, s := range []string{
			`"kafka": want host:port`, "must differ", "NOTIFICATIONS_MAX_ATTEMPTS", "NOTIFICATIONS_OPS_ADDR",
			"NOTIFICATIONS_OTLP_ENDPOINT", "NOTIFICATIONS_LOG_LEVEL", "NOTIFICATIONS_DRAIN_DELAY",
		} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("error %q does not mention %q", err, s)
			}
		}
	})
	t.Run("brokers required", func(t *testing.T) {
		_, err := loadConfig(env(nil))
		if err == nil || !strings.Contains(err.Error(), "NOTIFICATIONS_KAFKA_BROKERS is required") {
			t.Errorf("loadConfig error = %v", err)
		}
	})
}

// equalConfig сравнивает конфигурации: config содержит срез и через ==
// не сравнивается.
func equalConfig(a, b config) bool {
	return slices.Equal(a.brokers, b.brokers) &&
		a.topic == b.topic && a.group == b.group && a.dlqTopic == b.dlqTopic &&
		a.maxAttempts == b.maxAttempts && a.dbPath == b.dbPath && a.opsAddr == b.opsAddr &&
		a.otlpEndpoint == b.otlpEndpoint && a.logLevel == b.logLevel && a.drainDelay == b.drainDelay
}

// TestRun поднимает сервис целиком на kfake и временной SQLite: событие
// обрабатывается, /readyz и /metrics отвечают, отмена ctx останавливает
// сервис без ошибки.
func TestRun(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, "orders.events"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	dbPath := filepath.Join(t.TempDir(), "notifications.db")

	stdout := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"notifications"}, env(map[string]string{
			"NOTIFICATIONS_KAFKA_BROKERS": strings.Join(cluster.ListenAddrs(), ","),
			"NOTIFICATIONS_DB_PATH":       dbPath,
			"NOTIFICATIONS_OPS_ADDR":      "127.0.0.1:0",
		}), stdout)
	}()

	addr := waitLog(t, stdout, done, "ops listening")["addr"].(string)

	prod, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	defer prod.Close()
	value, err := proto.Marshal(&eventsv1.OrderEvent{
		EventId: "e1", OrderId: 42,
		Event: &eventsv1.OrderEvent_OrderCreated{OrderCreated: &eventsv1.OrderCreated{CustomerId: 5, TotalCents: 598000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := prod.ProduceSync(t.Context(), &kgo.Record{Topic: "orders.events", Key: []byte("42"), Value: value}).FirstErr(); err != nil {
		t.Fatal(err)
	}

	sent := waitLog(t, stdout, done, "notification sent")
	if sent["text"] != "Заказ 42 оформлен на 5 980,00 ₽" || sent["service"] != "notifications" {
		t.Errorf("notification sent record = %v", sent)
	}

	if code, _ := get(t, "http://"+addr+"/readyz"); code != http.StatusOK {
		t.Errorf("/readyz = %d, want 200", code)
	}
	if code, body := get(t, "http://"+addr+"/metrics"); code != http.StatusOK ||
		!strings.Contains(body, `notifications_events_total{result="sent",type="order_created"} 1`) {
		t.Errorf("/metrics = %d, body lacks the sent event counter", code)
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

	// Уведомление — в базе, и база закрыта корректно (открывается снова).
	store, err := sqlite.Open(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if ns, err := store.Notifications(t.Context(), 42); err != nil || len(ns) != 1 {
		t.Errorf("notifications = %v, %v; want 1", ns, err)
	}
}

func TestRunFailsFastOnBadConfig(t *testing.T) {
	err := run(t.Context(), []string{"notifications"}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "NOTIFICATIONS_KAFKA_BROKERS") {
		t.Errorf("run = %v, want config error", err)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// waitLog ждёт в журнале JSON-запись с сообщением msg и возвращает её.
func waitLog(t *testing.T, out *syncBuffer, done <-chan error, msg string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		sc := bufio.NewScanner(strings.NewReader(out.String()))
		for sc.Scan() {
			var rec map[string]any
			if json.Unmarshal(sc.Bytes(), &rec) == nil && rec["msg"] == msg {
				return rec
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q log record; log:\n%s", msg, out)
		}
		select {
		case err := <-done:
			t.Fatalf("run exited early: %v; log:\n%s", err, out)
		case <-tick.C:
		}
	}
}

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
