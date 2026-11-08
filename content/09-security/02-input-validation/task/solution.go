package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
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
	// Читаем на байт больше лимита: так отличаем «ровно MaxBody»
	// от «больше», а бесконечный поток не прочитаем целиком.
	data, err := io.ReadAll(io.LimitReader(r, MaxBody+1))
	if err != nil {
		return Order{}, 0, err
	}
	if len(data) > MaxBody {
		return Order{}, 0, ErrTooLarge
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var o Order
	if err := dec.Decode(&o); err != nil {
		return Order{}, 0, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Order{}, 0, fmt.Errorf("%w: data after object", ErrInvalid)
	}
	if err := o.validate(); err != nil {
		return Order{}, 0, err
	}

	// Границы qty, цены и числа позиций проверены — сумма не
	// превысит 50 × 1000 × 10⁹ и в int64 не переполнится.
	var total int64
	for _, it := range o.Items {
		total += it.Qty * it.PriceCents
	}
	return o, total, nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func (o Order) validate() error {
	if strings.TrimSpace(o.Customer) == "" || utf8.RuneCountInString(o.Customer) > 100 {
		return invalid("customer")
	}
	if utf8.RuneCountInString(o.Note) > 500 {
		return invalid("note")
	}
	if len(o.Items) < 1 || len(o.Items) > 50 {
		return invalid("items: %d", len(o.Items))
	}
	for i, it := range o.Items {
		if !validSKU(it.SKU) {
			return invalid("items[%d].sku", i)
		}
		if it.Qty < 1 || it.Qty > 1000 {
			return invalid("items[%d].qty", i)
		}
		if it.PriceCents < 0 || it.PriceCents > 1_000_000_000 {
			return invalid("items[%d].price_cents", i)
		}
	}
	return nil
}

func validSKU(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
