package task

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestPutNil(t *testing.T) {
	p := NewBufferPool(16, 64)
	p.Put(nil)
	if n := p.Dropped(); n != 0 {
		t.Fatalf("Put(nil) изменил Dropped: %d", n)
	}
	if b := p.Get(); b == nil || b.Len() != 0 {
		t.Fatalf("Get() после Put(nil) = %v", b)
	}
}

func TestCapExactlyMaxIsKept(t *testing.T) {
	quiet(t)
	p := NewBufferPool(16, 1024)
	b := bytes.NewBuffer(make([]byte, 0, 1024))
	b.WriteString("abc")
	p.Put(b)
	if got := p.Get(); got != b {
		t.Fatal("буфер ёмкостью ровно maxCap должен вернуться в пул")
	}
	over := bytes.NewBuffer(make([]byte, 0, 1025))
	p.Put(over)
	if got := p.Get(); got == over {
		t.Fatal("буфер ёмкостью maxCap+1 вернулся в пул")
	}
	if n := p.Dropped(); n != 1 {
		t.Fatalf("Dropped() = %d, want 1", n)
	}
}

func TestForeignBufferAccepted(t *testing.T) {
	quiet(t)
	p := NewBufferPool(0, 512)
	b := bytes.NewBufferString("чужой буфер")
	p.Put(b)
	if got := p.Get(); got != b || got.Len() != 0 {
		t.Fatalf("Put чужого буфера: got %p (Len %d), want %p пустой", got, got.Len(), b)
	}
}

func TestNegativeAndInvertedCaps(t *testing.T) {
	quiet(t)
	// initCap < 0 считается нулём.
	p := NewBufferPool(-10, 100)
	if b := p.Get(); b == nil || b.Len() != 0 {
		t.Fatalf("Get() = %v", b)
	}
	// maxCap < initCap поднимается до initCap: буфер ёмкостью
	// initCap возвращается в пул.
	q := NewBufferPool(512, 8)
	b := q.Get()
	if b.Cap() < 512 {
		t.Fatalf("Cap = %d, want >= 512", b.Cap())
	}
	exact := bytes.NewBuffer(make([]byte, 0, 512))
	q.Put(exact)
	if got := q.Get(); got != exact {
		t.Fatal("при maxCap < initCap буфер ёмкостью initCap должен остаться в пуле")
	}
	if n := q.Dropped(); n != 0 {
		t.Fatalf("Dropped() = %d, want 0", n)
	}
	q.Put(bytes.NewBuffer(make([]byte, 0, 513)))
	if n := q.Dropped(); n != 1 {
		t.Fatalf("Dropped() = %d, want 1", n)
	}
}

func TestDroppedCountsEveryLargePut(t *testing.T) {
	p := NewBufferPool(8, 32)
	for range 5 {
		p.Put(bytes.NewBuffer(make([]byte, 0, 33)))
	}
	p.Put(bytes.NewBuffer(make([]byte, 0, 32)))
	if n := p.Dropped(); n != 5 {
		t.Fatalf("Dropped() = %d, want 5", n)
	}
}

// Многие горутины берут, пишут и возвращают буферы. Каждый
// полученный буфер пуст, не превышает maxCap и не делится ни с кем.
func TestConcurrentUse(t *testing.T) {
	const maxCap = 2048
	p := NewBufferPool(64, maxCap)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 2000 {
				b := p.Get()
				if b.Len() != 0 {
					t.Errorf("Get() вернул непустой буфер: %q", b.String())
					return
				}
				if b.Cap() > maxCap {
					t.Errorf("Get() вернул буфер ёмкостью %d > %d", b.Cap(), maxCap)
					return
				}
				want := fmt.Sprintf("g%d-i%d-", g, i) + strings.Repeat("z", (i*37)%3000)
				b.WriteString(want)
				if b.String() != want {
					t.Errorf("буфер испорчен другой горутиной")
					return
				}
				p.Put(b)
			}
		})
	}
	wg.Wait()
	if p.Dropped() == 0 {
		t.Fatal("буферы больше maxCap должны были выбрасываться")
	}
}

func TestZeroAllocsWithSmallInitCap(t *testing.T) {
	quiet(t)
	// Буфер вырастает при первой записи, но потом переиспользуется.
	p := NewBufferPool(0, 1<<20)
	line := strings.Repeat("w", 1000)
	b := p.Get()
	b.WriteString(line)
	p.Put(b)
	allocs := testing.AllocsPerRun(1000, func() {
		b := p.Get()
		b.WriteString(line)
		p.Put(b)
	})
	if allocs != 0 {
		t.Fatalf("аллокаций на цикл: %.0f, want 0 — выросший буфер должен переиспользоваться", allocs)
	}
}
