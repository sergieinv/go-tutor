package task

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
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
	// Сначала размер: читаем не больше MaxInput+1 байт, и если лишний байт
	// нашёлся — вход слишком велик, что бы в нём ни было.
	data, err := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if err != nil {
		return nil, fmt.Errorf("promdiff: read: %w", err)
	}
	if len(data) > MaxInput {
		return nil, ErrTooLarge
	}
	var out []Sample
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.Trim(line, " \t")
		if line == "" || line[0] == '#' {
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %v", ErrSyntax, i+1, err)
		}
		id := seriesID(s.Name, s.Labels)
		if seen[id] {
			return nil, fmt.Errorf("%w: line %d: duplicate series", ErrSyntax, i+1)
		}
		seen[id] = true
		out = append(out, s)
	}
	return out, nil
}

func parseLine(line string) (Sample, error) {
	s := Sample{Labels: map[string]string{}}
	n := 0
	for n < len(line) && isNameChar(line[n], n == 0, true) {
		n++
	}
	if n == 0 {
		return s, errors.New("bad metric name")
	}
	s.Name, line = line[:n], line[n:]
	if strings.HasPrefix(line, "{") {
		rest, err := parseLabels(line[1:], s.Labels)
		if err != nil {
			return s, err
		}
		line = rest
	}
	// После имени или «}» — хотя бы один пробел, затем значение и, может
	// быть, метка времени.
	if line == "" || (line[0] != ' ' && line[0] != '\t') {
		return s, errors.New("want value after name")
	}
	fields := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(fields) == 0 || len(fields) > 2 {
		return s, errors.New("want value and optional timestamp")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return s, fmt.Errorf("bad value %q", fields[0])
	}
	if len(fields) == 2 {
		if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
			return s, fmt.Errorf("bad timestamp %q", fields[1])
		}
	}
	s.Value = v
	return s, nil
}

// parseLabels разбирает метки после «{» и возвращает остаток строки после «}».
func parseLabels(s string, into map[string]string) (string, error) {
	names := map[string]bool{} // повтор имени ловим и у пустых значений
	s = skipBlanks(s)
	for {
		if strings.HasPrefix(s, "}") {
			return s[1:], nil
		}
		n := 0
		for n < len(s) && isNameChar(s[n], n == 0, false) {
			n++
		}
		if n == 0 {
			return "", errors.New("bad label name")
		}
		name := s[:n]
		s = s[n:]
		if !strings.HasPrefix(s, `="`) {
			return "", fmt.Errorf("label %s: want =\"", name)
		}
		s = s[2:]
		var val strings.Builder
		closed := false
		for len(s) > 0 && !closed {
			c := s[0]
			s = s[1:]
			switch c {
			case '"':
				closed = true
			case '\\':
				if s == "" {
					return "", errors.New("unterminated label value")
				}
				switch s[0] {
				case '\\':
					val.WriteByte('\\')
				case '"':
					val.WriteByte('"')
				case 'n':
					val.WriteByte('\n')
				default:
					return "", fmt.Errorf("bad escape \\%c", s[0])
				}
				s = s[1:]
			default:
				val.WriteByte(c)
			}
		}
		if !closed {
			return "", errors.New("unterminated label value")
		}
		if names[name] {
			return "", fmt.Errorf("duplicate label %s", name)
		}
		names[name] = true
		// Пустое значение метки — то же, что её отсутствие (как в Prometheus).
		if v := val.String(); v != "" {
			into[name] = v
		}
		s = skipBlanks(s)
		switch {
		case strings.HasPrefix(s, ","):
			s = skipBlanks(s[1:])
		case strings.HasPrefix(s, "}"):
		default:
			return "", errors.New("want , or }")
		}
	}
}

func skipBlanks(s string) string { return strings.TrimLeft(s, " \t") }

func isNameChar(c byte, first, colon bool) bool {
	switch {
	case c == '_', 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		return true
	case c == ':':
		return colon
	case '0' <= c && c <= '9':
		return !first
	}
	return false
}

// seriesID — ключ ряда: имя и непустые метки в порядке имён.
func seriesID(name string, labels map[string]string) string {
	var b strings.Builder
	b.WriteString(name)
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		if labels[k] == "" {
			continue
		}
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte(0)
		b.WriteString(labels[k])
	}
	return b.String()
}

// Increase — прирост счётчика name между снимками по правилам rate в
// Prometheus: каждый ряд отдельно, сброс счётчика — отсчёт от нуля.
func Increase(before, after []Sample, name string, match map[string]string) float64 {
	prev := map[string]float64{}
	for _, s := range before {
		if s.Name == name {
			prev[seriesID(s.Name, s.Labels)] = s.Value
		}
	}
	var sum float64
next:
	for _, s := range after {
		if s.Name != name {
			continue
		}
		for k, v := range match {
			if s.Labels[k] != v {
				continue next
			}
		}
		b, ok := prev[seriesID(s.Name, s.Labels)]
		if !valid(s.Value) || (ok && !valid(b)) {
			continue
		}
		switch {
		case !ok: // ряд появился за шаг: счётчик начался с нуля
			sum += s.Value
		case s.Value < b: // сброс (рестарт процесса)
			sum += s.Value
		default:
			sum += s.Value - b
		}
	}
	return sum
}

func valid(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

// Summarize считает сводку шага длительностью seconds.
func Summarize(before, after []Sample, seconds float64) (Summary, error) {
	if !(seconds > 0) || math.IsInf(seconds, 0) {
		return Summary{}, ErrDuration
	}
	inc := func(name string, kv ...string) float64 {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return Increase(before, after, name, m)
	}
	const lookups, history, handled = "orders_catalog_cache_lookups_total", "orders_history_reads_total", "grpc_server_handled_total"
	var s Summary
	s.DBQueries = inc("orders_catalog_db_queries_total")
	s.DBPerSecond = s.DBQueries / seconds
	s.CacheHits = inc(lookups, "result", "hit") + inc(lookups, "result", "negative_hit")
	s.CacheLookups = s.CacheHits + inc(lookups, "result", "miss")
	s.HitRatio = ratio(s.CacheHits, s.CacheLookups)
	s.ReplicaReads = inc(history, "target", "replica")
	s.HistoryReads = s.ReplicaReads + inc(history, "target", "primary")
	s.ReplicaRatio = ratio(s.ReplicaReads, s.HistoryReads)
	create := []string{"grpc_service", "orders.v1.OrderService", "grpc_method", "CreateOrder"}
	s.CreateCalls = inc(handled, create...)
	s.Shed = inc(handled, append(create, "grpc_code", "ResourceExhausted")...)
	s.ShedRatio = ratio(s.Shed, s.CreateCalls)
	return s, nil
}

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
