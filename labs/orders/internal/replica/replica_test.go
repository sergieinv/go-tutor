package replica_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/replica"
)

// node — база в памяти: заказы покупателей и позиция WAL. Реплика — та
// же структура, которая «применила» заказы до своей позиции.
type node struct {
	mu        sync.Mutex
	orders    []orders.Order
	lsn       uint64 // основная: позиция записи; реплика: применённая
	err       error
	reads     atomic.Int64
	positions atomic.Int64
}

func (n *node) ListOrders(_ context.Context, customerID, _ int64, _ int) ([]orders.Order, error) {
	n.reads.Add(1)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return nil, n.err
	}
	var out []orders.Order
	for _, o := range n.orders {
		if o.CustomerID == customerID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (n *node) WALPosition(context.Context) (uint64, error) {
	n.positions.Add(1)
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.lsn, n.err
}

func (n *node) ReplayPosition(ctx context.Context) (uint64, error) { return n.WALPosition(ctx) }

// write — запись на основной: заказ и сдвиг позиции.
func (n *node) write(o orders.Order) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.orders = append(n.orders, o)
	n.lsn += 100
}

// catchUp — реплика применила всё, что есть на основной.
func (n *node) catchUp(primary *node) {
	primary.mu.Lock()
	defer primary.mu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	n.orders = append([]orders.Order(nil), primary.orders...)
	n.lsn = primary.lsn
}

type harness struct {
	primary, replica *node
	enabled          atomic.Bool
	reg              *prometheus.Registry
	h                *replica.History
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	x := &harness{primary: &node{lsn: 1000}, replica: &node{lsn: 1000}, reg: prometheus.NewRegistry()}
	x.enabled.Store(true)
	h, err := replica.New(replica.Config{
		Primaries:  []replica.Primary{x.primary},
		Replica:    x.replica,
		Enabled:    x.enabled.Load,
		Registerer: x.reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	x.h = h
	return x
}

func (x *harness) list(t *testing.T, tok orders.Position) []orders.Order {
	t.Helper()
	list, err := x.h.ListOrders(t.Context(), 7, 0, 10, tok)
	if err != nil {
		t.Fatalf("ListOrders(%q): %v", tok, err)
	}
	return list
}

func (x *harness) reads(target, reason string) float64 {
	mfs, _ := x.reg.Gather()
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["target"] == target && labels["reason"] == reason {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// TestReadYourWrites — главный сценарий: заказ записан на основной,
// реплика отстаёт. Без токена история с реплики его не содержит, с
// токеном — содержит (чтение ушло на основную); когда реплика догнала —
// токен пропускает на реплику.
func TestReadYourWrites(t *testing.T) {
	x := newHarness(t)
	x.primary.write(orders.Order{ID: 1, CustomerID: 7})
	tok := x.h.WritePosition(t.Context(), 7)
	if tok != "lsn:0/44C" { // 1100 = 0x44C
		t.Fatalf("token = %q, want lsn:0/44C", tok)
	}

	if got := x.list(t, ""); len(got) != 0 {
		t.Errorf("without token: %d orders, want 0 (stale replica)", len(got))
	}
	if got := x.list(t, tok); len(got) != 1 {
		t.Errorf("with token: %d orders, want 1 (read-your-writes)", len(got))
	}
	if x.reads("primary", "lagging") != 1 || x.reads("replica", "no_token") != 1 {
		t.Errorf("reads: primary/lagging=%v replica/no_token=%v, want 1 and 1",
			x.reads("primary", "lagging"), x.reads("replica", "no_token"))
	}

	x.replica.catchUp(x.primary)
	if got := x.list(t, tok); len(got) != 1 {
		t.Errorf("caught up: %d orders, want 1", len(got))
	}
	if x.reads("replica", "caught_up") != 1 {
		t.Errorf("replica/caught_up = %v, want 1", x.reads("replica", "caught_up"))
	}

	// Позиция реплики закеширована: тот же токен не требует запроса
	// позиции к реплике.
	before := x.replica.positions.Load()
	x.list(t, tok)
	if x.replica.positions.Load() != before {
		t.Error("replay position queried again for an already satisfied token")
	}
}

func TestFallbacks(t *testing.T) {
	x := newHarness(t)
	x.primary.write(orders.Order{ID: 1, CustomerID: 7})

	// Выключено переключателем.
	x.enabled.Store(false)
	if got := x.list(t, ""); len(got) != 1 {
		t.Errorf("disabled: %d orders, want 1 from primary", len(got))
	}
	x.enabled.Store(true)

	// Реплика сломалась — читаем с основной, ошибки клиенту нет.
	x.replica.err = errors.New("connection refused")
	if got := x.list(t, ""); len(got) != 1 {
		t.Errorf("replica down: %d orders, want 1 from primary", len(got))
	}
	if got := x.list(t, "lsn:0/44C"); len(got) != 1 {
		t.Errorf("replica down with token: %d orders, want 1", len(got))
	}
	x.replica.err = nil

	// Основная не назвала позицию — токен «только основная».
	x.primary.err = errors.New("boom")
	tok := x.h.WritePosition(t.Context(), 7)
	x.primary.err = nil
	if tok != "primary" {
		t.Fatalf("token on error = %q, want primary", tok)
	}
	if got := x.list(t, tok); len(got) != 1 {
		t.Errorf("forced primary: %d orders, want 1", len(got))
	}

	for _, reason := range []string{"disabled", "replica_error", "forced"} {
		if x.reads("primary", reason) == 0 {
			t.Errorf("primary/%s = 0, want > 0", reason)
		}
	}

	for _, bad := range []orders.Position{"lsn:zz", "lsn:1", "v2:abc"} {
		_, err := x.h.ListOrders(t.Context(), 7, 0, 10, bad)
		var ve *orders.ValidationError
		if !errors.As(err, &ve) || ve.Field != "consistency_token" {
			t.Errorf("token %q: err = %v, want ValidationError", bad, err)
		}
	}
}

// TestShards: реплика есть только у шарда 0; покупатели шарда 1 читают с
// его основной и токен не получают.
func TestShards(t *testing.T) {
	p0, p1, r0 := &node{lsn: 1}, &node{lsn: 1}, &node{lsn: 1}
	h, err := replica.New(replica.Config{
		Primaries: []replica.Primary{p0, p1},
		Replica:   r0,
		ShardOf:   func(c int64) int { return int(c % 2) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok := h.WritePosition(t.Context(), 1); tok != "" {
		t.Errorf("shard 1 token = %q, want empty", tok)
	}
	if _, err := h.ListOrders(t.Context(), 1, 0, 10, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ListOrders(t.Context(), 2, 0, 10, ""); err != nil {
		t.Fatal(err)
	}
	if p1.reads.Load() != 1 || r0.reads.Load() != 1 || p0.reads.Load() != 0 {
		t.Errorf("reads p0/p1/r0 = %d/%d/%d, want 0/1/1", p0.reads.Load(), p1.reads.Load(), r0.reads.Load())
	}
}
