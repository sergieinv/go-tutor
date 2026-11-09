package hist

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestIndexAndUpperAreConsistent(t *testing.T) {
	prev := -1
	for v := uint64(0); v < 1<<20; v++ {
		i := index(v)
		if i < prev || i > prev+1 {
			t.Fatalf("index(%d) = %d after %d: корзины должны идти подряд", v, i, prev)
		}
		if v > upper(i) || (i > 0 && v <= upper(i-1)) {
			t.Fatalf("value %d outside bucket %d (upper %d, prev upper %d)", v, i, upper(i), upper(i-1))
		}
		prev = i
	}
}

func TestSmallValuesAreExact(t *testing.T) {
	var h Histogram
	for v := 1; v <= 50; v++ {
		h.Record(time.Duration(v) * time.Microsecond)
	}
	if got := h.Quantile(0.5); got != 25*time.Microsecond {
		t.Errorf("p50 = %v, want 25µs", got)
	}
	if got := h.Quantile(1); got != 50*time.Microsecond || h.Max() != 50*time.Microsecond {
		t.Errorf("p100 = %v, max = %v, want 50µs", got, h.Max())
	}
	if h.Count() != 50 {
		t.Errorf("count = %d", h.Count())
	}
}

// TestRelativeError: перцентили случайных задержек совпадают с точными
// (по отсортированному массиву) с ошибкой не больше 1/32.
func TestRelativeError(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var h Histogram
	vals := make([]time.Duration, 100000)
	for i := range vals {
		// Логнормальное: тело ~ миллисекунды, хвост — десятки миллисекунд.
		us := math.Exp(r.NormFloat64() + 7.5)
		vals[i] = time.Duration(us) * time.Microsecond
		h.Record(vals[i])
	}
	slices.Sort(vals)
	for _, q := range []float64{0.5, 0.9, 0.99, 0.999} {
		exact := vals[int(q*float64(len(vals)))-1]
		got := h.Quantile(q)
		if diff := float64(got-exact) / float64(exact); diff < 0 || diff > 1.0/32 {
			t.Errorf("q%.3f = %v, exact %v: relative error %.4f", q, got, exact, diff)
		}
	}
	if h.Max() != vals[len(vals)-1] {
		t.Errorf("max = %v, want %v", h.Max(), vals[len(vals)-1])
	}
}

func TestMergeAndEmpty(t *testing.T) {
	var a, b, empty Histogram
	if empty.Quantile(0.99) != 0 || empty.Max() != 0 {
		t.Error("empty histogram must report zeros")
	}
	a.Record(time.Millisecond)
	b.Record(time.Second)
	b.Record(2 * time.Second)
	a.Merge(&b)
	if a.Count() != 3 || a.Max() != 2*time.Second {
		t.Errorf("merged count %d max %v", a.Count(), a.Max())
	}
	if p50 := a.Quantile(0.5); p50 < time.Second || p50 > time.Second+time.Second/32 {
		t.Errorf("merged p50 = %v, want ≈1s", p50)
	}
	a.Merge(&empty)
	if a.Count() != 3 {
		t.Error("merge with empty changed count")
	}
}
