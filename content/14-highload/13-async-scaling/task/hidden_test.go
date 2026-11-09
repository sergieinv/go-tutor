package task

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
)

// stream — события n заказов в правильном порядке: created, позиции,
// иногда оплата или отмена. Детерминированно по seed.
func stream(seed uint64, n int) []Event {
	r := rand.New(rand.NewPCG(seed, 7))
	var out []Event
	customers := []string{"anna", "boris", "vera"}
	for i := range n {
		id := fmt.Sprintf("o%02d", i)
		out = append(out, created(id, customers[r.IntN(len(customers))]))
		seq := 2
		for range r.IntN(4) {
			out = append(out, item(id, seq, int64(1+r.IntN(500))))
			seq++
		}
		switch r.IntN(3) {
		case 1:
			out = append(out, Event{OrderID: id, Seq: seq, Kind: "paid"})
		case 2:
			out = append(out, Event{OrderID: id, Seq: seq, Kind: "cancelled"})
		}
	}
	return out
}

// reference — модель чтения при доставке ровно один раз по порядку.
func reference(events []Event) map[string]OrderView {
	m := map[string]OrderView{}
	for _, e := range events {
		v := m[e.OrderID]
		switch e.Kind {
		case "created":
			v = OrderView{OrderID: e.OrderID, Customer: e.Customer, Status: "new"}
		case "item_added":
			v.Total += e.Amount
		case "paid":
			v.Status = "paid"
		case "cancelled":
			v.Status = "cancelled"
		}
		v.Seq = e.Seq
		m[e.OrderID] = v
	}
	return m
}

// delivery — at-least-once: каждое событие хотя бы раз, часть — повторно,
// всё перемешано. Возвращает доставки и число повторов.
func delivery(seed uint64, events []Event) ([]Event, int) {
	r := rand.New(rand.NewPCG(seed, 11))
	out := append([]Event(nil), events...)
	dups := 0
	for _, e := range events {
		for r.IntN(3) == 0 {
			out = append(out, e)
			dups++
		}
	}
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, dups
}

func compare(t *testing.T, p *Projection, want map[string]OrderView, events int, dups int) {
	t.Helper()
	for id, w := range want {
		got, ok := p.Order(id)
		if !ok || got != w {
			t.Fatalf("Order(%s) = %+v, %v; want %+v", id, got, ok, w)
		}
	}
	if s := p.Stats(); s != (Stats{Applied: events, Duplicates: dups}) {
		t.Fatalf("Stats = %+v, want Applied %d, Duplicates %d, Pending 0", s, events, dups)
	}
}

func TestShuffledWithDuplicates(t *testing.T) {
	for seed := range uint64(50) {
		events := stream(seed, 20)
		got, dups := delivery(seed, events)
		p := NewProjection()
		mustApply(t, p, got...)
		compare(t, p, reference(events), len(events), dups)
	}
}

// Проекцию кормят несколько горутин сразу — итог тот же, гонок нет.
func TestConcurrent(t *testing.T) {
	for seed := range uint64(5) {
		events := stream(100+seed, 40)
		got, dups := delivery(seed, events)
		p := NewProjection()
		var wg sync.WaitGroup
		const workers = 8
		for w := range workers {
			wg.Go(func() {
				for i := w; i < len(got); i += workers {
					if err := p.Apply(got[i]); err != nil {
						t.Errorf("Apply: %v", err)
					}
					p.Order(got[i].OrderID)
					p.ByCustomer("anna")
				}
			})
		}
		wg.Wait()
		compare(t, p, reference(events), len(events), dups)
	}
}

func TestPendingDuplicate(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"), item("o1", 3, 10), item("o1", 3, 999))
	if s := p.Stats(); s != (Stats{Applied: 1, Duplicates: 1, Pending: 1}) {
		t.Fatalf("Stats = %+v, want Applied 1, Duplicates 1, Pending 1", s)
	}
	v, _ := p.Order("o1")
	if v.Seq != 1 || v.Total != 0 {
		t.Fatalf("до прихода №2 Order = %+v: отложенное событие не должно применяться", v)
	}
	mustApply(t, p, item("o1", 2, 5))
	v, _ = p.Order("o1")
	if v.Seq != 3 || v.Total != 15 {
		t.Fatalf("Order = %+v, want Seq 3, Total 15 (первое из повторов №3 побеждает)", v)
	}
	if s := p.Stats(); s != (Stats{Applied: 3, Duplicates: 1}) {
		t.Fatalf("Stats = %+v", s)
	}
}

func TestLateDuplicate(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"), item("o1", 2, 10), item("o1", 3, 20),
		created("o1", "boris"), item("o1", 2, 10))
	v, _ := p.Order("o1")
	if v.Customer != "anna" || v.Total != 30 {
		t.Fatalf("Order = %+v: повторы created и №2 должны отброситься", v)
	}
	if s := p.Stats(); s.Duplicates != 2 {
		t.Fatalf("Duplicates = %d, want 2", s.Duplicates)
	}
}

func TestBadEventDetails(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"))
	bad := []Event{
		{OrderID: "o2", Seq: 2, Kind: "created", Customer: "anna"},
		{OrderID: "o2", Seq: 1, Kind: "created"},
		{OrderID: "o2", Seq: 1, Kind: "item_added", Amount: 5},
		{OrderID: "o2", Seq: 1, Kind: "paid"},
		{OrderID: "o1", Seq: -1, Kind: "paid"},
		{OrderID: "o1", Seq: 2, Kind: ""},
		{OrderID: "o1", Seq: 2, Kind: "Paid"},
	}
	for _, e := range bad {
		if err := p.Apply(e); !errors.Is(err, ErrBadEvent) {
			t.Errorf("Apply(%+v) = %v, want ErrBadEvent", e, err)
		}
	}
	if s := p.Stats(); s != (Stats{Applied: 1}) {
		t.Fatalf("плохие события изменили счётчики: %+v", s)
	}
	if _, ok := p.Order("o2"); ok {
		t.Fatal("плохое событие создало заказ")
	}
}

func TestOrdersIndependent(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"), item("o1", 3, 10), created("o2", "anna"), item("o2", 2, 7),
		Event{OrderID: "o2", Seq: 3, Kind: "cancelled"})
	v, _ := p.Order("o2")
	if v.Status != "cancelled" || v.Total != 7 {
		t.Fatalf("o2 = %+v: дыра в o1 не должна задерживать o2", v)
	}
	if s := p.Stats(); s.Pending != 1 || s.Applied != 4 {
		t.Fatalf("Stats = %+v, want Applied 4, Pending 1", s)
	}
}

func TestCopies(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"), created("o2", "anna"))
	list := p.ByCustomer("anna")
	if len(list) != 2 {
		t.Fatalf("ByCustomer(anna) = %+v, want 2 заказа", list)
	}
	list[0].Total = 1_000_000
	v, _ := p.Order("o1")
	v.Status = "paid"
	again, _ := p.Order("o1")
	if again.Total != 0 || again.Status != "new" {
		t.Fatalf("изменение возвращённых значений изменило проекцию: %+v", again)
	}
	if got := p.ByCustomer("nobody"); len(got) != 0 {
		t.Fatalf("ByCustomer(nobody) = %+v, want пусто", got)
	}
	if _, ok := p.Order("nope"); ok {
		t.Fatal("Order(nope) нашёл заказ")
	}
}
