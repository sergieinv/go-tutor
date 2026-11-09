package task

import "io"

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
	// TODO: strconv.Append*, свои append-функции для строк
	return dst
}

// WriteOrders пишет JSON-массив заказов одним вызовом Write.
func WriteOrders(w io.Writer, orders []Order) error {
	// TODO: буфер из sync.Pool, AppendOrder, один w.Write
	return nil
}
