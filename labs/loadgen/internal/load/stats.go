package load

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"gotutor/labs/loadgen/internal/hist"
)

// Stats — результаты прогона: по каждой операции (catalog.get,
// orders.create…) гистограмма задержек и счётчики исходов.
type Stats struct {
	mu  sync.Mutex
	ops map[string]*OpStats
	// Dropped — сколько запланированных сценариев открытой модели не
	// запущено: генератор упёрся в свой лимит одновременных сценариев.
	// Ненулевое значение значит, что сервис не справился (или лимит мал).
	Dropped int64
	// Started — сколько сценариев запущено.
	Started int64
	// Stale — проверки read-your-writes: заказ не найден в истории сразу
	// после создания, по токену (StaleWithToken) и без (StaleNoToken).
	StaleWithToken, StaleNoToken, RYWChecks int64
	Elapsed                                 time.Duration
}

// OpStats — одна операция.
type OpStats struct {
	Latency hist.Histogram
	// Outcomes — исходы: HTTP-код ("200", "429") или вид ошибки
	// ("timeout", "conn_error").
	Outcomes map[string]int64
	// ByOutcome — задержки отдельно по исходу: быстрый 429 и медленный
	// 200 в одной гистограмме смешались бы.
	ByOutcome map[string]*hist.Histogram
}

// NewStats создаёт пустые результаты.
func NewStats() *Stats { return &Stats{ops: map[string]*OpStats{}} }

// Record добавляет результат запроса. Задержка — от intended (момента,
// когда запрос должен был уйти по расписанию), а не от фактической
// отправки: так задержка, накопленная из-за того, что сервис или сам
// генератор не успевали, не теряется (coordinated omission).
func (s *Stats) Record(op string, intended, end time.Time, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.ops[op]
	if o == nil {
		o = &OpStats{Outcomes: map[string]int64{}, ByOutcome: map[string]*hist.Histogram{}}
		s.ops[op] = o
	}
	d := end.Sub(intended)
	o.Latency.Record(d)
	o.Outcomes[outcome]++
	h := o.ByOutcome[outcome]
	if h == nil {
		h = &hist.Histogram{}
		o.ByOutcome[outcome] = h
	}
	h.Record(d)
}

func (s *Stats) add(f func(s *Stats)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// Op возвращает статистику операции (nil — не было).
func (s *Stats) Op(name string) *OpStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ops[name]
}

// Ops — имена операций по алфавиту.
func (s *Stats) Ops() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(maps.Keys(s.ops))
}

// Count — всего запросов операции.
func (o *OpStats) Count() int64 {
	if o == nil {
		return 0
	}
	return int64(o.Latency.Count())
}

// Errors — запросы с исходом не 2xx (в том числе 429 и 4xx). Оборванные
// концом прогона закрытой модели ("canceled") не считаются.
func (o *OpStats) Errors() int64 {
	if o == nil {
		return 0
	}
	var n int64
	for k, v := range o.Outcomes {
		if !strings.HasPrefix(k, "2") && k != "canceled" {
			n += v
		}
	}
	return n
}

// OutcomeQuantile — перцентиль задержки запросов с данным исходом.
func (o *OpStats) OutcomeQuantile(outcome string, q float64) time.Duration {
	if o == nil || o.ByOutcome[outcome] == nil {
		return 0
	}
	return o.ByOutcome[outcome].Quantile(q)
}

// Quantile — перцентиль задержки (0, если операции не было).
func (o *OpStats) Quantile(q float64) time.Duration {
	if o == nil {
		return 0
	}
	return o.Latency.Quantile(q)
}

// Report печатает отчёт: по операции — число, частоту, исходы, перцентили.
func (s *Stats) Report(w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secs := s.Elapsed.Seconds()
	if secs <= 0 {
		secs = 1
	}
	fmt.Fprintf(w, "сценариев запущено: %d (%.1f/с)", s.Started, float64(s.Started)/secs)
	if s.Dropped > 0 {
		fmt.Fprintf(w, ", не запущено (лимит генератора): %d", s.Dropped)
	}
	fmt.Fprintf(w, ", длительность %s\n", s.Elapsed.Round(time.Millisecond))
	fmt.Fprintf(w, "%-15s %8s %8s %9s %9s %9s %9s %9s  %s\n",
		"операция", "запросов", "в сек", "p50", "p90", "p99", "p99.9", "max", "исходы")
	for _, name := range slices.Sorted(maps.Keys(s.ops)) {
		o := s.ops[name]
		h := &o.Latency
		fmt.Fprintf(w, "%-15s %8d %8.1f %9s %9s %9s %9s %9s  %s\n",
			name, h.Count(), float64(h.Count())/secs,
			ms(h.Quantile(0.5)), ms(h.Quantile(0.9)), ms(h.Quantile(0.99)), ms(h.Quantile(0.999)), ms(h.Max()),
			outcomes(o.Outcomes))
	}
	if s.RYWChecks > 0 {
		fmt.Fprintf(w, "read-your-writes: проверок %d, заказ не виден без токена: %d, с токеном: %d\n",
			s.RYWChecks, s.StaleNoToken, s.StaleWithToken)
	}
}

func outcomes(m map[string]int64) string {
	keys := slices.Sorted(maps.Keys(m))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s:%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

// ms форматирует задержку в миллисекундах с точностью, которая видна
// глазом: 0.42ms, 12.3ms, 1520ms.
func ms(d time.Duration) string {
	v := float64(d) / float64(time.Millisecond)
	switch {
	case v < 10:
		return fmt.Sprintf("%.2fms", v)
	case v < 100:
		return fmt.Sprintf("%.1fms", v)
	}
	return fmt.Sprintf("%.0fms", v)
}

// Ms — то же для внешних пакетов (сводная таблица разбора).
func Ms(d time.Duration) string { return ms(d) }
