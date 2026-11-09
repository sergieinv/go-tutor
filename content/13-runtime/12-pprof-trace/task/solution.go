package task

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// ErrFormat — текст не похож на профиль горутин в формате debug=1.
var ErrFormat = errors.New("goroutine profile: bad format")

// Site — функция пакета и число горутин, которые стоят в ней.
type Site struct {
	Func  string
	Count int
}

// Blocked разбирает профиль горутин (pprof.Lookup("goroutine").WriteTo(w, 1))
// и для каждой группы одинаковых стеков находит верхнюю функцию пакета pkg.
func Blocked(profile, pkg string) ([]Site, error) {
	lines := strings.Split(profile, "\n")
	if !strings.HasPrefix(lines[0], "goroutine profile: total ") {
		return nil, ErrFormat
	}
	prefix := pkg + "."
	counts := map[string]int{}
	count := 0     // число горутин в текущей группе
	found := false // верхняя функция пакета уже найдена
	inRecord := false

	for _, line := range lines[1:] {
		switch {
		case line == "":
			inRecord = false
		case strings.HasPrefix(line, "# labels:"):
			// метки pprof.Do — пропускаем
		case strings.HasPrefix(line, "#\t"):
			if !inRecord {
				return nil, ErrFormat
			}
			if found {
				continue
			}
			fn, ok := frameFunc(line)
			if !ok {
				return nil, ErrFormat
			}
			if strings.HasPrefix(fn, prefix) {
				counts[fn] += count
				found = true
			}
		default:
			// Заголовок группы: «3 @ 0x1007eff78 0x10078b0b0 ...».
			n, _, ok := strings.Cut(line, " @")
			if !ok {
				return nil, ErrFormat
			}
			c, err := strconv.Atoi(n)
			if err != nil || c <= 0 {
				return nil, ErrFormat
			}
			count, found, inRecord = c, false, true
		}
	}

	sites := make([]Site, 0, len(counts))
	for fn, c := range counts {
		sites = append(sites, Site{Func: fn, Count: c})
	}
	slices.SortFunc(sites, func(a, b Site) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Func, b.Func))
	})
	return sites, nil
}

// frameFunc достаёт имя функции из строки кадра:
// «#\t0x100845a33\tmain.(*server).handle+0x23\t/path/main.go:13».
// Поля разделены одной или несколькими табуляциями.
func frameFunc(line string) (string, bool) {
	var fields []string
	for f := range strings.SplitSeq(line, "\t") {
		if f != "" {
			fields = append(fields, f)
		}
	}
	if len(fields) < 3 {
		return "", false
	}
	fn := fields[2]
	if i := strings.LastIndex(fn, "+0x"); i > 0 {
		fn = fn[:i]
	}
	return fn, true
}
