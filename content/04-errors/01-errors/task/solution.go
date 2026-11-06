package task

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseConfig разбирает строки вида "key=value" в map.
// Пустые строки пропускаются. При ошибке возвращает nil
// и ошибку с текстом "line N: ...", где N — номер строки с 1.
func ParseConfig(lines []string) (map[string]int, error) {
	cfg := make(map[string]int)
	for i, line := range lines {
		n := i + 1
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: missing '='", n)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", n)
		}
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			// Контекст — номер строки и ключ, причина — текст ошибки Atoi.
			return nil, fmt.Errorf("line %d: key %q: %v", n, key, err)
		}
		if _, dup := cfg[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", n, key)
		}
		cfg[key] = v
	}
	return cfg, nil
}
