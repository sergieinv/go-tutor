package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openDB открывает базу с пулом из одного соединения: незакрытые
// Rows или незавершённая транзакция заблокируют следующий запрос.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	return db
}

// withTimeout — чтобы зависание на пуле стало ошибкой, а не вечным тестом.
func withTimeout(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// fakePub запоминает опубликованные события; failOn — номер вызова
// Publish (с 1), который вернёт errPub.
type fakePub struct {
	got    []Event
	calls  int
	failOn int
}

var errPub = errors.New("брокер недоступен")

func (p *fakePub) Publish(ctx context.Context, e Event) error {
	p.calls++
	if p.calls == p.failOn {
		return errPub
	}
	p.got = append(p.got, e)
	return nil
}

func unpublished(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateOrder(t *testing.T) {
	db := openDB(t)
	id, err := CreateOrder(withTimeout(t), db, "Анна", 300)
	if err != nil || id != 1 {
		t.Fatalf("CreateOrder = (%d, %v), want (1, nil)", id, err)
	}
	var topic, key string
	var payload []byte
	err = db.QueryRow(`SELECT topic, key, payload FROM outbox WHERE published_at IS NULL`).Scan(&topic, &key, &payload)
	if err != nil {
		t.Fatalf("в outbox нет неопубликованного события: %v", err)
	}
	var body OrderCreated
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("payload не JSON: %v (%s)", err, payload)
	}
	if topic != TopicOrderCreated || key != "1" || body != (OrderCreated{OrderID: 1, Customer: "Анна", Total: 300}) {
		t.Fatalf("событие: topic=%q key=%q body=%+v", topic, key, body)
	}
}

func TestCreateOrderInvalid(t *testing.T) {
	db := openDB(t)
	if _, err := CreateOrder(withTimeout(t), db, "", 100); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("CreateOrder без покупателя = %v, want ErrInvalidOrder", err)
	}
}

func TestRelay(t *testing.T) {
	db := openDB(t)
	for _, c := range []string{"a", "b", "c"} {
		if _, err := CreateOrder(withTimeout(t), db, c, 10); err != nil {
			t.Fatal(err)
		}
	}
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub}
	n, err := r.RunOnce(withTimeout(t))
	if err != nil || n != 3 {
		t.Fatalf("RunOnce = (%d, %v), want (3, nil)", n, err)
	}
	for i, e := range pub.got {
		if e.ID != int64(i+1) || e.Topic != TopicOrderCreated {
			t.Errorf("событие %d: %+v", i, e)
		}
	}
	if u := unpublished(t, db); u != 0 {
		t.Fatalf("после RunOnce не отмечено %d событий", u)
	}
	n, err = r.RunOnce(withTimeout(t))
	if err != nil || n != 0 || len(pub.got) != 3 {
		t.Fatalf("повторный RunOnce = (%d, %v), опубликовано всего %d, want (0, nil) и 3", n, err, len(pub.got))
	}
}

func TestRelayPublishError(t *testing.T) {
	db := openDB(t)
	for _, c := range []string{"a", "b", "c"} {
		if _, err := CreateOrder(withTimeout(t), db, c, 10); err != nil {
			t.Fatal(err)
		}
	}
	pub := &fakePub{failOn: 2}
	r := &Relay{DB: db, Pub: pub}
	n, err := r.RunOnce(withTimeout(t))
	if !errors.Is(err, errPub) || n != 1 {
		t.Fatalf("RunOnce при сбое брокера на 2-м событии = (%d, %v), want (1, errPub)", n, err)
	}
	if u := unpublished(t, db); u != 2 {
		t.Fatalf("после сбоя не отмечено %d событий, want 2", u)
	}
}
