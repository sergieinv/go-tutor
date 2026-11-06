package task

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Parse читает конфигурацию вида «ключ = значение».
func Parse(r io.Reader) (map[string]string, error) {
	sc := bufio.NewScanner(r)
	// Поднимаем лимит длины строки с 64 КБ до 2 МиБ.
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	cfg := map[string]string{}
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("line %d: ожидается ключ = значение", n)
		}
		cfg[key] = strings.TrimSpace(value)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение конфигурации: %w", err)
	}
	return cfg, nil
}

// Load открывает файл и разбирает его через Parse.
func Load(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // файл открыт только на чтение — ошибку Close можно не проверять
	return Parse(f)
}
