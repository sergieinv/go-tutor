package task

import (
	"errors"
	"testing"
)

func created(order, customer string) Event {
	return Event{OrderID: order, Seq: 1, Kind: "created", Customer: customer}
}

func item(order string, seq int, amount int64) Event {
	return Event{OrderID: order, Seq: seq, Kind: "item_added", Amount: amount}
}

func mustApply(t *testing.T, p *Projection, events ...Event) {
	t.Helper()
	for _, e := range events {
		if err := p.Apply(e); err != nil {
			t.Fatalf("Apply(%+v): %v", e, err)
		}
	}
}

func TestInOrder(t *testing.T) {
	p := NewProjection()
	mustApply(t, p,
		created("o1", "anna"),
		item("o1", 2, 100),
		item("o1", 3, 50),
		Event{OrderID: "o1", Seq: 4, Kind: "paid"},
	)
	got, ok := p.Order("o1")
	want := OrderView{OrderID: "o1", Customer: "anna", Status: "paid", Total: 150, Seq: 4}
	if !ok || got != want {
		t.Fatalf("Order = %+v, %v; want %+v", got, ok, want)
	}
	if s := p.Stats(); s != (Stats{Applied: 4}) {
		t.Fatalf("Stats = %+v", s)
	}
}

func TestDuplicateIgnored(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o1", "anna"), item("o1", 2, 100), item("o1", 2, 100))
	got, _ := p.Order("o1")
	if got.Total != 100 {
		t.Fatalf("Total = %d, want 100: повтор применён", got.Total)
	}
	if s := p.Stats(); s.Duplicates != 1 || s.Applied != 2 {
		t.Fatalf("Stats = %+v, want Applied 2, Duplicates 1", s)
	}
}

func TestOutOfOrder(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, item("o1", 2, 100))
	if _, ok := p.Order("o1"); ok {
		t.Fatal("заказ виден до события created")
	}
	if s := p.Stats(); s.Pending != 1 {
		t.Fatalf("Pending = %d, want 1", s.Pending)
	}
	mustApply(t, p, created("o1", "anna"))
	got, ok := p.Order("o1")
	if !ok || got.Total != 100 || got.Seq != 2 {
		t.Fatalf("Order = %+v, %v; want Total 100, Seq 2", got, ok)
	}
	if s := p.Stats(); s != (Stats{Applied: 2}) {
		t.Fatalf("Stats = %+v, want Applied 2", s)
	}
}

func TestBadEvent(t *testing.T) {
	p := NewProjection()
	for _, e := range []Event{
		{OrderID: "", Seq: 1, Kind: "created", Customer: "anna"},
		{OrderID: "o1", Seq: 0, Kind: "paid"},
		{OrderID: "o1", Seq: 2, Kind: "shipped"},
	} {
		if err := p.Apply(e); !errors.Is(err, ErrBadEvent) {
			t.Errorf("Apply(%+v) = %v, want ErrBadEvent", e, err)
		}
	}
}

func TestByCustomer(t *testing.T) {
	p := NewProjection()
	mustApply(t, p, created("o2", "anna"), created("o1", "anna"), created("o3", "boris"))
	got := p.ByCustomer("anna")
	if len(got) != 2 || got[0].OrderID != "o1" || got[1].OrderID != "o2" {
		t.Fatalf("ByCustomer(anna) = %+v, want o1, o2", got)
	}
}
