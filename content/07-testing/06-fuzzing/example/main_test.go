// Тесты — во внешнем пакете `rle_test`: они видят только
// экспортируемый API. Модуль песочницы называется `lesson`.

package rle_test

import (
	"strings"
	"testing"

	rle "lesson"
)

// Таблица проверяет конкретные ответы — и проходит.
// Все входы придумал человек, и ни в одном нет цифр.
func TestEncode(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"aaab", "3a1b"},
		{"abc", "1a1b1c"},
		{"aabccc", "2a1b3c"},
	}
	for _, tt := range tests {
		if got := rle.Encode(tt.in); got != tt.want {
			t.Errorf("Encode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Fuzz-тест проверяет не ответ, а свойство, верное для
// любого входа: декодирование отменяет кодирование.
func FuzzRoundTrip(f *testing.F) {
	// Seed-корпус: с него фаззер начинает мутации.
	// Без флага `-fuzz` прогоняются только эти входы
	// и файлы из `testdata/fuzz/FuzzRoundTrip/` —
	// fuzz-тест работает как обычный табличный.
	f.Add("aaab")
	f.Add("hello, world")
	f.Add(strings.Repeat("z", 25))

	// Аргументы после `t` — типы входов. Они должны
	// совпадать с типами в `f.Add`.
	f.Fuzz(func(t *testing.T, s string) {
		enc := rle.Encode(s)
		dec, err := rle.Decode(enc)
		if err != nil {
			t.Fatalf("Decode(%q): %v", enc, err)
		}
		if dec != s {
			t.Errorf("Decode(Encode(%q)) = %q", s, dec)
		}
	})
}
