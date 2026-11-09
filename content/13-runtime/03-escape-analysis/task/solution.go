package task

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// ErrEmpty — Mean получила пустой срез.
var ErrEmpty = errors.New("empty input")

// AppendKey дописывает к dst ключ вида "user:id" и возвращает
// получившийся срез — как append.
func AppendKey(dst []byte, user string, id int) []byte {
	// fmt.Appendf упаковывает user и id в any — это выделения в куче.
	// append и strconv.AppendInt пишут прямо в dst.
	dst = append(dst, user...)
	dst = append(dst, ':')
	return strconv.AppendInt(dst, int64(id), 10)
}

// ParseKV делит строку по первому '=' на ключ и значение и обрезает
// пробельные символы по их краям. Если '=' нет — "", "", false.
func ParseKV(line string) (key, value string, ok bool) {
	// strings.SplitN возвращает новый срез []string — он в куче.
	// strings.Cut отдаёт две подстроки исходной строки, без копий.
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

// SortByLen сортирует слова на месте: по длине в байтах,
// равные по длине — лексикографически.
func SortByLen(words []string) {
	// sort.Sort принимает интерфейс: заголовок среза byLen копируется
	// в кучу. slices.SortFunc — дженерик, интерфейса нет.
	slices.SortFunc(words, func(a, b string) int {
		if c := cmp.Compare(len(a), len(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
}

// Mean возвращает среднее арифметическое. Для пустого среза —
// 0 и ошибку, для которой errors.Is(err, ErrEmpty).
func Mean(xs []float64) (float64, error) {
	if len(xs) == 0 {
		// fmt.Errorf создаёт новую ошибку на каждый вызов,
		// а сигнальная ошибка уже создана — один раз, при старте.
		return 0, ErrEmpty
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs)), nil
}
