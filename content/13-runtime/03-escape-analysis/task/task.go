package task

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrEmpty — Mean получила пустой срез.
var ErrEmpty = errors.New("empty input")

// Все функции ниже работают правильно, но выделяют память в куче
// на каждый вызов. Перепишите их так, чтобы выделений не было,
// а поведение осталось прежним.

// AppendKey дописывает к dst ключ вида "user:id" и возвращает
// получившийся срез — как append.
func AppendKey(dst []byte, user string, id int) []byte {
	return fmt.Appendf(dst, "%s:%d", user, id)
}

// ParseKV делит строку по первому '=' на ключ и значение и обрезает
// пробельные символы по их краям. Если '=' нет — "", "", false.
func ParseKV(line string) (key, value string, ok bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// SortByLen сортирует слова на месте: по длине в байтах,
// равные по длине — лексикографически.
func SortByLen(words []string) {
	sort.Sort(byLen(words))
}

type byLen []string

func (s byLen) Len() int      { return len(s) }
func (s byLen) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byLen) Less(i, j int) bool {
	if len(s[i]) != len(s[j]) {
		return len(s[i]) < len(s[j])
	}
	return s[i] < s[j]
}

// Mean возвращает среднее арифметическое. Для пустого среза —
// 0 и ошибку, для которой errors.Is(err, ErrEmpty).
func Mean(xs []float64) (float64, error) {
	if len(xs) == 0 {
		return 0, fmt.Errorf("mean: %w", ErrEmpty)
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs)), nil
}
