package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	var err error
	switch table { // имена таблиц — только из этого списка
	case "orders":
		err = db.QueryRow(`SELECT count(*) FROM orders`).Scan(&n)
	case "outbox":
		err = db.QueryRow(`SELECT count(*) FROM outbox`).Scan(&n)
	default:
		t.Fatalf("неизвестная таблица %q", table)
	}
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func ids(events []Event) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func createN(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	for i := range n {
		if _, err := CreateOrder(withTimeout(t), db, fmt.Sprintf("c%d", i+1), int64(i+1)); err != nil {
			t.Fatalf("CreateOrder #%d = %v", i+1, err)
		}
	}
}

func TestCreateOrderOutboxFailureRollsBack(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `CREATE TRIGGER outbox_down BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT, 'outbox недоступен'); END`)
	if _, err := CreateOrder(withTimeout(t), db, "Анна", 300); err == nil {
		t.Fatal("CreateOrder вернул nil, хотя запись события упала")
	}
	if n := countRows(t, db, "orders"); n != 0 {
		t.Fatalf("запись события упала, а заказ остался (%d) — заказ и событие должны быть в одной транзакции", n)
	}
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после ошибки занято соединений: %d — транзакция не откатилась", n)
	}
	mustExec(t, db, `DROP TRIGGER outbox_down`)
	if _, err := CreateOrder(withTimeout(t), db, "Анна", 300); err != nil {
		t.Fatalf("CreateOrder после восстановления = %v", err)
	}
	if countRows(t, db, "orders") != 1 || countRows(t, db, "outbox") != 1 {
		t.Fatal("после восстановления должны быть 1 заказ и 1 событие")
	}
}

func TestCreateOrderOrdersFailureRollsBack(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `CREATE TRIGGER orders_down BEFORE INSERT ON orders BEGIN SELECT RAISE(ABORT, 'orders недоступен'); END`)
	if _, err := CreateOrder(withTimeout(t), db, "Анна", 300); err == nil {
		t.Fatal("CreateOrder вернул nil, хотя вставка заказа упала")
	}
	if n := countRows(t, db, "outbox"); n != 0 {
		t.Fatalf("заказ не создан, а событие о нём осталось (%d)", n)
	}
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после ошибки занято соединений: %d", n)
	}
}

func TestCreateOrderInvalidWritesNothing(t *testing.T) {
	db := openDB(t)
	for _, tc := range []struct {
		customer string
		total    int64
	}{{"", 100}, {"Анна", 0}, {"Анна", -5}} {
		if id, err := CreateOrder(withTimeout(t), db, tc.customer, tc.total); !errors.Is(err, ErrInvalidOrder) || id != 0 {
			t.Errorf("CreateOrder(%q, %d) = (%d, %v), want (0, ErrInvalidOrder)", tc.customer, tc.total, id, err)
		}
	}
	if countRows(t, db, "orders") != 0 || countRows(t, db, "outbox") != 0 {
		t.Fatal("неверный заказ что-то записал")
	}
}

func TestCreateOrderPayload(t *testing.T) {
	db := openDB(t)
	createN(t, db, 2)
	customer := `Ольга "О'Нил" <script>`
	id, err := CreateOrder(withTimeout(t), db, customer, 777)
	if err != nil || id != 3 {
		t.Fatalf("CreateOrder = (%d, %v), want (3, nil)", id, err)
	}
	var key string
	var payload []byte
	if err := db.QueryRow(`SELECT key, payload FROM outbox WHERE id = 3`).Scan(&key, &payload); err != nil {
		t.Fatal(err)
	}
	var body OrderCreated
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("payload не JSON: %v", err)
	}
	if key != "3" || body != (OrderCreated{OrderID: 3, Customer: customer, Total: 777}) {
		t.Fatalf("событие заказа 3: key=%q body=%+v", key, body)
	}
	var stored string
	db.QueryRow(`SELECT customer FROM orders WHERE id = 3`).Scan(&stored)
	if stored != customer {
		t.Fatalf("в orders записан покупатель %q", stored)
	}
}

func TestRelayResumesInOrder(t *testing.T) {
	db := openDB(t)
	createN(t, db, 4)
	pub := &fakePub{failOn: 2}
	r := &Relay{DB: db, Pub: pub}
	if n, err := r.RunOnce(withTimeout(t)); n != 1 || !errors.Is(err, errPub) {
		t.Fatalf("RunOnce = (%d, %v), want (1, errPub)", n, err)
	}
	if pub.calls != 2 {
		t.Fatalf("после сбоя на событии 2 Publish вызван %d раз, want 2 — следующие события нельзя публиковать раньше упавшего", pub.calls)
	}
	n, err := r.RunOnce(withTimeout(t))
	if err != nil || n != 3 {
		t.Fatalf("RunOnce после восстановления = (%d, %v), want (3, nil)", n, err)
	}
	if got := ids(pub.got); !slices.Equal(got, []int64{1, 2, 3, 4}) {
		t.Fatalf("брокер получил события %v, want [1 2 3 4] — без потерь и по порядку", got)
	}
	for _, e := range pub.got {
		var body OrderCreated
		if err := json.Unmarshal(e.Payload, &body); err != nil || body.OrderID != e.ID || e.Key != fmt.Sprint(e.ID) || e.Topic != TopicOrderCreated {
			t.Fatalf("событие передано не целиком: %+v (%v)", e, err)
		}
	}
}

func TestRelayFirstEventFails(t *testing.T) {
	db := openDB(t)
	createN(t, db, 2)
	pub := &fakePub{failOn: 1}
	r := &Relay{DB: db, Pub: pub}
	n, err := r.RunOnce(withTimeout(t))
	if n != 0 || !errors.Is(err, errPub) {
		t.Fatalf("RunOnce = (%d, %v), want (0, errPub)", n, err)
	}
	if u := unpublished(t, db); u != 2 {
		t.Fatalf("не отмечено %d событий, want 2", u)
	}
}

func TestRelayMarkFailureRepublishes(t *testing.T) {
	db := openDB(t)
	createN(t, db, 1)
	mustExec(t, db, `CREATE TRIGGER mark_down BEFORE UPDATE ON outbox BEGIN SELECT RAISE(ABORT, 'отметка не удалась'); END`)
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub}
	n, err := r.RunOnce(withTimeout(t))
	if err == nil || n != 0 {
		t.Fatalf("RunOnce при сбое отметки = (%d, %v), want (0, ошибка)", n, err)
	}
	mustExec(t, db, `DROP TRIGGER mark_down`)
	if n, err := r.RunOnce(withTimeout(t)); err != nil || n != 1 {
		t.Fatalf("RunOnce после восстановления = (%d, %v), want (1, nil)", n, err)
	}
	// At-least-once: неотмеченное событие уходит повторно.
	if got := ids(pub.got); !slices.Equal(got, []int64{1, 1}) {
		t.Fatalf("брокер получил %v, want [1 1]", got)
	}
}

func TestRelayBatchSize(t *testing.T) {
	db := openDB(t)
	createN(t, db, 5)
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub, BatchSize: 2}
	for _, want := range []int{2, 2, 1, 0} {
		if n, err := r.RunOnce(withTimeout(t)); err != nil || n != want {
			t.Fatalf("RunOnce с BatchSize 2 = (%d, %v), want (%d, nil)", n, err, want)
		}
	}
	if got := ids(pub.got); !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("брокер получил %v", got)
	}
}

func TestRelayDefaultBatchSize(t *testing.T) {
	db := openDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 120 {
		if _, err := tx.Exec(`INSERT INTO outbox (topic, key, payload) VALUES ('t', ?, '{}')`, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, -1} {
		mustExec(t, db, `UPDATE outbox SET published_at = NULL`)
		pub := &fakePub{}
		r := &Relay{DB: db, Pub: pub, BatchSize: size}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // 100 отметок — 100 коммитов
		n, err := r.RunOnce(ctx)
		cancel()
		if err != nil || n != 100 {
			t.Fatalf("RunOnce с BatchSize %d = (%d, %v), want (100, nil)", size, n, err)
		}
	}
}

func TestRelaySkipsPublished(t *testing.T) {
	db := openDB(t)
	createN(t, db, 3)
	mustExec(t, db, `UPDATE outbox SET published_at = '2026-01-01 00:00:00' WHERE id = 2`)
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub}
	if n, err := r.RunOnce(withTimeout(t)); err != nil || n != 2 {
		t.Fatalf("RunOnce = (%d, %v), want (2, nil)", n, err)
	}
	if got := ids(pub.got); !slices.Equal(got, []int64{1, 3}) {
		t.Fatalf("брокер получил %v, want [1 3]", got)
	}
	var ts string
	db.QueryRow(`SELECT published_at FROM outbox WHERE id = 2`).Scan(&ts)
	if ts != "2026-01-01 00:00:00" {
		t.Fatalf("отметка уже опубликованного события изменилась: %q", ts)
	}
}

func TestRelayEmpty(t *testing.T) {
	db := openDB(t)
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub}
	if n, err := r.RunOnce(withTimeout(t)); err != nil || n != 0 || pub.calls != 0 {
		t.Fatalf("RunOnce на пустом outbox = (%d, %v), Publish вызван %d раз", n, err, pub.calls)
	}
}

// dbPub — публикатор, который сам ходит в ту же базу: если relay
// держит соединение (незакрытые Rows, открытая транзакция) во время
// публикации, запрос упадёт по таймауту.
type dbPub struct {
	db   *sql.DB
	errs []error
	got  []Event
}

func (p *dbPub) Publish(ctx context.Context, e Event) error {
	qctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var n int
	if err := p.db.QueryRowContext(qctx, `SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		p.errs = append(p.errs, err)
		return err
	}
	p.got = append(p.got, e)
	return nil
}

func TestRelayReleasesConnectionDuringPublish(t *testing.T) {
	db := openDB(t)
	createN(t, db, 2)
	pub := &dbPub{db: db}
	r := &Relay{DB: db, Pub: pub}
	n, err := r.RunOnce(withTimeout(t))
	if len(pub.errs) > 0 {
		t.Fatalf("публикатор не смог обратиться к базе: %v — во время Publish relay держит единственное соединение", pub.errs[0])
	}
	if err != nil || n != 2 {
		t.Fatalf("RunOnce = (%d, %v), want (2, nil)", n, err)
	}
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после RunOnce занято соединений: %d", n)
	}
}

func TestRelayCanceled(t *testing.T) {
	db := openDB(t)
	createN(t, db, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pub := &fakePub{}
	r := &Relay{DB: db, Pub: pub}
	if _, err := r.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce с отменённым контекстом = %v, want context.Canceled", err)
	}
	if pub.calls != 0 {
		t.Fatalf("с отменённым контекстом Publish вызван %d раз", pub.calls)
	}
	if u := unpublished(t, db); u != 2 {
		t.Fatalf("с отменённым контекстом отмечено %d событий", 2-u)
	}
}
