// Примеры пишут во внешнем пакете `money_test`: так они
// выглядят ровно как код пользователя пакета.

package money_test

import (
	"fmt"
	"os"
	"slices"
	"testing"

	money "lesson"
)

// Имя связывает пример с идентификатором:
// `ExampleAmount_String` — метод `String` типа `Amount`.
// `go test` перехватывает stdout и сравнивает его
// с комментарием `// Output:` (пробелы по краям
// не учитываются).
func ExampleAmount_String() {
	fmt.Println(money.Amount(1234))
	fmt.Println(money.Amount(5))
	// Output:
	// 12.34
	// 0.05
}

// Суффикс после `_` со строчной буквы задаёт ещё один
// пример к тому же методу.
func ExampleAmount_String_negative() {
	fmt.Println(money.Amount(-5))
	// Output: -0.05
}

func ExampleAmount_Split() {
	fmt.Println(money.Amount(1000).Split(3))
	// Output: [3.34 3.33 3.33]
}

// Порядок обхода map случаен — для такого вывода
// есть `// Unordered output:`: строки сравниваются
// без учёта порядка.
func ExampleTotals() {
	t := money.Totals(map[string][]money.Amount{
		"еда":       {35000, 12050},
		"транспорт": {6200},
	})
	for cat, sum := range t {
		fmt.Println(cat, sum)
	}
	// Unordered output:
	// транспорт 62.00
	// еда 470.50
}

// Пример без комментария `Output` компилируется, но не
// запускается: годится для кода, который нельзя
// выполнить в тесте (сеть, файлы, `os.Exit`).
// `Example` без имени — пример ко всему пакету.
func Example() {
	share := money.Amount(150000).Split(4)
	fmt.Fprintln(os.Stderr, "к оплате каждому:", share[0])
	os.Exit(0)
}

// Обычные тесты никуда не деваются. Нажмите «Покрытие»:
// ветка `n <= 0` в `Split` не выполнялась ни разу —
// отчёт `go tool cover -func` покажет меньше 100%.
func TestSplitSum(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7} {
		parts := money.Amount(1001).Split(n)
		var sum money.Amount
		for _, p := range parts {
			sum += p
		}
		if sum != 1001 || len(parts) != n {
			t.Errorf("Split(%d) = %v: сумма %v", n, parts, sum)
		}
		if !slices.IsSortedFunc(parts, func(a, b money.Amount) int { return int(b - a) }) {
			t.Errorf("Split(%d) = %v: лишние копейки не в начале", n, parts)
		}
	}
}
