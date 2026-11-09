package task

import (
	"io"
	"strconv"
	"sync"
)

type Item struct {
	SKU        string
	Qty        int
	PriceCents int64
}

type Order struct {
	ID       int64
	Customer string
	Items    []Item
	Paid     bool
}

// AppendOrder дописывает к dst JSON-объект заказа.
func AppendOrder(dst []byte, o *Order) []byte {
	var total int64
	b := append(dst, `{"id":`...)
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
		b = strconv.AppendInt(b, it.PriceCents, 10)
		b = append(b, '}')
		total += int64(it.Qty) * it.PriceCents
	}
	b = append(b, `],"paid":`...)
	b = strconv.AppendBool(b, o.Paid)
	b = append(b, `,"total":`...)
	b = strconv.AppendInt(b, total, 10)
	return append(b, '}')
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

// Пул хранит указатели на срезы: Put(*[]byte) не выделяет память.
var bufPool = sync.Pool{New: func() any {
	b := make([]byte, 0, 4<<10)
	return &b
}}

// WriteOrders пишет JSON-массив заказов одним вызовом Write.
func WriteOrders(w io.Writer, orders []Order) error {
	bp := bufPool.Get().(*[]byte)
	b := append((*bp)[:0], '[')
	for i := range orders {
		if i > 0 {
			b = append(b, ',')
		}
		b = AppendOrder(b, &orders[i])
	}
	b = append(b, "]\n"...)
	_, err := w.Write(b)
	if cap(b) <= 64<<10 { // огромные буферы в пул не возвращаем
		*bp = b
		bufPool.Put(bp)
	}
	return err
}
