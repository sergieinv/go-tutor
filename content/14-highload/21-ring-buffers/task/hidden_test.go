package task

import (
	"go/parser"
	"go/token"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"
	"weak"
)

func TestHiddenNoLocks(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "task.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("не удалось разобрать task.go: %v", err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "sync" {
			t.Fatal("task.go импортирует sync: кольцо должно обходиться без блокировок")
		}
	}
}

// head и tail пишут разные горутины — они не должны делить линию.
func TestHiddenLayout(t *testing.T) {
	var r Ring[int]
	h, tl := unsafe.Offsetof(r.head), unsafe.Offsetof(r.tail)
	d := max(h, tl) - min(h, tl)
	if d < CacheLine {
		t.Fatalf("head и tail на расстоянии %d байт, want >= %d", d, CacheLine)
	}
}

func TestHiddenSmallCapacity(t *testing.T) {
	for _, c := range []int{0, -5} {
		if got := New[int](c).Cap(); got != 1 {
			t.Errorf("New(%d).Cap() = %d, want 1", c, got)
		}
	}
	if got := New[int](1000).Cap(); got != 1024 {
		t.Errorf("New(1000).Cap() = %d, want 1024", got)
	}
	r := New[int](0)
	if r.Cap() != 1 {
		t.Fatalf("Cap() = %d, want 1", r.Cap())
	}
	if !r.TryPush(1) || r.TryPush(2) {
		t.Fatal("кольцо на 1 элемент: первый TryPush — true, второй — false")
	}
	if v, ok := r.TryPop(); !ok || v != 1 {
		t.Fatalf("TryPop() = %d, %v", v, ok)
	}
}

// Индексы много раз проходят по кругу; Len точен без конкуренции.
func TestHiddenWrapAround(t *testing.T) {
	r := New[int](4)
	if r.Cap() != 4 {
		t.Fatalf("Cap() = %d, want 4", r.Cap())
	}
	next, want := 0, 0
	for round := range 3000 {
		k := round%4 + 1
		for range k {
			if !r.TryPush(next) {
				t.Fatalf("TryPush(%d) = false при Len() = %d", next, r.Len())
			}
			next++
		}
		if r.Len() != k {
			t.Fatalf("Len() = %d, want %d", r.Len(), k)
		}
		for range k {
			v, ok := r.TryPop()
			if !ok || v != want {
				t.Fatalf("TryPop() = %d, %v; want %d", v, ok, want)
			}
			want++
		}
		if r.Len() != 0 {
			t.Fatalf("Len() = %d, want 0", r.Len())
		}
	}
	for i := range 4 {
		r.TryPush(i)
	}
	if r.TryPush(99) || r.Len() != 4 {
		t.Fatalf("полное кольцо: TryPush = true или Len() = %d", r.Len())
	}
}

type blob struct{ data [256]byte }

//go:noinline
func pushPopDrop(r *Ring[*blob]) weak.Pointer[blob] {
	b := new(blob)
	wp := weak.Make(b)
	r.TryPush(b)
	r.TryPop()
	return wp
}

// Выданный элемент не должен оставаться в ячейке: иначе кольцо
// держит его живым, пока ячейку не перезапишут.
func TestHiddenNoRetention(t *testing.T) {
	r := New[*blob](8)
	wp := pushPopDrop(r)
	for range 3 {
		runtime.GC()
	}
	if wp.Value() != nil {
		t.Fatal("кольцо держит ссылку на выданный элемент")
	}
	runtime.KeepAlive(r)
}

// Стресс под -race: ёмкость 1 и 8, порядок FIFO, Len в пределах.
func TestHiddenStress(t *testing.T) {
	for _, capacity := range []int{1, 8} {
		const n = 20_000
		r := New[int](capacity)
		if r.Cap() != capacity {
			t.Fatalf("Cap() = %d, want %d", r.Cap(), capacity)
		}
		errs := make(chan string, 1)
		go func() {
			for i := 0; i < n; {
				if l := r.Len(); l < 0 || l > r.Cap() {
					select {
					case errs <- "Len() = " + strconv.Itoa(l):
					default:
					}
				}
				if r.TryPush(i) {
					i++
				} else {
					runtime.Gosched()
				}
			}
		}()
		deadline := time.Now().Add(3 * time.Second)
		for want := 0; want < n; {
			v, ok := r.TryPop()
			if !ok {
				if time.Now().After(deadline) {
					t.Fatalf("cap %d: застряли на значении %d", capacity, want)
				}
				runtime.Gosched()
				continue
			}
			if v != want {
				t.Fatalf("cap %d: получили %d, ждали %d", capacity, v, want)
			}
			if l := r.Len(); l < 0 || l > r.Cap() {
				t.Fatalf("cap %d: Len() = %d", capacity, l)
			}
			want++
		}
		select {
		case e := <-errs:
			t.Fatalf("cap %d: %s", capacity, e)
		default:
		}
	}
}
