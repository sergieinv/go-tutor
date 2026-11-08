package orders

import (
	"errors"
	"testing"
)

func TestStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		from, to Status
		want     bool
	}{
		{StatusNew, StatusReserved, true},
		{StatusNew, StatusCancelled, true},
		{StatusReserved, StatusCancelled, true},
		{StatusReserved, StatusNew, false},
		{StatusCancelled, StatusReserved, false},
		{StatusCancelled, StatusCancelled, false},
		{Status("unknown"), StatusCancelled, false},
	}
	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
			t.Errorf("%q.CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestCreateOrderRequestValidate(t *testing.T) {
	item := ItemRequest{ProductID: 1, Quantity: 1}
	tooMany := make([]ItemRequest, MaxItems+1)
	for i := range tooMany {
		tooMany[i] = ItemRequest{ProductID: int64(i + 1), Quantity: 1}
	}

	tests := []struct {
		name      string
		req       CreateOrderRequest
		wantField string // "" — запрос корректен
	}{
		{"ok", CreateOrderRequest{CustomerID: 1, Items: []ItemRequest{item, {ProductID: 2, Quantity: MaxQuantity}}}, ""},
		{"no customer", CreateOrderRequest{Items: []ItemRequest{item}}, "customer_id"},
		{"negative customer", CreateOrderRequest{CustomerID: -1, Items: []ItemRequest{item}}, "customer_id"},
		{"no items", CreateOrderRequest{CustomerID: 1}, "items"},
		{"too many items", CreateOrderRequest{CustomerID: 1, Items: tooMany}, "items"},
		{"zero product", CreateOrderRequest{CustomerID: 1, Items: []ItemRequest{item, {Quantity: 1}}}, "items[1].product_id"},
		{"zero quantity", CreateOrderRequest{CustomerID: 1, Items: []ItemRequest{{ProductID: 1}}}, "items[0].quantity"},
		{"huge quantity", CreateOrderRequest{CustomerID: 1, Items: []ItemRequest{{ProductID: 1, Quantity: MaxQuantity + 1}}}, "items[0].quantity"},
		{"duplicate", CreateOrderRequest{CustomerID: 1, Items: []ItemRequest{item, {ProductID: 2, Quantity: 1}, item}}, "items[2].product_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("validate() = %v, want *ValidationError", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}
