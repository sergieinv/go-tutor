package task

import (
	"errors"
	"time"
)

// ErrBounds — границы бакетов пусты или не возрастают строго.
var ErrBounds = errors.New("histogram: bounds must be non-empty and strictly increasing")

// Histogram — гистограмма задержек с фиксированными бакетами.
type Histogram struct {
	// TODO: границы, счётчики бакетов, общее число, максимум, мьютекс
}

// NewHistogram создаёт гистограмму с верхними границами бакетов bounds.
func NewHistogram(bounds []time.Duration) (*Histogram, error) {
	// TODO: проверить и скопировать bounds
	return &Histogram{}, nil
}

// Record записывает одно значение.
func (h *Histogram) Record(d time.Duration) {
	// TODO
}

// RecordCorrected записывает d и «пропущенные» генератором замеры
// d−expected, d−2·expected, … — пока они не меньше expected.
func (h *Histogram) RecordCorrected(d, expected time.Duration) {
	// TODO
}

// Count возвращает число записанных значений.
func (h *Histogram) Count() int64 {
	// TODO
	return 0
}

// Quantile возвращает верхнюю границу бакета, в котором лежит
// значение ранга ⌈q·Count⌉.
func (h *Histogram) Quantile(q float64) time.Duration {
	// TODO
	return 0
}
