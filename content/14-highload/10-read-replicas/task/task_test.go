package task

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

// fakeDB — фейковый сервер со счётчиками обращений. lsn — до какой
// позиции проигран WAL (для реплики), err — чем отвечать.
type fakeDB struct {
	name    string
	orders  []Order
	lsn     LSN
	err     error
	lsnErr  error
	mu      sync.Mutex
	history int
	replay  int
	onCall  func() // вызывается при каждом обращении (например, cancel)
}

func (f *fakeDB) History(ctx context.Context, customerID int64) ([]Order, error) {
	f.mu.Lock()
	f.history++
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.orders), nil
}

func (f *fakeDB) ReplayLSN(ctx context.Context) (LSN, error) {
	f.mu.Lock()
	f.replay++
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall()
	}
	return f.lsn, f.lsnErr
}

func (f *fakeDB) calls() (history, replay int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.history, f.replay
}

func TestParseLSN(t *testing.T) {
	cases := []struct {
		in   string
		want LSN
	}{
		{"0/0", 0},
		{"0/3DF8B18", 0x3DF8B18},
		{"16/B374D848", 0x16_B374D848},
		{"1/0", 1 << 32},
	}
	for _, c := range cases {
		got, err := ParseLSN(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseLSN(%q) = %v, %v; want %d", c.in, uint64(got), err, uint64(c.want))
		}
	}
	for _, bad := range []string{"", "0", "/1", "1/", "x/1"} {
		if _, err := ParseLSN(bad); !errors.Is(err, ErrBadLSN) {
			t.Errorf("ParseLSN(%q): err = %v, want ErrBadLSN", bad, err)
		}
	}
}

func TestLSNString(t *testing.T) {
	if got := LSN(0x16_B374D848).String(); got != "16/B374D848" {
		t.Fatalf("String = %q, want 16/B374D848", got)
	}
}

func TestRouterFreshReplica(t *testing.T) {
	primary := &fakeDB{name: "primary", orders: []Order{{1, 100}, {2, 200}}}
	replica := &fakeDB{name: "replica", orders: []Order{{1, 100}, {2, 200}}, lsn: 500}
	r := NewRouter(primary, replica)
	orders, fromReplica, err := r.History(context.Background(), 7, 400)
	if err != nil || !fromReplica || len(orders) != 2 {
		t.Fatalf("History = %v, %v, %v; want 2 заказа с реплики", orders, fromReplica, err)
	}
	if h, _ := primary.calls(); h != 0 {
		t.Fatalf("основной вызван %d раз, want 0", h)
	}
}

func TestRouterLaggingReplica(t *testing.T) {
	primary := &fakeDB{orders: []Order{{1, 100}, {2, 200}, {3, 300}}}
	replica := &fakeDB{orders: []Order{{1, 100}, {2, 200}}, lsn: 300}
	r := NewRouter(primary, replica)
	orders, fromReplica, err := r.History(context.Background(), 7, 400)
	if err != nil || fromReplica || len(orders) != 3 {
		t.Fatalf("History = %v, %v, %v; want 3 заказа с основного", orders, fromReplica, err)
	}
	if h, _ := replica.calls(); h != 0 {
		t.Fatalf("History отстающей реплики вызван %d раз, want 0", h)
	}
}

func TestRouterNoReplicas(t *testing.T) {
	primary := &fakeDB{orders: []Order{{1, 100}}}
	r := NewRouter(primary)
	for _, token := range []LSN{0, 10} {
		orders, fromReplica, err := r.History(context.Background(), 7, token)
		if err != nil || fromReplica || len(orders) != 1 {
			t.Fatalf("token %d: History = %v, %v, %v; want заказ с основного", token, orders, fromReplica, err)
		}
	}
}
