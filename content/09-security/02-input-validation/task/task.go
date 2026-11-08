package task

import (
	"errors"
	"io"
)

// MaxBody — наибольший допустимый размер тела в байтах.
const MaxBody = 16 << 10

// Item — позиция заказа.
type Item struct {
	SKU        string `json:"sku"`
	Qty        int64  `json:"qty"`
	PriceCents int64  `json:"price_cents"`
}

// Order — тело запроса «оформить заказ».
type Order struct {
	Customer string `json:"customer"`
	Note     string `json:"note"`
	Items    []Item `json:"items"`
}

var (
	ErrTooLarge = errors.New("body too large")
	ErrInvalid  = errors.New("invalid order")
)

// ParseOrder читает заказ из r, проверяет его и считает сумму в копейках.
func ParseOrder(r io.Reader) (Order, int64, error) {
	// TODO: реализуйте функцию
	return Order{}, 0, nil
}
