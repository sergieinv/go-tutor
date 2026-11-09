package task

import (
	"slices"
	"testing"
)

func TestCollectsGarbage(t *testing.T) {
	h := NewHeap()
	a, b, c := h.Alloc(1), h.Alloc(0), h.Alloc(0)
	if a != 0 || b != 1 || c != 2 {
		t.Fatalf("Alloc вернул ID %d, %d, %d; want 0, 1, 2", a, b, c)
	}
	h.Write(a, 0, b) // a → b, c ни от кого не достижим
	h.AddRoot(a)
	if h.Read(a, 0) != b {
		t.Fatalf("Read(a, 0) = %d, want %d", h.Read(a, 0), b)
	}

	h.StartGC()
	freed := h.Sweep()
	if !slices.Equal(freed, []int{c}) {
		t.Errorf("Sweep() = %v, want [%d]", freed, c)
	}
	if !h.Live(a) || !h.Live(b) || h.Live(c) {
		t.Errorf("Live: a=%v b=%v c=%v; want true true false", h.Live(a), h.Live(b), h.Live(c))
	}
}

func TestMarkStepColors(t *testing.T) {
	h := NewHeap()
	a, b := h.Alloc(1), h.Alloc(0)
	h.Write(a, 0, b)
	h.AddRoot(a)

	h.StartGC()
	if h.Color(a) != Grey || h.Color(b) != White {
		t.Fatalf("после StartGC: a=%v b=%v; want серый, белый", h.Color(a), h.Color(b))
	}
	if !h.MarkStep() || h.Color(a) != Black || h.Color(b) != Grey {
		t.Fatalf("после 1-го MarkStep: a=%v b=%v; want чёрный, серый", h.Color(a), h.Color(b))
	}
	if !h.MarkStep() || h.Color(b) != Black {
		t.Fatalf("после 2-го MarkStep: b=%v; want чёрный", h.Color(b))
	}
	if h.MarkStep() {
		t.Fatalf("MarkStep вернул true, хотя серых не осталось")
	}
}

// Мутатор переносит единственную ссылку на c из серого объекта
// в уже просмотренный стек. Без барьера c останется белым и будет
// освобождён, хотя на него ссылается корень.
func TestBarrierKeepsMovedObject(t *testing.T) {
	h := NewHeap()
	a, b, c := h.Alloc(1), h.Alloc(1), h.Alloc(0)
	h.Write(a, 0, b)
	h.Write(b, 0, c)
	h.AddRoot(a)

	h.StartGC()
	h.MarkStep() // a чёрный, b серый, c белый

	moved := h.Read(b, 0)
	h.AddRoot(moved)   // c теперь ещё и на стеке — без барьера
	h.Write(b, 0, Nil) // удаляем ссылку b → c: барьер красит c
	if h.Color(c) != Grey {
		t.Fatalf("после Write(b, 0, Nil) c = %v, want серый", h.Color(c))
	}

	if freed := h.Sweep(); len(freed) != 0 {
		t.Errorf("Sweep() = %v, want пусто: всё достижимо", freed)
	}
	if !h.Live(c) {
		t.Errorf("c освобождён, хотя на него ссылается корень")
	}
}
