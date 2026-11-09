package task

import (
	"errors"
	"math"
	"slices"
	"sync"
	"time"
)

// ErrBounds — границы бакетов пусты или не возрастают строго.
var ErrBounds = errors.New("histogram: bounds must be non-empty and strictly increasing")

// Histogram — гистограмма задержек с фиксированными бакетами.
type Histogram struct {
	mu     sync.Mutex
	bounds []time.Duration
	counts []int64 // len(bounds)+1: последний — переполнение
	total  int64
	maxVal time.Duration
}

// NewHistogram создаёт гистограмму с верхними границами бакетов bounds.
func NewHistogram(bounds []time.Duration) (*Histogram, error) {
	if len(bounds) == 0 {
		return nil, ErrBounds
	}
	for i := 1; i < len(bounds); i++ {
		if bounds[i] <= bounds[i-1] {
			return nil, ErrBounds
		}
	}
	return &Histogram{
		bounds: slices.Clone(bounds),
		counts: make([]int64, len(bounds)+1),
	}, nil
}

// recordLocked кладёт значение в бакет: первый, чья граница >= d.
func (h *Histogram) recordLocked(d time.Duration) {
	i, _ := slices.BinarySearch(h.bounds, d)
	h.counts[i]++
	if h.total == 0 || d > h.maxVal {
		h.maxVal = d
	}
	h.total++
}

// Record записывает одно значение.
func (h *Histogram) Record(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordLocked(d)
}

// RecordCorrected записывает d и «пропущенные» генератором замеры
// d−expected, d−2·expected, … — пока они не меньше expected.
func (h *Histogram) RecordCorrected(d, expected time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordLocked(d)
	if expected <= 0 {
		return
	}
	for m := d - expected; m >= expected; m -= expected {
		h.recordLocked(m)
	}
}

// Count возвращает число записанных значений.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// Quantile возвращает верхнюю границу бакета, в котором лежит
// значение ранга ⌈q·Count⌉.
func (h *Histogram) Quantile(q float64) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total == 0 {
		return 0
	}
	if math.IsNaN(q) {
		q = 0
	}
	q = min(max(q, 0), 1)
	k := max(int64(math.Ceil(q*float64(h.total))), 1)
	var acc int64
	for i, c := range h.counts {
		acc += c
		if acc >= k {
			if i == len(h.bounds) {
				return h.maxVal
			}
			return h.bounds[i]
		}
	}
	return h.maxVal // недостижимо: acc дойдёт до total >= k
}
