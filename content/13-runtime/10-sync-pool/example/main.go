// `sync.Pool` — кеш временных объектов: взяли буфер, поработали,
// вернули — и следующий вызов не выделяет память заново. Пример
// запускается через `go test`: тесты показывают, что пул реально
// экономит аллокации, переживает одну сборку мусора и почему в
// него кладут указатели, а не срезы. Бенчмарки — по кнопке.
package main

import (
	"bytes"
	"strconv"
	"sync"
)

// Пул хранит `*bytes.Buffer`. `New` вызывается, когда пул пуст;
// без `New` метод `Get` вернул бы `nil`.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Формат строки отчёта без пула: каждый вызов растит свой буфер
// с нуля — внутренний массив буфера выделяется в куче.
func FormatNew(user string, items []int) string {
	var buf bytes.Buffer
	writeReport(&buf, user, items)
	return buf.String()
}

// С пулом: `Get` → **обязательный** `Reset` (в буфере остались
// данные прошлого вызова) → работа → `Put`. `buf.String()`
// копирует байты в новую строку, поэтому вернуть буфер в пул
// после неё безопасно. Остаётся одна аллокация — сама строка.
func FormatPooled(user string, items []int) string {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	writeReport(buf, user, items)
	s := buf.String()
	// Не возвращаем огромные буферы: один отчёт на мегабайт
	// иначе навсегда раздует каждый буфер пула.
	if buf.Cap() <= 64<<10 {
		bufPool.Put(buf)
	}
	return s
}

func writeReport(buf *bytes.Buffer, user string, items []int) {
	buf.WriteString(user)
	buf.WriteString(": ")
	for i, n := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(strconv.AppendInt(buf.AvailableBuffer(), int64(n), 10))
	}
}

// Пул байтовых срезов хранит **указатели** `*[]byte`. Срез — три
// слова, и `Put(b)` упаковал бы его в интерфейс `any` с аллокацией
// на каждом вызове; указатель помещается в интерфейс без неё.
var slicePool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

func main() {}
