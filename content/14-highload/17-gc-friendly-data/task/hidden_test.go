package task

import (
	"math"
	"strconv"
	"testing"
)

// refBloom — независимая реализация условия: с ней сверяются ответы
// MayContain бит в бит.
type refBloom struct {
	set map[uint64]bool
	m   uint64
	k   uint64
}

func refParams(n int, p float64) (m uint64, k int) {
	if n < 1 {
		n = 1
	}
	if !(p > 0 && p < 1) {
		p = 0.01
	}
	m = uint64(math.Ceil(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2)))
	k = int(math.Round(float64(m) / float64(n) * math.Ln2))
	if k < 1 {
		k = 1
	}
	return m, k
}

func refHash(key string) (uint64, uint64) {
	h := uint64(14695981039346656037)
	for _, c := range []byte(key) {
		h = (h ^ uint64(c)) * 1099511628211
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return uint64(uint32(h)), h >> 32
}

func newRef(n int, p float64) *refBloom {
	m, k := refParams(n, p)
	return &refBloom{set: map[uint64]bool{}, m: m, k: uint64(k)}
}

func (r *refBloom) add(key string) {
	h1, h2 := refHash(key)
	for i := range r.k {
		r.set[(h1+i*h2)%r.m] = true
	}
}

func (r *refBloom) has(key string) bool {
	h1, h2 := refHash(key)
	for i := range r.k {
		if !r.set[(h1+i*h2)%r.m] {
			return false
		}
	}
	return true
}

func TestHiddenParams(t *testing.T) {
	cases := []struct {
		n int
		p float64
	}{
		{1000, 0.01}, {1, 0.5}, {10, 0.001}, {100_000, 0.05}, {7, 0.3}, {1_000_000, 0.0001},
		{0, 0.01}, {-5, 0.01}, // n < 1 — как n = 1
		{100, 0}, {100, 1}, {100, -0.2}, {100, math.NaN()}, // p вне (0, 1) — как 0.01
	}
	for _, c := range cases {
		m, k := refParams(c.n, c.p)
		b := NewBloom(c.n, c.p)
		if b.Bits() != int(m) || b.K() != k {
			t.Errorf("NewBloom(%d, %v): m = %d, k = %d; want %d, %d", c.n, c.p, b.Bits(), b.K(), m, k)
		}
	}
}

func TestHiddenExactBits(t *testing.T) {
	for _, c := range []struct {
		n int
		p float64
	}{{500, 0.02}, {64, 0.1}, {3, 0.5}} {
		b, r := NewBloom(c.n, c.p), newRef(c.n, c.p)
		for i := range c.n {
			k := "order:" + strconv.Itoa(i*7)
			b.Add(k)
			r.add(k)
		}
		for i := range 20_000 {
			k := "probe:" + strconv.Itoa(i)
			if b.MayContain(k) != r.has(k) {
				t.Fatalf("NewBloom(%d, %v): MayContain(%q) = %v, по условию %v — проверьте хеш и номера бит", c.n, c.p, k, b.MayContain(k), r.has(k))
			}
		}
	}
}

func TestHiddenEdgeKeys(t *testing.T) {
	b := NewBloom(10, 0.01)
	if b.MayContain("") {
		t.Errorf(`пустой фильтр: MayContain("") = true`)
	}
	for _, k := range []string{"", "ё", "\x00", "очень длинный ключ " + string(make([]byte, 1000))} {
		b.Add(k)
		if !b.MayContain(k) {
			t.Errorf("MayContain(%q) = false после Add", k)
		}
	}
}

func TestHiddenOverfilled(t *testing.T) {
	// Фильтр на 10 ключей, а добавили 5000: ложных срабатываний много,
	// но ложных отказов по-прежнему нет.
	b := NewBloom(10, 0.01)
	for i := range 5000 {
		b.Add(strconv.Itoa(i))
	}
	for i := range 5000 {
		if !b.MayContain(strconv.Itoa(i)) {
			t.Fatalf("переполненный фильтр потерял ключ %d", i)
		}
	}
}

var (
	probeKey = "customer-123456"
	sinkHas  bool
)

func TestHiddenNoAllocs(t *testing.T) {
	b := NewBloom(10_000, 0.01)
	if a := testing.AllocsPerRun(200, func() { b.Add(probeKey) }); a != 0 {
		t.Errorf("Add: %v выделений на вызов, want 0", a)
	}
	if a := testing.AllocsPerRun(200, func() { sinkHas = b.MayContain(probeKey) }); a != 0 {
		t.Errorf("MayContain: %v выделений на вызов, want 0", a)
	}
}
