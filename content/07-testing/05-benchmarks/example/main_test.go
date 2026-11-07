// Бенчмарки живут в тех же `_test.go`, что и тесты.
// `go test` их не запускает — нужен флаг `-bench`
// (в песочнице — кнопка «Бенчмарки»).

package main

import (
	"fmt"
	"strings"
	"testing"
)

// Сначала — корректность: быстрая, но неверная
// реализация никому не нужна.
func TestJoinSame(t *testing.T) {
	for _, parts := range [][]string{nil, {"a"}, {"a", "b", "c"}, {"", ""}} {
		want := strings.Join(parts, ", ")
		if got := JoinNaive(parts, ", "); got != want {
			t.Errorf("JoinNaive(%q) = %q, want %q", parts, got, want)
		}
		if got := JoinBuilder(parts, ", "); got != want {
			t.Errorf("JoinBuilder(%q) = %q, want %q", parts, got, want)
		}
	}
}

// `testing.AllocsPerRun` превращает «не больше N аллокаций»
// в обычную проверку: функция выполняется много раз,
// результат — среднее число аллокаций на вызов.
func TestJoinBuilderAllocs(t *testing.T) {
	parts := makeParts(100)
	allocs := testing.AllocsPerRun(100, func() {
		JoinBuilder(parts, ",")
	})
	if allocs > 1 {
		t.Errorf("JoinBuilder: %.0f аллокаций на вызов, want <= 1", allocs)
	}
}

func makeParts(n int) []string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("item%d", i)
	}
	return parts
}

// Бенчмарк — функция `BenchmarkXxx(b *testing.B)`.
// `b.Run` создаёт под-бенчмарки: каждая реализация на
// входах разного размера, чтобы увидеть рост, а не
// одну точку. Имя строится как в подтестах:
// `BenchmarkJoin/naive/n=100`.
func BenchmarkJoin(b *testing.B) {
	impls := []struct {
		name string
		fn   func([]string, string) string
	}{
		{"naive", JoinNaive},
		{"builder", JoinBuilder},
	}
	for _, size := range []int{10, 100, 1000} {
		// Подготовка — вне измеряемого цикла.
		parts := makeParts(size)
		for _, impl := range impls {
			b.Run(fmt.Sprintf("%s/n=%d", impl.name, size), func(b *testing.B) {
				// `ReportAllocs` добавляет B/op и allocs/op
				// даже без флага `-benchmem`.
				b.ReportAllocs()
				// `b.Loop()` (Go 1.24) сам решает, сколько
				// итераций нужно, и включает таймер только
				// на время цикла. Компилятор не выкинет вызов,
				// даже если результат не используется.
				for b.Loop() {
					impl.fn(parts, ",")
				}
			})
		}
	}
}
