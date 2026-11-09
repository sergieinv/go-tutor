package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// recPublisher запоминает события; fail — ошибка, которую вернуть.
type recPublisher struct {
	mu     sync.Mutex
	events []Event
	fail   error
}

func (p *recPublisher) Publish(ctx context.Context, e Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	p.events = append(p.events, e)
	return nil
}

func (p *recPublisher) list() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.events)
}

// seqIDs выдаёт o-1, o-2, … и считает вызовы.
type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) next() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("o-%d", s.n)
}

func (s *seqIDs) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func TestPlaceOrder(t *testing.T) {
	stock := NewMemStock(map[string]int{"tea": 5, "cup": 2})
	pub := &recPublisher{}
	ids := &seqIDs{}
	svc := NewService(stock, pub, ids.next)

	o, err := svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 2}, {"cup", 1}})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	want := Order{ID: "o-1", Customer: "alice", Lines: []Line{{"cup", 1}, {"tea", 2}}}
	if o.ID != want.ID || o.Customer != want.Customer || !slices.Equal(o.Lines, want.Lines) {
		t.Fatalf("PlaceOrder = %+v, want %+v", o, want)
	}
	if got := stock.Available("tea"); got != 3 {
		t.Errorf("Available(tea) = %d, want 3", got)
	}
	if got := stock.Available("cup"); got != 1 {
		t.Errorf("Available(cup) = %d, want 1", got)
	}
	if ev := pub.list(); !slices.Equal(ev, []Event{{"order.created", "o-1", "alice"}}) {
		t.Errorf("события = %+v, want [{order.created o-1 alice}]", ev)
	}
	got, ok := svc.Get("o-1")
	if !ok || got.ID != "o-1" || !slices.Equal(got.Lines, want.Lines) {
		t.Errorf("Get(o-1) = %+v, %v", got, ok)
	}
}

func TestPlaceOrderOutOfStock(t *testing.T) {
	stock := NewMemStock(map[string]int{"tea": 5, "cup": 2})
	pub := &recPublisher{}
	svc := NewService(stock, pub, (&seqIDs{}).next)

	_, err := svc.PlaceOrder(context.Background(), "bob", []Line{{"tea", 1}, {"cup", 5}})
	if !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("err = %v, want ErrOutOfStock", err)
	}
	if stock.Available("tea") != 5 || stock.Available("cup") != 2 {
		t.Errorf("остаток изменился: tea=%d cup=%d", stock.Available("tea"), stock.Available("cup"))
	}
	if len(pub.list()) != 0 {
		t.Errorf("при отказе склада событие опубликовано: %+v", pub.list())
	}
	if _, ok := svc.Get("o-1"); ok {
		t.Error("заказ сохранён, хотя резерв не удался")
	}
}

func TestMemStock(t *testing.T) {
	ctx := context.Background()
	m := NewMemStock(map[string]int{"tea": 3})
	if err := m.Reserve(ctx, "a", []Line{{"tea", 2}}); err != nil {
		t.Fatalf("Reserve(a): %v", err)
	}
	if err := m.Reserve(ctx, "b", []Line{{"tea", 2}}); !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("Reserve(b) err = %v, want ErrOutOfStock", err)
	}
	if err := m.Release(ctx, "a"); err != nil {
		t.Fatalf("Release(a): %v", err)
	}
	if got := m.Available("tea"); got != 3 {
		t.Errorf("после Release Available(tea) = %d, want 3", got)
	}
	if got := m.Available("nope"); got != 0 {
		t.Errorf("Available(nope) = %d, want 0", got)
	}
}
