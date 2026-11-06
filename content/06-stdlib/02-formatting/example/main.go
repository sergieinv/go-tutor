// Пакет `fmt` форматирует по глаголам в стиле `printf`:
// `%v` — значение «как есть», `%d` — целое, `%s` — строка.
// Строковой интерполяции, как `$"..."` в C#, в Go нет.

package main

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
)

type point struct{ X, Y int }

// `Stringer` — метод `String() string`. `fmt` вызывает его
// для `%v` и `%s` — это аналог `ToString()`.
type celsius float64

func (c celsius) String() string { return fmt.Sprintf("%.1f°C", float64(c)) }

// `GoStringer` — метод `GoString()` для `%#v`: представление
// значения в виде Go-кода, удобно в отладке.
func (p point) GoString() string { return fmt.Sprintf("pt(%d, %d)", p.X, p.Y) }

func main() {
	p := point{1, 2}
	// `%v` — значения полей, `%+v` — с именами полей,
	// `%#v` — синтаксис Go (или `GoString`), `%T` — тип.
	fmt.Printf("%v | %+v | %#v | %T\n", p, p, p, p)

	// `%q` — строка в кавычках с экранированием, `%x` —
	// шестнадцатеричный вид чисел и байтов строки.
	fmt.Printf("%q %x %x %X\n", "tab\there", 255, "Go", 3054)

	// Ширина и точность: `%08.3f` — 8 символов, 3 знака
	// после точки, дополнить нулями; `%-10s` — по левому
	// краю; `*` берёт ширину из аргумента.
	fmt.Printf("[%08.3f] [%-10s] [%*d]\n", 3.14159, "left", 6, 42)

	// `Stringer` срабатывает и для `%v`, и в `Println`.
	t := celsius(21.55)
	fmt.Println(t, []celsius{-3, 0})

	// `Sprintf` возвращает строку, `Fprintf` пишет в любой
	// `io.Writer`, `Errorf` создаёт ошибку (с `%w` — обёртку).
	s := fmt.Sprintf("%d%%", 95)
	fmt.Fprintf(os.Stdout, "загрузка: %s\n", s)
	base := errors.New("нет места")
	err := fmt.Errorf("запись %q: %w", "log.txt", base)
	fmt.Println(err, errors.Is(err, base))

	// `Appendf` дописывает результат в `[]byte` без
	// промежуточной строки — удобно для буферов.
	buf := []byte("id=")
	buf = fmt.Appendf(buf, "%04d", 7)
	fmt.Println(string(buf))

	// Ошибка в глаголе не роняет программу: `fmt` вставит
	// пометку прямо в вывод. `go vet` ловит такие места.
	fmt.Printf("%d %s\n", "x", 1)

	// Текстовые таблицы — `text/tabwriter`: колонки
	// разделяются `\t`, ширина подбирается по самой длинной
	// ячейке. Не забудьте `Flush`.
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "товар\tкол-во\tцена\t")
	fmt.Fprintln(w, "кофе\t2\t350.00\t")
	fmt.Fprintln(w, "круассан\t10\t1200.50\t")
	w.Flush()
}
