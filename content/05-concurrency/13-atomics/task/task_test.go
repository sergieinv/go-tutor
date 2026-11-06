package task

import "testing"

func TestGaugeSequential(t *testing.T) {
	var g Gauge
	for _, v := range []int64{5, 12, 3, 12, 7} {
		g.Observe(v)
	}
	if n := g.Count(); n != 5 {
		t.Errorf("Count() = %d, want 5", n)
	}
	if m, ok := g.Max(); !ok || m != 12 {
		t.Errorf("Max() = (%d, %v), want (12, true)", m, ok)
	}
}

func TestGaugeEmpty(t *testing.T) {
	var g Gauge
	if n := g.Count(); n != 0 {
		t.Errorf("Count() = %d, want 0", n)
	}
	if m, ok := g.Max(); ok || m != 0 {
		t.Errorf("Max() = (%d, %v), want (0, false)", m, ok)
	}
}
