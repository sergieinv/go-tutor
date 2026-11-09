package task

import (
	"errors"
	"io"
)

// MaxInput — наибольший размер одного снимка /metrics в байтах.
const MaxInput = 1 << 20

var (
	ErrTooLarge = errors.New("promdiff: input too large")
	ErrSyntax   = errors.New("promdiff: syntax error")
	ErrDuration = errors.New("promdiff: bad duration")
)

// Sample — один ряд снимка: имя, метки и значение.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Summary — сводка шага нагрузочного разбора.
type Summary struct {
	DBQueries    float64 // прирост orders_catalog_db_queries_total
	DBPerSecond  float64 // DBQueries / seconds
	CacheHits    float64 // hit + negative_hit
	CacheLookups float64 // hit + negative_hit + miss
	HitRatio     float64 // CacheHits / CacheLookups
	ReplicaReads float64 // target="replica"
	HistoryReads float64 // target="replica" + target="primary"
	ReplicaRatio float64 // ReplicaReads / HistoryReads
	CreateCalls  float64 // все завершённые CreateOrder
	Shed         float64 // CreateOrder с кодом ResourceExhausted (HTTP 429)
	ShedRatio    float64 // Shed / CreateCalls
}

// Parse разбирает снимок в текстовом формате экспозиции Prometheus.
func Parse(r io.Reader) ([]Sample, error) {
	// TODO
	return nil, nil
}

// Increase — прирост счётчика name между снимками по правилам rate в
// Prometheus: каждый ряд отдельно, сброс счётчика — отсчёт от нуля.
func Increase(before, after []Sample, name string, match map[string]string) float64 {
	// TODO
	return 0
}

// Summarize считает сводку шага длительностью seconds.
func Summarize(before, after []Sample, seconds float64) (Summary, error) {
	// TODO
	return Summary{}, nil
}
