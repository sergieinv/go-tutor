package task

import (
	"testing"
	"time"
)

const ms = time.Millisecond

var std = []time.Duration{1 * ms, 5 * ms, 10 * ms, 50 * ms, 100 * ms, 500 * ms}

func mustNew(t *testing.T, bounds []time.Duration) *Histogram {
	t.Helper()
	h, err := NewHistogram(bounds)
	if err != nil || h == nil {
		t.Fatalf("NewHistogram(%v) = %v, %v", bounds, h, err)
	}
	return h
}

func TestQuantiles(t *testing.T) {
	h := mustNew(t, std)
	for range 90 {
		h.Record(3 * ms) // бакет (1, 5]
	}
	for range 9 {
		h.Record(40 * ms) // бакет (10, 50]
	}
	h.Record(300 * ms) // бакет (100, 500]
	if n := h.Count(); n != 100 {
		t.Fatalf("Count() = %d, want 100", n)
	}
	tests := []struct {
		q    float64
		want time.Duration
	}{
		{0, 5 * ms},
		{0.5, 5 * ms},
		{0.9, 5 * ms},
		{0.91, 50 * ms},
		{0.99, 50 * ms},
		{1, 500 * ms},
	}
	for _, tt := range tests {
		if got := h.Quantile(tt.q); got != tt.want {
			t.Errorf("Quantile(%v) = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestOverflowReturnsMax(t *testing.T) {
	h := mustNew(t, std)
	h.Record(2 * ms)
	h.Record(1500 * ms)
	h.Record(900 * ms)
	if got := h.Quantile(1); got != 1500*ms {
		t.Fatalf("Quantile(1) = %v, want 1.5s (максимум в бакете переполнения)", got)
	}
}

func TestRecordCorrected(t *testing.T) {
	h := mustNew(t, std)
	h.RecordCorrected(50*ms, 10*ms) // 50, 40, 30, 20, 10
	if n := h.Count(); n != 5 {
		t.Fatalf("после RecordCorrected(50ms, 10ms) Count() = %d, want 5", n)
	}
	if got := h.Quantile(0.2); got != 10*ms {
		t.Fatalf("Quantile(0.2) = %v, want 10ms", got)
	}
	if got := h.Quantile(0.4); got != 50*ms {
		t.Fatalf("Quantile(0.4) = %v, want 50ms", got)
	}
}

func TestBadBounds(t *testing.T) {
	for _, b := range [][]time.Duration{nil, {}, {5 * ms, 5 * ms}, {10 * ms, 1 * ms}} {
		h, err := NewHistogram(b)
		if err != ErrBounds || h != nil {
			t.Errorf("NewHistogram(%v) = %v, %v, want nil, ErrBounds", b, h, err)
		}
	}
}
