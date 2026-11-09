package task

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// quiet делает пул предсказуемым: один P и никаких автоматических
// сборок мусора, которые очищают sync.Pool.
func quiet(t *testing.T) {
	t.Helper()
	procs := runtime.GOMAXPROCS(1)
	gc := debug.SetGCPercent(-1)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(procs)
		debug.SetGCPercent(gc)
	})
}

func TestNewBufferHasInitCap(t *testing.T) {
	p := NewBufferPool(256, 4096)
	b := p.Get()
	if b == nil {
		t.Fatal("Get() = nil")
	}
	if b.Len() != 0 || b.Cap() < 256 {
		t.Fatalf("новый буфер: Len = %d, Cap = %d, want 0 и >= 256", b.Len(), b.Cap())
	}
}

func TestReuseAndReset(t *testing.T) {
	quiet(t)
	p := NewBufferPool(64, 4096)
	b := p.Get()
	b.WriteString("секрет прошлого запроса")
	p.Put(b)
	got := p.Get()
	if got != b {
		t.Fatal("Get() после Put вернул другой буфер — пул не переиспользует буферы")
	}
	if got.Len() != 0 {
		t.Fatalf("переиспользованный буфер не пуст: %q", got.String())
	}
}

func TestDropsLargeBuffers(t *testing.T) {
	quiet(t)
	p := NewBufferPool(64, 1024)
	big := p.Get()
	big.WriteString(strings.Repeat("x", 4096)) // буфер вырос до 4 КБ+
	p.Put(big)
	if got := p.Get(); got == big {
		t.Fatalf("пул вернул буфер ёмкостью %d при maxCap = 1024", big.Cap())
	}
	if n := p.Dropped(); n != 1 {
		t.Fatalf("Dropped() = %d, want 1", n)
	}
}

func TestZeroAllocsSteadyState(t *testing.T) {
	quiet(t)
	p := NewBufferPool(256, 4096)
	line := strings.Repeat("y", 200)
	allocs := testing.AllocsPerRun(1000, func() {
		b := p.Get()
		b.WriteString(line)
		p.Put(b)
	})
	if allocs != 0 {
		t.Fatalf("аллокаций на цикл Get/Write/Put: %.0f, want 0", allocs)
	}
}
