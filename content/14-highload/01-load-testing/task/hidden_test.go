package task

import (
	"sync"
	"testing"
	"time"
)

func TestEmptyHistogram(t *testing.T) {
	h := mustNew(t, std)
	for _, q := range []float64{-1, 0, 0.5, 1, 2} {
		if got := h.Quantile(q); got != 0 {
			t.Errorf("пустая гистограмма: Quantile(%v) = %v, want 0", q, got)
		}
	}
	if h.Count() != 0 {
		t.Errorf("Count() = %d, want 0", h.Count())
	}
}

func TestBoundsAreCopied(t *testing.T) {
	b := []time.Duration{10 * ms, 20 * ms}
	h := mustNew(t, b)
	b[0], b[1] = 1000*ms, 2000*ms
	h.Record(15 * ms)
	if got := h.Quantile(1); got != 20*ms {
		t.Fatalf("Quantile(1) = %v, want 20ms — гистограмма не скопировала bounds?", got)
	}
}

func TestBucketEdges(t *testing.T) {
	h := mustNew(t, std)
	h.Record(5 * ms) // ровно граница — бакет (1, 5]
	if got := h.Quantile(1); got != 5*ms {
		t.Fatalf("значение 5ms: Quantile(1) = %v, want 5ms (граница входит в бакет)", got)
	}
	h2 := mustNew(t, std)
	h2.Record(5*ms + 1)
	if got := h2.Quantile(1); got != 10*ms {
		t.Fatalf("значение 5ms+1ns: Quantile(1) = %v, want 10ms", got)
	}
	h3 := mustNew(t, std)
	h3.Record(0)
	h3.Record(-3 * ms)
	if got := h3.Quantile(1); got != 1*ms {
		t.Fatalf("ноль и отрицательные: Quantile(1) = %v, want 1ms (первый бакет)", got)
	}
	h4 := mustNew(t, std)
	h4.Record(500 * ms)
	if got := h4.Quantile(1); got != 500*ms {
		t.Fatalf("значение на последней границе: Quantile(1) = %v, want 500ms", got)
	}
}

func TestQuantileClamp(t *testing.T) {
	h := mustNew(t, std)
	h.Record(3 * ms)
	h.Record(70 * ms)
	h.Record(800 * ms)
	if got := h.Quantile(-0.5); got != 5*ms {
		t.Errorf("Quantile(-0.5) = %v, want 5ms (как q = 0)", got)
	}
	if got := h.Quantile(7); got != 800*ms {
		t.Errorf("Quantile(7) = %v, want 800ms (как q = 1, переполнение — максимум)", got)
	}
	if got := h.Quantile(0.5); got != 100*ms {
		t.Errorf("Quantile(0.5) = %v, want 100ms (ранг 2)", got)
	}
}

func TestOverflowOnly(t *testing.T) {
	h := mustNew(t, []time.Duration{1 * ms})
	h.Record(7 * ms)
	h.Record(9 * ms)
	h.Record(8 * ms)
	for _, q := range []float64{0, 0.5, 1} {
		if got := h.Quantile(q); got != 9*ms {
			t.Errorf("Quantile(%v) = %v, want 9ms (бакет переполнения — максимум)", q, got)
		}
	}
}

func TestRecordCorrectedEdges(t *testing.T) {
	tests := []struct {
		d, exp time.Duration
		count  int64
	}{
		{50 * ms, 0, 1},
		{50 * ms, -10 * ms, 1},
		{8 * ms, 10 * ms, 1},
		{10 * ms, 10 * ms, 1},
		{20 * ms, 10 * ms, 2},
		{29 * ms, 10 * ms, 2},
		{1000 * ms, 1 * ms, 1000},
	}
	for _, tt := range tests {
		h := mustNew(t, std)
		h.RecordCorrected(tt.d, tt.exp)
		if n := h.Count(); n != tt.count {
			t.Errorf("RecordCorrected(%v, %v): Count() = %d, want %d", tt.d, tt.exp, n, tt.count)
		}
	}
	// 29ms при интервале 10ms: 29 и 19 — оба в бакете (10, 50].
	h := mustNew(t, std)
	h.RecordCorrected(29*ms, 10*ms)
	if got := h.Quantile(0); got != 50*ms {
		t.Errorf("RecordCorrected(29ms, 10ms): Quantile(0) = %v, want 50ms", got)
	}
}

// Закрытый генератор под перегрузкой: без поправки p90 выглядит
// прилично, с поправкой видно очередь.
func TestCorrectionChangesTail(t *testing.T) {
	plain, fixed := mustNew(t, std), mustNew(t, std)
	for range 99 {
		plain.Record(2 * ms)
		fixed.RecordCorrected(2*ms, 10*ms)
	}
	plain.Record(400 * ms)
	fixed.RecordCorrected(400*ms, 10*ms) // + 390, 380, …, 10 — 39 значений
	if plain.Count() != 100 || fixed.Count() != 139 {
		t.Fatalf("Count: без поправки %d, с поправкой %d, want 100 и 139", plain.Count(), fixed.Count())
	}
	if got := plain.Quantile(0.9); got != 5*ms {
		t.Errorf("без поправки Quantile(0.9) = %v, want 5ms", got)
	}
	if got := fixed.Quantile(0.9); got != 500*ms {
		t.Errorf("с поправкой Quantile(0.9) = %v, want 500ms", got)
	}
}

func TestConcurrentRecord(t *testing.T) {
	h := mustNew(t, std)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 500 {
				if i%2 == 0 {
					h.Record(time.Duration(g+i) * ms)
				} else {
					h.RecordCorrected(25*ms, 10*ms) // 25 и 15 — два значения
				}
				_ = h.Quantile(0.99)
				_ = h.Count()
			}
		})
	}
	wg.Wait()
	if n := h.Count(); n != 8*(250+2*250) {
		t.Fatalf("Count() = %d, want %d — потеряны записи при конкурентном вызове", n, 8*(250+2*250))
	}
	if got := h.Quantile(1); got != 505*ms {
		t.Fatalf("Quantile(1) = %v, want 505ms (максимум 7+498 мс лежит в бакете переполнения)", got)
	}
}
