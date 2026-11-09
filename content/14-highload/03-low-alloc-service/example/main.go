// Горячий обработчик сервиса заказов — «до» и «после».
// Пример запускается через `go test`: тесты проверяют, что обе
// версии отвечают одинаково, и считают аллокации на запрос через
// `testing.AllocsPerRun`. Бенчмарки — по кнопке «Бенчмарки».
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
)

type Item struct {
	SKU   string `json:"sku"`
	Qty   int    `json:"qty"`
	Price int64  `json:"price"`
}

type Order struct {
	ID       int64  `json:"id"`
	Customer string `json:"customer"`
	Items    []Item `json:"items"`
}

// **До.** Так пишут часто: ответ собирают в `map[string]any`.
// Каждый вызов создаёт map, упаковывает значения в интерфейсы,
// а кодировщик сортирует ключи и обходит всё рефлексией.
func renderSlow(w io.Writer, o *Order) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"order": o,
		"count": len(o.Items),
	})
}

// Типизированный ответ — уже лучше: ни map, ни упаковки. Свой
// буфер `json.Encoder` берёт из внутреннего пула, но поля всё
// равно обходит рефлексией.
type orderResponse struct {
	Count int    `json:"count"`
	Order *Order `json:"order"`
}

func renderTyped(w io.Writer, o *Order) error {
	return json.NewEncoder(w).Encode(orderResponse{Count: len(o.Items), Order: o})
}

func statusSlow(o *Order) string {
	return fmt.Sprintf("order %d: %d items", o.ID, len(o.Items))
}

// **После.** Буфер ответа берём из `sync.Pool` (указатель на
// срез — чтобы `Put` не упаковывал заголовок среза в `any`),
// JSON собираем `append`-функциями и `strconv.Append*` прямо
// в буфер, отдаём одним `Write`.
var bufPool = sync.Pool{New: func() any {
	b := make([]byte, 0, 4<<10)
	return &b
}}

func renderFast(w io.Writer, o *Order) error {
	bp := bufPool.Get().(*[]byte)
	b := append((*bp)[:0], `{"count":`...)
	b = strconv.AppendInt(b, int64(len(o.Items)), 10)
	b = append(b, `,"order":`...)
	b = appendOrder(b, o)
	b = append(b, "}\n"...)
	_, err := w.Write(b)
	// Огромный буфер в пул не возвращаем: он держал бы память,
	// пока пул не очистит сборщик мусора.
	if cap(b) <= 64<<10 {
		*bp = b
		bufPool.Put(bp)
	}
	return err
}

// Ручная сериализация знает схему заранее — рефлексии нет. Строки
// экранируем по правилам JSON; `<`, `>` и `&` оставляем как есть,
// как `json.Encoder` с `SetEscapeHTML(false)`.
func appendOrder(b []byte, o *Order) []byte {
	b = append(b, `{"id":`...)
	b = strconv.AppendInt(b, o.ID, 10)
	b = append(b, `,"customer":`...)
	b = appendString(b, o.Customer)
	b = append(b, `,"items":[`...)
	for i, it := range o.Items {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"sku":`...)
		b = appendString(b, it.SKU)
		b = append(b, `,"qty":`...)
		b = strconv.AppendInt(b, int64(it.Qty), 10)
		b = append(b, `,"price":`...)
		b = strconv.AppendInt(b, it.Price, 10)
		b = append(b, '}')
	}
	return append(b, "]}"...)
}

func appendString(b []byte, s string) []byte {
	const hex = "0123456789abcdef"
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		default:
			b = append(b, c)
		}
	}
	return append(b, '"')
}

// `strconv.AppendInt` вместо `fmt.Sprintf`: пишет цифры в чужой
// буфер, ничего не упаковывая и не выделяя.
func statusFast(b []byte, o *Order) []byte {
	b = append(b, "order "...)
	b = strconv.AppendInt(b, o.ID, 10)
	b = append(b, ": "...)
	b = strconv.AppendInt(b, int64(len(o.Items)), 10)
	return append(b, " items"...)
}

// Обработчик — тонкая обёртка: заголовки и `renderFast`.
func handleOrder(o *Order) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := renderFast(w, o); err != nil {
			return // клиент ушёл — писать некуда
		}
	}
}

func main() {}
