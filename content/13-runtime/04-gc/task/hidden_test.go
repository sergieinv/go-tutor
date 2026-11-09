package task

import (
	"slices"
	"testing"
)

func TestHiddenNewObjectsWhiteOutsideGC(t *testing.T) {
	h := NewHeap()
	a := h.Alloc(3)
	if h.Color(a) != White || !h.Live(a) {
		t.Fatalf("новый объект: цвет %v, Live %v; want белый, true", h.Color(a), h.Live(a))
	}
	for i := range 3 {
		if got := h.Read(a, i); got != Nil {
			t.Errorf("Read(a, %d) = %d у нового объекта, want Nil", i, got)
		}
	}
}

func TestHiddenRootsAscending(t *testing.T) {
	h := NewHeap()
	for range 6 {
		h.Alloc(0)
	}
	h.AddRoot(5)
	h.AddRoot(2)
	h.AddRoot(4)
	h.RemoveRoot(4)
	h.StartGC()
	for id, want := range []Color{White, White, Grey, White, White, Grey} {
		if got := h.Color(id); got != want {
			t.Errorf("после StartGC Color(%d) = %v, want %v", id, got, want)
		}
	}
	h.MarkStep()
	if h.Color(2) != Black || h.Color(5) != Grey {
		t.Errorf("первым должен просматриваться корень с меньшим ID: Color(2)=%v Color(5)=%v", h.Color(2), h.Color(5))
	}
	freed := h.Sweep()
	if !slices.Equal(freed, []int{0, 1, 3, 4}) {
		t.Errorf("Sweep() = %v, want [0 1 3 4]", freed)
	}
}

func TestHiddenFIFOOrder(t *testing.T) {
	// r → (b, c), b → d. Очередь: r; b, c; c, d; d.
	h := NewHeap()
	r, b, c, d := h.Alloc(2), h.Alloc(1), h.Alloc(0), h.Alloc(0)
	h.Write(r, 0, b)
	h.Write(r, 1, c)
	h.Write(b, 0, d)
	h.AddRoot(r)
	h.StartGC()

	want := [][4]Color{
		{Black, Grey, Grey, White},
		{Black, Black, Grey, Grey},
		{Black, Black, Black, Grey},
		{Black, Black, Black, Black},
	}
	for step, w := range want {
		if !h.MarkStep() {
			t.Fatalf("MarkStep #%d вернул false", step+1)
		}
		got := [4]Color{h.Color(r), h.Color(b), h.Color(c), h.Color(d)}
		if got != w {
			t.Fatalf("после MarkStep #%d цвета r,b,c,d = %v, want %v", step+1, got, w)
		}
	}
	if h.MarkStep() {
		t.Errorf("MarkStep вернул true при пустой очереди")
	}
}

func TestHiddenCycles(t *testing.T) {
	h := NewHeap()
	// Достижимый цикл a ⇄ b и недостижимый c ⇄ d, плюс e → e.
	a, b, c, d, e := h.Alloc(1), h.Alloc(1), h.Alloc(1), h.Alloc(1), h.Alloc(1)
	h.Write(a, 0, b)
	h.Write(b, 0, a)
	h.Write(c, 0, d)
	h.Write(d, 0, c)
	h.Write(e, 0, e)
	h.AddRoot(b)
	h.StartGC()
	if freed := h.Sweep(); !slices.Equal(freed, []int{c, d, e}) {
		t.Errorf("Sweep() = %v, want [%d %d %d]: циклы без корня — мусор", freed, c, d, e)
	}
	if !h.Live(a) || !h.Live(b) {
		t.Errorf("достижимый цикл освобождён")
	}
}

func TestHiddenSweepResetsColorsAndSecondCycle(t *testing.T) {
	h := NewHeap()
	a, b := h.Alloc(1), h.Alloc(0)
	h.Write(a, 0, b)
	h.AddRoot(a)
	h.StartGC()
	h.Sweep()
	if h.Color(a) != White || h.Color(b) != White {
		t.Fatalf("после Sweep выжившие: a=%v b=%v; want белые", h.Color(a), h.Color(b))
	}
	// Между циклами барьер выключен: запись не красит.
	h.Write(a, 0, Nil)
	if h.Color(b) != White {
		t.Fatalf("Write вне цикла GC перекрасил объект: b=%v", h.Color(b))
	}
	h.StartGC()
	if freed := h.Sweep(); !slices.Equal(freed, []int{b}) {
		t.Errorf("второй цикл: Sweep() = %v, want [%d]", freed, b)
	}
	if h.MarkStep() {
		t.Errorf("MarkStep вернул true вне цикла GC")
	}
}

func TestHiddenSweepWithoutGC(t *testing.T) {
	h := NewHeap()
	a := h.Alloc(0) // не корень, но цикл не начат
	if freed := h.Sweep(); len(freed) != 0 {
		t.Errorf("Sweep без StartGC = %v, want пусто", freed)
	}
	if !h.Live(a) {
		t.Errorf("Sweep без StartGC освободил объект")
	}
}

func TestHiddenSweepFinishesMarking(t *testing.T) {
	// Sweep сам доводит маркировку: длинная цепочка без MarkStep.
	h := NewHeap()
	prev := h.Alloc(1)
	h.AddRoot(prev)
	for range 50 {
		next := h.Alloc(1)
		h.Write(prev, 0, next)
		prev = next
	}
	garbage := h.Alloc(0)
	h.StartGC()
	if freed := h.Sweep(); !slices.Equal(freed, []int{garbage}) {
		t.Errorf("Sweep() = %v, want [%d]", freed, garbage)
	}
}

func TestHiddenAllocDuringMarkIsBlack(t *testing.T) {
	h := NewHeap()
	a := h.Alloc(1)
	h.AddRoot(a)
	h.StartGC()
	n := h.Alloc(0) // ни от кого не достижим, но создан во время маркировки
	if h.Color(n) != Black {
		t.Fatalf("объект, созданный во время маркировки: %v, want чёрный", h.Color(n))
	}
	if freed := h.Sweep(); len(freed) != 0 {
		t.Errorf("Sweep() = %v: объект, созданный во время маркировки, переживает этот цикл", freed)
	}
	// В следующем цикле он обычный мусор.
	h.StartGC()
	if freed := h.Sweep(); !slices.Equal(freed, []int{n}) {
		t.Errorf("следующий цикл: Sweep() = %v, want [%d]", freed, n)
	}
}

func TestHiddenBarrierShadesBoth(t *testing.T) {
	h := NewHeap()
	a, old, nw := h.Alloc(1), h.Alloc(0), h.Alloc(0)
	h.Write(a, 0, old)
	h.StartGC() // корней нет: все белые
	h.Write(a, 0, nw)
	if h.Color(old) != Grey || h.Color(nw) != Grey {
		t.Fatalf("после Write во время маркировки: старое=%v новое=%v; want оба серые", h.Color(old), h.Color(nw))
	}
	// Сначала красится старое значение, потом новое — так они и встают в очередь.
	h.MarkStep()
	if h.Color(old) != Black || h.Color(nw) != Grey {
		t.Errorf("порядок очереди: старое=%v новое=%v; want старое просмотрено первым", h.Color(old), h.Color(nw))
	}
	// Объект a не корень и никем не достижим — его соберут,
	// а old и nw переживут цикл: барьер консервативен.
	if freed := h.Sweep(); !slices.Equal(freed, []int{a}) {
		t.Errorf("Sweep() = %v, want [%d]", freed, a)
	}
}

// Классическая потеря объекта: указатель на белый c записан в уже
// чёрный a, а последняя ссылка из серого b удалена.
func TestHiddenLostObjectScenario(t *testing.T) {
	h := NewHeap()
	a, b, c := h.Alloc(1), h.Alloc(1), h.Alloc(0)
	h.Write(a, 0, b)
	h.Write(b, 0, c)
	h.AddRoot(a)
	h.StartGC()
	h.MarkStep() // a чёрный
	h.Write(a, 0, c)
	h.Write(b, 0, Nil)
	h.Sweep()
	if !h.Live(c) {
		t.Fatalf("c освобождён, хотя на него ссылается корень через a")
	}
	if !h.Live(b) {
		t.Errorf("b освобождён в этом цикле, хотя был серым: плавающий мусор собирается в следующем")
	}
	h.StartGC()
	if freed := h.Sweep(); !slices.Equal(freed, []int{b}) {
		t.Errorf("следующий цикл: Sweep() = %v, want [%d]", freed, b)
	}
}
