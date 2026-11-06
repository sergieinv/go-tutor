// Маленькие интерфейсы стандартной библиотеки:
// реализуйте один метод — и тип заработает со всеми
// функциями, которые этот интерфейс принимают.

package main

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
)

type Money struct {
	Cents    int64
	Currency string
}

// `fmt.Stringer`: `fmt` вызывает `String()` для `%v`,
// `%s` и `Println`.
func (m Money) String() string {
	return fmt.Sprintf("%d.%02d %s", m.Cents/100, m.Cents%100, m.Currency)
}

// Свой `io.Writer`: считает байты и пересылает их дальше.
// Любой код, умеющий писать в `io.Writer`, умеет и в него.
type countWriter struct {
	w io.Writer
	n int
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// `sort.Interface` — старый способ сортировки: тип
// реализует `Len`, `Less` и `Swap`.
type byCents []Money

func (b byCents) Len() int           { return len(b) }
func (b byCents) Less(i, j int) bool { return b[i].Cents < b[j].Cents }
func (b byCents) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func main() {
	fmt.Println(Money{12345, "RUB"})

	// `strings.NewReader` — `io.Reader` поверх строки,
	// `bytes.Buffer` — `io.Writer` (и `Reader`) в памяти.
	// `io.Copy` гонит байты из любого Reader в любой Writer.
	var buf bytes.Buffer
	n, err := io.Copy(&buf, strings.NewReader("hello, reader"))
	fmt.Println(n, err, buf.String())

	// Ручное чтение: `Read` заполняет буфер и сообщает,
	// сколько байт положил. Конец данных — `io.EOF`.
	r := strings.NewReader("abcdefg")
	chunk := make([]byte, 3)
	for {
		n, err := r.Read(chunk)
		fmt.Printf("%d %q %v\n", n, chunk[:n], err)
		if err == io.EOF {
			break
		}
	}

	// Обёртки комбинируются: `fmt.Fprintf` пишет в наш
	// счётчик, тот — в `os.Stdout`.
	cw := &countWriter{w: os.Stdout}
	fmt.Fprintf(cw, "%s\n", Money{500, "USD"})
	fmt.Println("written:", cw.n)

	// `io.MultiWriter` раздаёт запись в несколько Writer.
	var a, b bytes.Buffer
	io.WriteString(io.MultiWriter(&a, &b), "twice")
	fmt.Println(a.String(), b.String())

	prices := []Money{{300, "USD"}, {100, "USD"}, {200, "USD"}}
	sort.Sort(byCents(prices))
	fmt.Println(prices)

	// Современный способ — `slices.SortFunc` с функцией
	// сравнения: не нужен отдельный тип с тремя методами.
	slices.SortFunc(prices, func(x, y Money) int {
		return cmp.Compare(y.Cents, x.Cents)
	})
	fmt.Println(prices)
}
