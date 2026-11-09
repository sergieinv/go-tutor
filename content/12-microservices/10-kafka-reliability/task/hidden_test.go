package task

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

var errTransient = errors.New("склад недоступен")

func failing(calls *int, err error) func(context.Context, *kgo.Record) error {
	return func(context.Context, *kgo.Record) error {
		*calls++
		return err
	}
}

func TestDefaultMaxAttempts(t *testing.T) {
	for _, n := range []int{0, -1} {
		calls := 0
		dlq := &fakeDLQ{}
		p := &Processor{Handle: failing(&calls, errTransient), DLQ: dlq, DLQTopic: "d", MaxAttempts: n, Store: newStore()}
		out, err := p.Process(context.Background(), rec("e1"))
		if out != DeadLettered || err != nil || calls != 3 {
			t.Fatalf("MaxAttempts=%d: %v, %v, вызовов %d; want DeadLettered, nil, 3", n, out, err, calls)
		}
		if got := hdr(dlq.recs[0], HeaderDLQAttempts); got != "3" {
			t.Errorf("dlq-attempts = %q, want 3", got)
		}
		if got := hdr(dlq.recs[0], HeaderDLQReason); got != errTransient.Error() {
			t.Errorf("dlq-reason = %q", got)
		}
	}
}

func TestCustomMaxAttempts(t *testing.T) {
	calls := 0
	p := &Processor{Handle: failing(&calls, errTransient), DLQ: &fakeDLQ{}, DLQTopic: "d", MaxAttempts: 5, Store: newStore()}
	if out, _ := p.Process(context.Background(), rec("e1")); out != DeadLettered || calls != 5 {
		t.Fatalf("MaxAttempts=5: %v, вызовов %d", out, calls)
	}
}

func TestReasonIsLastError(t *testing.T) {
	calls := 0
	dlq := &fakeDLQ{}
	p := &Processor{
		Handle: func(context.Context, *kgo.Record) error {
			calls++
			if calls == 2 {
				return Permanent(fmt.Errorf("версия %d неизвестна", 7))
			}
			return errTransient
		},
		DLQ: dlq, DLQTopic: "d", MaxAttempts: 5, Store: newStore(),
	}
	if out, err := p.Process(context.Background(), rec("e1")); out != DeadLettered || err != nil {
		t.Fatalf("Process = %v, %v", out, err)
	}
	if calls != 2 || hdr(dlq.recs[0], HeaderDLQReason) != "версия 7 неизвестна" || hdr(dlq.recs[0], HeaderDLQAttempts) != "2" {
		t.Errorf("вызовов %d, reason %q, attempts %q; want 2, «версия 7 неизвестна», 2",
			calls, hdr(dlq.recs[0], HeaderDLQReason), hdr(dlq.recs[0], HeaderDLQAttempts))
	}
}

func TestHeadersOrderAndOriginalUntouched(t *testing.T) {
	dlq := &fakeDLQ{}
	r := rec("e9")
	r.Headers = append(make([]kgo.RecordHeader, 0, 16), // запас ёмкости — ловушка append
		kgo.RecordHeader{Key: "traceparent", Value: []byte("00-abc")},
		kgo.RecordHeader{Key: HeaderEventID, Value: []byte("e9")},
		kgo.RecordHeader{Key: HeaderEventID, Value: []byte("другой")},
	)
	orig := append([]kgo.RecordHeader(nil), r.Headers[:cap(r.Headers)]...)
	calls := 0
	p := &Processor{Handle: failing(&calls, Permanent(errors.New("плохо"))), DLQ: dlq, DLQTopic: "d", Store: newStore()}
	if out, err := p.Process(context.Background(), r); out != DeadLettered || err != nil {
		t.Fatalf("Process = %v, %v", out, err)
	}
	var keys []string
	for _, h := range dlq.recs[0].Headers {
		keys = append(keys, h.Key)
	}
	want := []string{"traceparent", HeaderEventID, HeaderEventID, HeaderDLQReason, HeaderDLQAttempts, HeaderDLQTopic, HeaderDLQPartition, HeaderDLQOffset}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("заголовки DLQ %v, want %v", keys, want)
	}
	if !reflect.DeepEqual(r.Headers[:cap(r.Headers)], orig) || len(r.Headers) != 3 {
		t.Errorf("исходная запись изменена: %v", r.Headers)
	}
	if got := newStoreFrom(p).marked; !reflect.DeepEqual(got, []string{"e9"}) {
		t.Errorf("отмечено %v, want [e9] — ключ из первого заголовка event-id", got)
	}
}

func newStoreFrom(p *Processor) *memStore { return p.Store.(*memStore) }

func TestNoEventID(t *testing.T) {
	for name, r := range map[string]*kgo.Record{
		"нет заголовка":   rec(""),
		"пустое значение": {Topic: "orders.events", Key: []byte("A-1"), Headers: []kgo.RecordHeader{{Key: HeaderEventID}}},
	} {
		dlq := &fakeDLQ{}
		store := newStore()
		calls := 0
		p := &Processor{Handle: failing(&calls, nil), DLQ: dlq, DLQTopic: "d", Store: store}
		out, err := p.Process(context.Background(), r)
		if out != DeadLettered || err != nil {
			t.Fatalf("%s: %v, %v; want DeadLettered, nil", name, out, err)
		}
		if calls != 0 || len(store.seenCalls) != 0 || len(store.marked) != 0 {
			t.Errorf("%s: Handle %d, Seen %v, MarkSeen %v — без event-id ничего не вызывается", name, calls, store.seenCalls, store.marked)
		}
		if len(dlq.recs) != 1 || hdr(dlq.recs[0], HeaderDLQReason) != ErrNoEventID.Error() || hdr(dlq.recs[0], HeaderDLQAttempts) != "0" {
			t.Errorf("%s: DLQ %v", name, dlq.recs)
		}
	}
}

func TestDLQFailureNotMarked(t *testing.T) {
	errDown := errors.New("брокер недоступен")
	store := newStore()
	calls := 0
	p := &Processor{Handle: failing(&calls, Permanent(errors.New("плохо"))), DLQ: &fakeDLQ{err: errDown}, DLQTopic: "d", Store: store}
	out, err := p.Process(context.Background(), rec("e1"))
	if out != 0 || !errors.Is(err, errDown) {
		t.Fatalf("Process = %v, %v; want 0 и ошибку отправки", out, err)
	}
	if len(store.marked) != 0 {
		t.Errorf("отмечено %v, хотя DLQ не принял запись", store.marked)
	}
}

func TestStoreErrors(t *testing.T) {
	errDB := errors.New("БД недоступна")
	store := newStore()
	store.seenErr = errDB
	calls := 0
	p := &Processor{Handle: failing(&calls, nil), DLQ: &fakeDLQ{}, DLQTopic: "d", Store: store}
	if out, err := p.Process(context.Background(), rec("e1")); out != 0 || !errors.Is(err, errDB) || calls != 0 {
		t.Errorf("ошибка Seen: %v, %v, вызовов %d; want 0, ошибку, 0", out, err, calls)
	}
	store = newStore()
	store.markErr = errDB
	p.Store = store
	if out, err := p.Process(context.Background(), rec("e1")); out != 0 || !errors.Is(err, errDB) {
		t.Errorf("ошибка MarkSeen: %v, %v; want 0 и ошибку", out, err)
	}
}

func TestCanceledContextStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dlq := &fakeDLQ{}
	store := newStore()
	calls := 0
	p := &Processor{
		Handle: func(context.Context, *kgo.Record) error {
			calls++
			cancel() // SIGTERM во время первой попытки
			return errTransient
		},
		DLQ: dlq, DLQTopic: "d", Store: store,
	}
	out, err := p.Process(ctx, rec("e1"))
	if out != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("Process = %v, %v; want 0 и context.Canceled", out, err)
	}
	if calls != 1 || len(dlq.recs) != 0 || len(store.marked) != 0 {
		t.Errorf("вызовов %d, в DLQ %d, отмечено %v; want 1, 0, [] — остановка не повод для DLQ", calls, len(dlq.recs), store.marked)
	}
}

func TestDLQOnKafka(t *testing.T) {
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "orders.events.dlq"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.ConsumeTopics("orders.events.dlq"), kgo.FetchMaxWait(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	calls := 0
	p := &Processor{Handle: failing(&calls, errTransient), DLQ: cl, DLQTopic: "orders.events.dlq", MaxAttempts: 2, Store: newStore()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := p.Process(ctx, rec("e5")); out != DeadLettered || err != nil {
		t.Fatalf("Process = %v, %v", out, err)
	}
	f := cl.PollFetches(ctx)
	if err := f.Err(); err != nil {
		t.Fatal(err)
	}
	recs := f.Records()
	if len(recs) != 1 || hdr(recs[0], HeaderDLQAttempts) != "2" || hdr(recs[0], HeaderEventID) != "e5" || string(recs[0].Key) != "A-1" {
		t.Fatalf("в топике DLQ: %v", recs)
	}
}
