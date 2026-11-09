// Package promtext — минимальный разбор текстового формата метрик
// Prometheus (то, что отдаёт /metrics): имя, метки, значение. Хватает,
// чтобы снять счётчики сервиса до и после прогона и посчитать разницу.
// Гистограммы видны как отдельные ряды _bucket/_sum/_count.
package promtext

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
)

// Sample — один ряд: name{labels} value.
type Sample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Snapshot — все ряды одного снятия.
type Snapshot []Sample

// Parse читает метрики. Комментарии (# HELP, # TYPE) пропускаются,
// метки времени после значения игнорируются.
func Parse(r io.Reader) (Snapshot, error) {
	var out Snapshot
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func parseLine(line string) (Sample, error) {
	s := Sample{Labels: map[string]string{}}
	rest := line
	if i := strings.IndexAny(line, "{ "); i < 0 {
		return s, fmt.Errorf("no value in %q", line)
	} else {
		s.Name, rest = line[:i], line[i:]
	}
	if strings.HasPrefix(rest, "{") {
		end, err := parseLabels(rest, s.Labels)
		if err != nil {
			return s, err
		}
		rest = rest[end:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, fmt.Errorf("no value in %q", line)
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return s, fmt.Errorf("bad value in %q", line)
	}
	s.Value = v
	return s, nil
}

// parseLabels разбирает {a="1",b="x\"y"} и возвращает индекс за '}'.
func parseLabels(s string, into map[string]string) (int, error) {
	i := 1
	for i < len(s) {
		if s[i] == '}' {
			return i + 1, nil
		}
		if s[i] == ',' || s[i] == ' ' {
			i++
			continue
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 || i+eq+1 >= len(s) || s[i+eq+1] != '"' {
			return 0, fmt.Errorf("bad labels in %q", s)
		}
		name := s[i : i+eq]
		i += eq + 2
		var val strings.Builder
		for ; i < len(s) && s[i] != '"'; i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					val.WriteByte('\n')
				default:
					val.WriteByte(s[i])
				}
				continue
			}
			val.WriteByte(s[i])
		}
		if i >= len(s) {
			return 0, fmt.Errorf("unterminated label value in %q", s)
		}
		into[name] = val.String()
		i++ // закрывающая кавычка
	}
	return 0, fmt.Errorf("unterminated labels in %q", s)
}

// Sum — сумма значений рядов name, у которых совпадают все метки match
// (пустой match — все ряды).
func (s Snapshot) Sum(name string, match map[string]string) float64 {
	var sum float64
next:
	for _, x := range s {
		if x.Name != name {
			continue
		}
		for k, v := range match {
			if x.Labels[k] != v {
				continue next
			}
		}
		sum += x.Value
	}
	return sum
}

// Increase — прирост рядов name, подходящих под match, от снимка before
// до s, как increase в Prometheus: каждый ряд отдельно; ряд, которого не
// было, и ряд, значение которого уменьшилось (процесс перезапустился и
// счётчик начался с нуля), считаются от нуля. Разность сумм так не умеет:
// сброс одного ряда она прячет за ростом остальных.
func (s Snapshot) Increase(before Snapshot, name string, match map[string]string) float64 {
	var inc float64
	for _, x := range s.filter(name, match) {
		v := x.Value
		for _, y := range before.filter(name, match) {
			if maps.Equal(x.Labels, y.Labels) {
				if y.Value <= v {
					v -= y.Value
				}
				break
			}
		}
		inc += v
	}
	return inc
}

func (s Snapshot) filter(name string, match map[string]string) Snapshot {
	var out Snapshot
next:
	for _, x := range s {
		if x.Name != name {
			continue
		}
		for k, v := range match {
			if x.Labels[k] != v {
				continue next
			}
		}
		out = append(out, x)
	}
	return out
}

// Has — есть ли хотя бы один ряд с таким именем.
func (s Snapshot) Has(name string) bool {
	for _, x := range s {
		if x.Name == name {
			return true
		}
	}
	return false
}
