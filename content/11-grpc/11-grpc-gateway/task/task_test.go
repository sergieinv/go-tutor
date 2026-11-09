package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// seed — заказы для тестов; NewServer получает их не по порядку id.
func seed() *Server {
	return NewServer(
		&Order{Id: 3, CustomerId: "c1", TotalCents: 300, Status: OrderStatus_ORDER_STATUS_PAID},
		&Order{Id: 1, CustomerId: "c1", TotalCents: 1500, Status: OrderStatus_ORDER_STATUS_NEW},
		&Order{Id: 2, CustomerId: "c2", TotalCents: 700, Status: OrderStatus_ORDER_STATUS_PAID},
		&Order{Id: 5, CustomerId: "c1", TotalCents: 100, Status: OrderStatus_ORDER_STATUS_CANCELLED},
	)
}

func gateway(t *testing.T, srv OrderServiceServer) http.Handler {
	t.Helper()
	h, err := NewGateway(context.Background(), srv)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	return h
}

type reply struct {
	code   int
	header http.Header
	body   map[string]any
	raw    string
}

// do выполняет запрос к обработчику; rid — заголовок X-Request-Id ("" — без него).
func do(t *testing.T, h http.Handler, method, path, body, rid string) reply {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if rid != "" {
		req.Header.Set("X-Request-Id", rid)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := reply{code: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	if err := json.Unmarshal(rec.Body.Bytes(), &r.body); err != nil {
		t.Fatalf("%s %s: тело не JSON-объект: %q", method, path, r.raw)
	}
	return r
}

// apiError достаёт поля {"error": {...}} из ответа.
func apiError(t *testing.T, r reply) (code, message, rid string) {
	t.Helper()
	e, ok := r.body["error"].(map[string]any)
	if !ok {
		t.Fatalf("нет объекта error: %s", r.raw)
	}
	for _, k := range []string{"code", "message", "request_id"} {
		if _, ok := e[k].(string); !ok {
			t.Fatalf("error.%s отсутствует или не строка: %s", k, r.raw)
		}
	}
	return e["code"].(string), e["message"].(string), e["request_id"].(string)
}

// ids — id заказов из ответа ListOrders (в JSON int64 — строки).
func ids(t *testing.T, r reply) []string {
	t.Helper()
	list, ok := r.body["orders"].([]any)
	if !ok {
		t.Fatalf("нет массива orders: %s", r.raw)
	}
	var out []string
	for _, o := range list {
		id, _ := o.(map[string]any)["id"].(string)
		out = append(out, id)
	}
	return out
}

func TestGetOrder(t *testing.T) {
	r := do(t, gateway(t, seed()), "GET", "/v1/orders/1", "", "")
	if r.code != 200 {
		t.Fatalf("статус %d: %s", r.code, r.raw)
	}
	o, _ := r.body["order"].(map[string]any)
	if o["id"] != "1" || o["customer_id"] != "c1" || o["status"] != "ORDER_STATUS_NEW" {
		t.Errorf("заказ %v, want id \"1\", customer_id c1, ORDER_STATUS_NEW", o)
	}
}

func TestNotFoundError(t *testing.T) {
	r := do(t, gateway(t, seed()), "GET", "/v1/orders/42", "", "r-1")
	if r.code != 404 {
		t.Fatalf("статус %d, want 404: %s", r.code, r.raw)
	}
	code, msg, rid := apiError(t, r)
	if code != "NOT_FOUND" || msg != "order 42 not found" || rid != "r-1" {
		t.Errorf("error = {%s, %q, %q}, want {NOT_FOUND, \"order 42 not found\", \"r-1\"}", code, msg, rid)
	}
}

func TestCustomerOrdersRoute(t *testing.T) {
	r := do(t, gateway(t, seed()), "GET", "/v1/customers/c1/orders", "", "")
	if r.code != 200 {
		t.Fatalf("статус %d: %s", r.code, r.raw)
	}
	if got := ids(t, r); !slices.Equal(got, []string{"1", "3", "5"}) {
		t.Errorf("id = %q, want [1 3 5]", got)
	}
}
