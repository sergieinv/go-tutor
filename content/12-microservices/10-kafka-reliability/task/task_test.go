package task

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

// fakeDLQ запоминает отправленные записи; err — ошибка отправки.
type fakeDLQ struct {
	mu   sync.Mutex
	recs []*kgo.Record
	err  error
}

func (f *fakeDLQ) ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out kgo.ProduceResults
	for _, r := range rs {
		if f.err == nil {
			f.recs = append(f.recs, r)
		}
		out = append(out, kgo.ProduceResult{Record: r, Err: f.err})
	}
	return out
}

// memStore — таблица обработанных ключей в памяти.
type memStore struct {
	seen              map[string]bool
	seenErr, markErr  error
	seenCalls, marked []string
}

func newStore() *memStore { return &memStore{seen: map[string]bool{}} }

func (s *memStore) Seen(_ context.Context, id string) (bool, error) {
	s.seenCalls = append(s.seenCalls, id)
	if s.seenErr != nil {
		return false, s.seenErr
	}
	return s.seen[id], nil
}

func (s *memStore) MarkSeen(_ context.Context, id string) error {
	if s.markErr != nil {
		return s.markErr
	}
	s.marked = append(s.marked, id)
	s.seen[id] = true
	return nil
}

func rec(id string) *kgo.Record {
	r := &kgo.Record{Topic: "orders.events", Partition: 2, Offset: 41, Key: []byte("A-1"), Value: []byte(`{"type":"paid"}`)}
	if id != "" {
		r.Headers = []kgo.RecordHeader{{Key: HeaderEventID, Value: []byte(id)}}
	}
	return r
}

func hdr(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func TestProcessedThenDuplicate(t *testing.T) {
	calls := 0
	store := newStore()
	p := &Processor{
		Handle:   func(context.Context, *kgo.Record) error { calls++; return nil },
		DLQ:      &fakeDLQ{},
		DLQTopic: "orders.events.dlq",
		Store:    store,
	}
	out, err := p.Process(context.Background(), rec("e1"))
	if out != Processed || err != nil {
		t.Fatalf("Process = %v, %v; want Processed, nil", out, err)
	}
	out, err = p.Process(context.Background(), rec("e1"))
	if out != Duplicate || err != nil {
		t.Fatalf("повтор: Process = %v, %v; want Duplicate, nil", out, err)
	}
	if calls != 1 {
		t.Errorf("Handle вызван %d раз, want 1", calls)
	}
}

func TestTransientErrorRetried(t *testing.T) {
	calls := 0
	p := &Processor{
		Handle: func(context.Context, *kgo.Record) error {
			calls++
			if calls < 3 {
				return errors.New("склад недоступен")
			}
			return nil
		},
		DLQ:      &fakeDLQ{},
		DLQTopic: "orders.events.dlq",
		Store:    newStore(),
	}
	out, err := p.Process(context.Background(), rec("e1"))
	if out != Processed || err != nil || calls != 3 {
		t.Fatalf("Process = %v, %v, вызовов %d; want Processed, nil, 3", out, err, calls)
	}
}

func TestPermanentGoesToDLQ(t *testing.T) {
	dlq := &fakeDLQ{}
	store := newStore()
	calls := 0
	p := &Processor{
		Handle: func(context.Context, *kgo.Record) error {
			calls++
			return Permanent(errors.New("битый JSON"))
		},
		DLQ:      dlq,
		DLQTopic: "orders.events.dlq",
		Store:    store,
	}
	out, err := p.Process(context.Background(), rec("e2"))
	if out != DeadLettered || err != nil {
		t.Fatalf("Process = %v, %v; want DeadLettered, nil", out, err)
	}
	if calls != 1 {
		t.Errorf("постоянная ошибка: Handle вызван %d раз, want 1", calls)
	}
	if len(dlq.recs) != 1 {
		t.Fatalf("в DLQ %d записей, want 1", len(dlq.recs))
	}
	d := dlq.recs[0]
	if d.Topic != "orders.events.dlq" || string(d.Key) != "A-1" || string(d.Value) != `{"type":"paid"}` {
		t.Errorf("DLQ-запись: topic=%q key=%q value=%q", d.Topic, d.Key, d.Value)
	}
	for k, want := range map[string]string{
		HeaderEventID: "e2", HeaderDLQReason: "битый JSON", HeaderDLQAttempts: "1",
		HeaderDLQTopic: "orders.events", HeaderDLQPartition: "2", HeaderDLQOffset: "41",
	} {
		if got := hdr(d, k); got != want {
			t.Errorf("заголовок %s = %q, want %q", k, got, want)
		}
	}
	if out, _ := p.Process(context.Background(), rec("e2")); out != Duplicate {
		t.Errorf("повторная доставка после DLQ: %v, want Duplicate", out)
	}
}
