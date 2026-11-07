package task

import (
	"errors"
	"io"
)

// Item — позиция чека.
type Item struct {
	Name  string
	Qty   int
	Price int64 // цена за штуку в копейках
}

// ErrInvalidItem — позиция с пустым названием, Qty < 1 или Price < 0.
var ErrInvalidItem = errors.New("receipt: неверная позиция")

// FormatMoney печатает сумму в копейках как рубли с двумя знаками.
func FormatMoney(kop int64) string {
	// TODO: реализуйте функцию
	return ""
}

// WriteReceipt печатает чек в w.
func WriteReceipt(w io.Writer, items []Item) error {
	// TODO: реализуйте функцию
	return nil
}
