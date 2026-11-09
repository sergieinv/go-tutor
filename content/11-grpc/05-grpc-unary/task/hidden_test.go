package task

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHiddenValidation(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	bad := []struct {
		name  string
		items []*Item
	}{
		{"пустой sku", []*Item{{Quantity: 1, PriceCents: 1}}},
		{"количество 0", []*Item{{Sku: "tea", PriceCents: 1}}},
		{"отрицательное количество", []*Item{{Sku: "tea", Quantity: -1}}},
		{"отрицательная цена", []*Item{{Sku: "tea", Quantity: 1, PriceCents: -5}}},
		{"плохая вторая позиция", []*Item{{Sku: "tea", Quantity: 1}, {Sku: "", Quantity: 1}}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: "alice", Items: tt.items})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
			}
		})
	}
	// Неудачные попытки не расходуют номера, бесплатная позиция допустима.
	resp, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: "alice", Items: []*Item{{Sku: "gift", Quantity: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetOrder().GetId() != "ord-1" || resp.GetOrder().GetTotalCents() != 0 {
		t.Fatalf("после ошибок создан %v, want ord-1 с суммой 0", resp.GetOrder())
	}
	if _, err := c.GetOrder(ctx, &GetOrderRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetOrder без id: код = %v, want InvalidArgument", status.Code(err))
	}
}

func TestHiddenEmptyList(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	resp, err := c.ListOrders(ctx, &ListOrdersRequest{})
	if err != nil || len(resp.GetOrders()) != 0 {
		t.Fatalf("ListOrders на пустом сервисе = %v, %v; want пусто без ошибки", resp.GetOrders(), err)
	}
	if _, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: "bob", Items: []*Item{{Sku: "tea", Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	resp, err = c.ListOrders(ctx, &ListOrdersRequest{Customer: "carol"})
	if err != nil || len(resp.GetOrders()) != 0 {
		t.Fatalf("ListOrders(carol) = %v, %v; want пусто без ошибки", resp.GetOrders(), err)
	}
	resp, err = c.ListOrders(ctx, &ListOrdersRequest{})
	if err != nil || len(resp.GetOrders()) != 1 || resp.GetOrders()[0].GetCustomer() != "bob" {
		t.Fatalf("ListOrders() = %v, %v; want один заказ bob", resp.GetOrders(), err)
	}
}

func TestHiddenConcurrentCreate(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			_, err := c.CreateOrder(ctx, &CreateOrderRequest{
				Customer: fmt.Sprintf("c%d", i),
				Items:    []*Item{{Sku: "tea", Quantity: 1, PriceCents: int64(i)}},
			})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("CreateOrder: %v", err)
		}
	}
	resp, err := c.ListOrders(ctx, &ListOrdersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetOrders()) != n {
		t.Fatalf("заказов %d, want %d", len(resp.GetOrders()), n)
	}
	for i, o := range resp.GetOrders() {
		if want := fmt.Sprintf("ord-%d", i+1); o.GetId() != want {
			t.Fatalf("ListOrders[%d].Id = %q, want %q (порядок создания, номера без пропусков)", i, o.GetId(), want)
		}
	}
}

func TestHiddenMessageLimit(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	items := []*Item{{Sku: "tea", Quantity: 1}}
	if _, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: strings.Repeat("a", 60<<10), Items: items}); err != nil {
		t.Fatalf("запрос 60 КиБ: %v, want успех", err)
	}
	_, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: strings.Repeat("a", 70<<10), Items: items})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("запрос 70 КиБ: код = %v, want ResourceExhausted", status.Code(err))
	}
}
