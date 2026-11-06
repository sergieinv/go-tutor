package task

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseKV разбирает строку вида "a=1; b = 2;c=3" в map.
// Пары разделены ';', ключ и значение — первым '='.
// Пробелы игнорируются, пустые пары пропускаются,
// ключи приводятся к нижнему регистру.
// Ошибка — если нет '=', ключ пустой или значение не целое.
func ParseKV(s string) (map[string]int, error) {
	result := make(map[string]int)
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		// Cut делит по первому '=' и сообщает, был ли он.
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("пара %q: нет '='", pair)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, fmt.Errorf("пара %q: пустой ключ", pair)
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("пара %q: %w", pair, err)
		}
		result[key] = n
	}
	return result, nil
}
