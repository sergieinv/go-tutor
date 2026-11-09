package grpcapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	ordersv1 "gotutor/labs/api/gen/orders/v1"
	"gotutor/labs/orders/internal/grpcapi"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/storetest"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/httpx"
)

type harness struct {
	client  ordersv1.OrderServiceClient
	rest    http.Handler
	wh      *storetest.Warehouse
	product orders.Product
}

// newHarness поднимает сервер так же, как main: grpcx.NewServer с
// валидацией, gateway через gRPC-соединение (здесь — bufconn).
func newHarness(t *testing.T) *harness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err := store.CreateProduct(t.Context(), orders.NewProduct{Name: "Ручка", PriceCents: 15000})
	if err != nil {
		t.Fatal(err)
	}
	wh := storetest.NewWarehouse()
	svc := orders.NewService(store, wh, orders.Options{})

	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	srv := grpcx.NewServer(grpcx.ServerConfig{Logger: log, Validator: v})
	ordersv1.RegisterOrderServiceServer(srv, grpcapi.New(svc, log))
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpcx.NewClient("passthrough:///bufnet", grpcx.ClientConfig{},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	gw := httpx.NewGatewayMux(log)
	if err := ordersv1.RegisterOrderServiceHandler(t.Context(), gw, conn); err != nil {
		t.Fatal(err)
	}
	return &harness{client: ordersv1.NewOrderServiceClient(conn), rest: httpx.RequestID(gw), wh: wh, product: p}
}

func (h *harness) create(t *testing.T, customer int64, qty int32) *ordersv1.Order {
	t.Helper()
	resp, err := h.client.CreateOrder(t.Context(), &ordersv1.CreateOrderRequest{
		CustomerId: customer,
		Items:      []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: qty}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	return resp.GetOrder()
}

func detail[T any](st *status.Status) (T, bool) {
	for _, d := range st.Details() {
		if v, ok := d.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

func TestCreateGetCancel(t *testing.T) {
	h := newHarness(t)
	o := h.create(t, 7, 2)
	if o.GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_RESERVED || o.GetTotalCents() != 30000 || o.GetCreateTime() == nil {
		t.Errorf("created = %v", o)
	}
	got, err := h.client.GetOrder(t.Context(), &ordersv1.GetOrderRequest{OrderId: o.GetOrderId()})
	if err != nil || got.GetOrder().GetOrderId() != o.GetOrderId() {
		t.Fatalf("GetOrder = %v, %v", got, err)
	}
	c, err := h.client.CancelOrder(t.Context(), &ordersv1.CancelOrderRequest{OrderId: o.GetOrderId()})
	if err != nil || c.GetOrder().GetStatus() != ordersv1.OrderStatus_ORDER_STATUS_CANCELLED {
		t.Fatalf("CancelOrder = %v, %v", c, err)
	}
}

func TestErrors(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	// Валидация по правилам контракта — до обработчика.
	_, err := h.client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{CustomerId: 7, Items: []*ordersv1.CreateOrderItem{
		{ProductId: h.product.ID, Quantity: 1}, {ProductId: h.product.ID, Quantity: 1},
	}})
	st := status.Convert(err)
	br, ok := detail[*errdetails.BadRequest](st)
	if st.Code() != codes.InvalidArgument || !ok || br.GetFieldViolations()[0].GetField() != "items" {
		t.Errorf("duplicate items: %v %v", st.Code(), st.Details())
	}

	// Товара нет в каталоге — доменная валидация, поле как в запросе.
	_, err = h.client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{CustomerId: 7, Items: []*ordersv1.CreateOrderItem{{ProductId: 999, Quantity: 1}}})
	st = status.Convert(err)
	if br, ok := detail[*errdetails.BadRequest](st); st.Code() != codes.InvalidArgument || !ok || br.GetFieldViolations()[0].GetField() != "items[0].product_id" {
		t.Errorf("unknown product: %v %v", st.Code(), st.Details())
	}

	// Нет товара на складе.
	h.wh.Set(&orders.OutOfStockError{ProductIDs: []int64{h.product.ID}}, nil)
	_, err = h.client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{CustomerId: 7, Items: []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: 1}}})
	st = status.Convert(err)
	info, ok := detail[*errdetails.ErrorInfo](st)
	pf, _ := detail[*errdetails.PreconditionFailure](st)
	if st.Code() != codes.FailedPrecondition || !ok || info.GetReason() != "OUT_OF_STOCK" ||
		len(pf.GetViolations()) != 1 || pf.GetViolations()[0].GetSubject() != "products/"+strconv.FormatInt(h.product.ID, 10) {
		t.Errorf("out of stock: %v %v", st.Code(), st.Details())
	}

	// Склад недоступен — UNAVAILABLE с подсказкой RetryInfo.
	h.wh.Set(orders.ErrWarehouseUnavailable, nil)
	_, err = h.client.CreateOrder(ctx, &ordersv1.CreateOrderRequest{CustomerId: 7, Items: []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: 1}}})
	st = status.Convert(err)
	if _, ok := detail[*errdetails.RetryInfo](st); st.Code() != codes.Unavailable || !ok {
		t.Errorf("warehouse down: %v %v", st.Code(), st.Details())
	}

	_, err = h.client.GetOrder(ctx, &ordersv1.GetOrderRequest{OrderId: 999})
	if status.Code(err) != codes.NotFound {
		t.Errorf("missing order: %v", err)
	}
}

func TestListOrdersPagination(t *testing.T) {
	h := newHarness(t)
	var want []int64
	for i := range 3 {
		want = append([]int64{h.create(t, 7, int32(i+1)).GetOrderId()}, want...)
	}
	h.create(t, 8, 1)

	var got []int64
	token := ""
	for range 5 {
		resp, err := h.client.ListOrders(t.Context(), &ordersv1.ListOrdersRequest{CustomerId: 7, PageSize: 2, PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range resp.GetOrders() {
			got = append(got, o.GetOrderId())
		}
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	if len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Errorf("orders = %v, want %v", got, want)
	}

	first, _ := h.client.ListOrders(t.Context(), &ordersv1.ListOrdersRequest{CustomerId: 7, PageSize: 1})
	// Курсор чужого запроса и мусор — INVALID_ARGUMENT, а не чужие данные.
	for _, tok := range []string{first.GetNextPageToken(), "garbage!"} {
		_, err := h.client.ListOrders(t.Context(), &ordersv1.ListOrdersRequest{CustomerId: 8, PageToken: tok})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("token %q: %v, want InvalidArgument", tok, err)
		}
	}
}

func doREST(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestREST(t *testing.T) {
	h := newHarness(t)
	pid := strconv.FormatInt(h.product.ID, 10)

	rec, body := doREST(t, h.rest, http.MethodPost, "/v1/orders",
		`{"customer_id": 42, "items": [{"product_id": `+pid+`, "quantity": 2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/orders = %d %s", rec.Code, rec.Body)
	}
	order := body["order"].(map[string]any)
	// int64 в protojson — строка; имена полей — как в .proto.
	id, _ := order["order_id"].(string)
	if id == "" || order["status"] != "ORDER_STATUS_RESERVED" || order["total_cents"] != "30000" {
		t.Errorf("order = %v", order)
	}

	if rec, _ := doREST(t, h.rest, http.MethodGet, "/v1/orders/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("GET = %d", rec.Code)
	}
	rec, body = doREST(t, h.rest, http.MethodGet, "/v1/orders?customer_id=42&page_size=10", "")
	if rec.Code != http.StatusOK || len(body["orders"].([]any)) != 1 {
		t.Errorf("list = %d %s", rec.Code, rec.Body)
	}
	rec, body = doREST(t, h.rest, http.MethodPost, "/v1/orders/"+id+":cancel", "")
	if rec.Code != http.StatusOK || body["order"].(map[string]any)["status"] != "ORDER_STATUS_CANCELLED" {
		t.Errorf("cancel = %d %s", rec.Code, rec.Body)
	}

	tests := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{http.MethodGet, "/v1/orders/999", "", http.StatusNotFound, "NOT_FOUND"},
		{http.MethodGet, "/v1/orders/abc", "", http.StatusBadRequest, "INVALID_ARGUMENT"},
		{http.MethodPost, "/v1/orders", `{"customer_id": 0, "items": []}`, http.StatusBadRequest, "INVALID_ARGUMENT"},
		// Опечатка в имени поля — ошибка, а не молча нулевое количество.
		{http.MethodPost, "/v1/orders", `{"customer_id": 1, "items": [{"product_id": 1, "quantiy": 1}]}`, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{http.MethodPost, "/v1/orders", `not json`, http.StatusBadRequest, "INVALID_ARGUMENT"},
	}
	for _, tt := range tests {
		rec, body := doREST(t, h.rest, tt.method, tt.path, tt.body)
		e, _ := body["error"].(map[string]any)
		if rec.Code != tt.status || e["code"] != tt.code || e["request_id"] == "" {
			t.Errorf("%s %s = %d %s, want %d %s", tt.method, tt.path, rec.Code, rec.Body, tt.status, tt.code)
		}
	}
}
