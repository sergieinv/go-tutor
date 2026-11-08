package task

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parse(body string) (Order, int64, error) {
	return ParseOrder(strings.NewReader(body))
}

func TestParseValid(t *testing.T) {
	o, total, err := parse(`{
		"customer": "Анна",
		"note": "позвонить за час",
		"items": [
			{"sku": "BOOK-1", "qty": 2, "price_cents": 1500},
			{"sku": "PEN-77", "qty": 10, "price_cents": 99}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseOrder корректного заказа: %v", err)
	}
	want := Order{
		Customer: "Анна",
		Note:     "позвонить за час",
		Items:    []Item{{"BOOK-1", 2, 1500}, {"PEN-77", 10, 99}},
	}
	if !reflect.DeepEqual(o, want) {
		t.Fatalf("заказ:\n got %+v\nwant %+v", o, want)
	}
	if total != 3990 {
		t.Fatalf("сумма = %d, want 3990", total)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct{ name, body string }{
		{"не JSON", `{"customer":`},
		{"пусто", ``},
		{"массив", `[]`},
		{"нет позиций", `{"customer":"Анна","items":[]}`},
		{"пустой покупатель", `{"customer":"  ","items":[{"sku":"A","qty":1,"price_cents":1}]}`},
		{"qty = 0", `{"customer":"Анна","items":[{"sku":"A","qty":0,"price_cents":1}]}`},
		{"строчные в sku", `{"customer":"Анна","items":[{"sku":"book","qty":1,"price_cents":1}]}`},
		{"qty строкой", `{"customer":"Анна","items":[{"sku":"A","qty":"1","price_cents":1}]}`},
	}
	for _, tt := range tests {
		o, total, err := parse(tt.body)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: ошибка %v, want ErrInvalid", tt.name, err)
		}
		if !reflect.DeepEqual(o, Order{}) || total != 0 {
			t.Errorf("%s: при ошибке вернулись %+v и %d, want Order{} и 0", tt.name, o, total)
		}
	}
}

func TestParseTooLarge(t *testing.T) {
	body := `{"customer":"Анна","note":"` + strings.Repeat("x", MaxBody) + `"}`
	if _, _, err := parse(body); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("тело %d байт: ошибка %v, want ErrTooLarge", len(body), err)
	}
}
