package task

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func create(t *testing.T, a *api, customer string) *Order {
	t.Helper()
	resp, err := a.client.CreateOrder(testCtx(t), &CreateOrderRequest{CustomerId: customer, Items: []*OrderItem{item("APPLE-1", 1, 10)}})
	if err != nil {
		t.Fatalf("CreateOrder(%s): %v", customer, err)
	}
	return resp.GetOrder()
}

func ids(orders []*Order) []string {
	var out []string
	for _, o := range orders {
		out = append(out, o.GetId())
	}
	return out
}

func TestValidationAllMethods(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	many := make([]*OrderItem, 51)
	for i := range many {
		many[i] = item("APPLE-1", 1, 1)
	}
	tests := []struct {
		name string
		call func() error
		want []string
	}{
		{"GetOrder без id", func() error { _, err := a.client.GetOrder(ctx, &GetOrderRequest{}); return err }, []string{"id"}},
		{"CancelOrder без id", func() error { _, err := a.client.CancelOrder(ctx, &CancelOrderRequest{}); return err }, []string{"id"}},
		{"ListOrders без клиента", func() error { _, err := a.client.ListOrders(ctx, &ListOrdersRequest{}); return err }, []string{"customer_id"}},
		{"ListOrders page_size 101", func() error {
			_, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: 101})
			return err
		}, []string{"page_size"}},
		{"ListOrders page_size -1", func() error {
			_, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: -1})
			return err
		}, []string{"page_size"}},
		{"CreateOrder без позиций", func() error {
			_, err := a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: "c-1"})
			return err
		}, []string{"items"}},
		{"CreateOrder 51 позиция", func() error {
			_, err := a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: "c-1", Items: many})
			return err
		}, []string{"items"}},
		{"CreateOrder длинный клиент и цена", func() error {
			_, err := a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: strings.Repeat("c", 65), Items: []*OrderItem{item("APPLE-1", 1001, 100000001)}})
			return err
		}, []string{"customer_id", "items[0].price_cents", "items[0].quantity"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
			}
			if got := violations(err); !slices.Equal(got, tt.want) {
				t.Fatalf("поля BadRequest = %v, want %v", got, tt.want)
			}
			for _, d := range status.Convert(err).Details() {
				if br, ok := d.(*errdetails.BadRequest); ok {
					for _, fv := range br.GetFieldViolations() {
						if fv.GetDescription() == "" {
							t.Fatalf("у нарушения %s пустое описание", fv.GetField())
						}
					}
				}
			}
		})
	}
}

func TestIDsSkipFailedCreates(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	create(t, a, "c-1")
	_, _ = a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: "c-1"}) // невалидный
	req := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}
	if _, err := a.client.CreateOrder(withKey(ctx, "k"), req); err != nil {
		t.Fatal(err)
	}
	req2 := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 2, 1)}}
	_, _ = a.client.CreateOrder(withKey(ctx, "k"), req2) // AlreadyExists
	if o := create(t, a, "c-1"); o.GetId() != "ord-3" {
		t.Fatalf("id = %s, want ord-3: отказы не расходуют номера", o.GetId())
	}
}

func TestIdempotencyScope(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	req := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}
	r1, err1 := a.client.CreateOrder(ctx, req)
	r2, err2 := a.client.CreateOrder(ctx, req)
	if err1 != nil || err2 != nil || r1.GetOrder().GetId() == r2.GetOrder().GetId() {
		t.Fatalf("без ключа два вызова: %v %v, %v %v; want два разных заказа", r1.GetOrder().GetId(), err1, r2.GetOrder().GetId(), err2)
	}
	r3, err3 := a.client.CreateOrder(withKey(ctx, "a"), req)
	r4, err4 := a.client.CreateOrder(withKey(ctx, "b"), req)
	if err3 != nil || err4 != nil || r3.GetOrder().GetId() == r4.GetOrder().GetId() {
		t.Fatalf("разные ключи: %v %v, %v %v; want два разных заказа", r3.GetOrder().GetId(), err3, r4.GetOrder().GetId(), err4)
	}
	// Повтор с ключом не создаёт заказ: у клиента ровно 4.
	if _, err := a.client.CreateOrder(withKey(ctx, "a"), req); err != nil {
		t.Fatal(err)
	}
	list, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1"})
	if err != nil || len(list.GetOrders()) != 4 {
		t.Fatalf("заказов у c-1 = %v, %v; want 4", ids(list.GetOrders()), err)
	}
}

func TestIdempotencyKeyFormat(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	req := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}
	bad := map[string]metadata.MD{
		"пустой":       metadata.Pairs("idempotency-key", ""),
		"65 символов":  metadata.Pairs("idempotency-key", strings.Repeat("k", 65)),
		"два значения": metadata.Pairs("idempotency-key", "a", "idempotency-key", "b"),
	}
	for name, md := range bad {
		if _, err := a.client.CreateOrder(metadata.NewOutgoingContext(ctx, md), req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ключ %s: %v, want InvalidArgument", name, err)
		}
	}
	if _, err := a.client.CreateOrder(withKey(ctx, strings.Repeat("k", 64)), req); err != nil {
		t.Fatalf("ключ из 64 символов: %v", err)
	}
	// Невалидный запрос с уже использованным ключом — InvalidArgument, не AlreadyExists.
	if _, err := a.client.CreateOrder(withKey(ctx, strings.Repeat("k", 64)), &CreateOrderRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("невалидный запрос с использованным ключом: %v, want InvalidArgument", err)
	}
}

func TestIdempotentConcurrent(t *testing.T) {
	a := startAPI(t)
	ctx := withKey(testCtx(t), "same")
	req := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}
	var wg sync.WaitGroup
	got := make(chan string, 20)
	for range 20 {
		wg.Go(func() {
			resp, err := a.client.CreateOrder(ctx, req)
			if err != nil {
				got <- "error: " + err.Error()
				return
			}
			got <- resp.GetOrder().GetId()
		})
	}
	wg.Wait()
	close(got)
	for id := range got {
		if id != "ord-1" {
			t.Fatalf("конкурентные повторы с одним ключом: %s, want ord-1", id)
		}
	}
}

func TestListDefaultsAndFilter(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	var want []string
	for i := range 12 {
		want = append(want, create(t, a, "c-1").GetId())
		if i%3 == 0 {
			create(t, a, "c-2")
		}
	}
	p1, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1"})
	if err != nil || !slices.Equal(ids(p1.GetOrders()), want[:10]) || p1.GetNextPageToken() == "" {
		t.Fatalf("страница по умолчанию = %v, %q, %v; want %v и токен", ids(p1.GetOrders()), p1.GetNextPageToken(), err, want[:10])
	}
	p2, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageToken: p1.GetNextPageToken()})
	if err != nil || !slices.Equal(ids(p2.GetOrders()), want[10:]) || p2.GetNextPageToken() != "" {
		t.Fatalf("вторая страница = %v, %q, %v; want %v без токена", ids(p2.GetOrders()), p2.GetNextPageToken(), err, want[10:])
	}
	// Ровно на границе страницы токена нет.
	p3, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-2", PageSize: 4})
	if err != nil || len(p3.GetOrders()) != 4 || p3.GetNextPageToken() != "" {
		t.Fatalf("c-2, page_size 4 = %v, %q, %v; want 4 заказа без токена", ids(p3.GetOrders()), p3.GetNextPageToken(), err)
	}
	empty, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-3"})
	if err != nil || len(empty.GetOrders()) != 0 || empty.GetNextPageToken() != "" {
		t.Fatalf("клиент без заказов = %v, %q, %v", ids(empty.GetOrders()), empty.GetNextPageToken(), err)
	}
}

func TestListBadToken(t *testing.T) {
	a := startAPI(t)
	create(t, a, "c-1")
	_, err := a.client.ListOrders(testCtx(t), &ListOrdersRequest{CustomerId: "c-1", PageToken: "not-a-token"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("мусорный page_token: %v, want InvalidArgument", err)
	}
}

func TestListSeesNewOrders(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	for range 3 {
		create(t, a, "c-1")
	}
	p1, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	create(t, a, "c-1") // ord-4 появился между страницами
	p2, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: 2, PageToken: p1.GetNextPageToken()})
	if err != nil || !slices.Equal(ids(p2.GetOrders()), []string{"ord-3", "ord-4"}) || p2.GetNextPageToken() != "" {
		t.Fatalf("вторая страница = %v, %q, %v; want [ord-3 ord-4] без токена", ids(p2.GetOrders()), p2.GetNextPageToken(), err)
	}
}

func TestCancelNotFoundAndVisible(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	if _, err := a.client.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"}); status.Code(err) != codes.NotFound {
		t.Fatalf("отмена несуществующего: %v, want NotFound", err)
	}
	create(t, a, "c-1")
	if _, err := a.client.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := a.client.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	if err != nil || got.GetOrder().GetStatus() != OrderStatus_ORDER_STATUS_CANCELLED {
		t.Fatalf("GetOrder после отмены = %v, %v", got.GetOrder(), err)
	}
}

func restStatus(t *testing.T, raw []byte) *spb.Status {
	t.Helper()
	var st spb.Status
	decode(t, raw, &st)
	return &st
}

func TestRESTErrors(t *testing.T) {
	a := startAPI(t)
	code, raw := a.rest(t, "POST", "/v1/orders", `{"customerId":"","items":[]}`)
	if code != http.StatusBadRequest || restStatus(t, raw).GetCode() != int32(codes.InvalidArgument) {
		t.Fatalf("невалидный POST → %d %s, want 400 с кодом 3", code, raw)
	}
	if !strings.Contains(string(raw), "BadRequest") {
		t.Fatalf("в теле ошибки нет деталей BadRequest: %s", raw)
	}
	body := `{"customerId":"c-1","items":[{"sku":"APPLE-1","quantity":1,"priceCents":"5"}]}`
	if code, raw = a.rest(t, "POST", "/v1/orders", body, "Idempotency-Key", "r1"); code != http.StatusOK {
		t.Fatalf("POST → %d %s", code, raw)
	}
	other := `{"customerId":"c-1","items":[{"sku":"APPLE-1","quantity":9,"priceCents":"5"}]}`
	if code, raw = a.rest(t, "POST", "/v1/orders", other, "Idempotency-Key", "r1"); code != http.StatusConflict {
		t.Fatalf("тот же ключ, другое тело → %d %s, want 409", code, raw)
	}
	if code, raw = a.rest(t, "POST", "/v1/orders/ord-1:cancel", "{}"); code != http.StatusOK {
		t.Fatalf("отмена → %d %s", code, raw)
	}
	if code, raw = a.rest(t, "POST", "/v1/orders/ord-1:cancel", "{}"); code != http.StatusBadRequest || restStatus(t, raw).GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("повторная отмена → %d %s, want 400 с кодом 9", code, raw)
	}
	if code, raw = a.rest(t, "GET", "/v1/orders/nope", ""); code != http.StatusNotFound {
		t.Fatalf("GET несуществующего → %d %s, want 404", code, raw)
	}
}

func TestRESTList(t *testing.T) {
	a := startAPI(t)
	for range 3 {
		create(t, a, "c-1")
	}
	code, raw := a.rest(t, "GET", "/v1/customers/c-1/orders?page_size=2", "")
	var p1 ListOrdersResponse
	if decode(t, raw, &p1); code != http.StatusOK || len(p1.GetOrders()) != 2 || p1.GetNextPageToken() == "" {
		t.Fatalf("GET список → %d %s", code, raw)
	}
	code, raw = a.rest(t, "GET", fmt.Sprintf("/v1/customers/c-1/orders?page_size=2&page_token=%s", url.QueryEscape(p1.GetNextPageToken())), "")
	var p2 ListOrdersResponse
	if decode(t, raw, &p2); code != http.StatusOK || !slices.Equal(ids(p2.GetOrders()), []string{"ord-3"}) {
		t.Fatalf("GET вторая страница → %d %s", code, raw)
	}
	if code, raw = a.rest(t, "GET", "/v1/customers/c-1/orders?page_size=500", ""); code != http.StatusBadRequest {
		t.Fatalf("page_size=500 → %d %s, want 400", code, raw)
	}
}
