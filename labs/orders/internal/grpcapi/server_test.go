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
	"time"

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
	"gotutor/labs/orders/internal/shed"
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
	return newHarnessWith(t, nil)
}

// newHarnessWith — то же с обёрткой склада и дополнительными опциями
// gRPC-сервера (interceptor'ы модуля 14).
func newHarnessWith(t *testing.T, wrapWH func(orders.Warehouse) orders.Warehouse, extra ...grpc.ServerOption) *harness {
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
	var whIface orders.Warehouse = wh
	if wrapWH != nil {
		whIface = wrapWH(wh)
	}
	svc := orders.NewService(store, whIface, orders.Options{})

	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	srv := grpcx.NewServer(grpcx.ServerConfig{Logger: log, Validator: v}, extra...)
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

func TestGetProduct(t *testing.T) {
	h := newHarness(t)
	resp, err := h.client.GetProduct(t.Context(), &ordersv1.GetProductRequest{ProductId: h.product.ID})
	if err != nil || resp.GetProduct().GetName() != "Ручка" || resp.GetProduct().GetPriceCents() != 15000 {
		t.Fatalf("GetProduct = %v, %v", resp, err)
	}
	_, err = h.client.GetProduct(t.Context(), &ordersv1.GetProductRequest{ProductId: 999})
	if st := status.Convert(err); st.Code() != codes.NotFound || st.Message() != "product not found" {
		t.Errorf("missing product: %v", err)
	}

	rec := httptest.NewRecorder()
	h.rest.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/products/"+strconv.FormatInt(h.product.ID, 10), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"price_cents":"15000"`) {
		t.Errorf("REST GET product = %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.rest.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/products/999", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("REST missing product = %d", rec.Code)
	}
}

// TestConsistencyTokenRoundTrip: без реплик токен пустой, а ListOrders
// принимает поле (REST — параметр запроса) и отдаёт историю.
func TestConsistencyTokenRoundTrip(t *testing.T) {
	h := newHarness(t)
	resp, err := h.client.CreateOrder(t.Context(), &ordersv1.CreateOrderRequest{
		CustomerId: 7, Items: []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok := resp.GetConsistencyToken(); tok != "" {
		t.Errorf("token without replicas = %q, want empty", tok)
	}
	rec := httptest.NewRecorder()
	h.rest.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/orders?customer_id=7&consistency_token=lsn:0/1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"customer_id":"7"`) {
		t.Errorf("REST list with token = %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.rest.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/orders?customer_id=7&consistency_token="+strings.Repeat("x", 65), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("REST list with oversized token = %d, want 400", rec.Code)
	}
}

// blockingWarehouse держит Reserve, пока тест не закроет gate.
type blockingWarehouse struct {
	orders.Warehouse
	entered chan struct{}
	gate    chan struct{}
}

func (w *blockingWarehouse) Reserve(ctx context.Context, id int64, items []orders.ItemRequest) error {
	w.entered <- struct{}{}
	<-w.gate
	return w.Warehouse.Reserve(ctx, id, items)
}

// TestOverloadRejectsFast: место занято, очереди нет — CreateOrder сразу
// получает RESOURCE_EXHAUSTED с RetryInfo, REST — 429 с Retry-After.
// Чтение при этом работает.
func TestOverloadRejectsFast(t *testing.T) {
	bw := &blockingWarehouse{entered: make(chan struct{}, 1), gate: make(chan struct{})}
	limiter := shed.New(shed.Config{MaxInFlight: 1, RetryAfter: 2 * time.Second})
	h := newHarnessWith(t, func(w orders.Warehouse) orders.Warehouse { bw.Warehouse = w; return bw },
		grpc.ChainUnaryInterceptor(limiter.UnaryInterceptor(ordersv1.OrderService_CreateOrder_FullMethodName)))

	first := make(chan error, 1)
	go func() {
		_, err := h.client.CreateOrder(context.Background(), &ordersv1.CreateOrderRequest{
			CustomerId: 7, Items: []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: 1}},
		})
		first <- err
	}()
	<-bw.entered // первый заказ занял единственное место

	_, err := h.client.CreateOrder(t.Context(), &ordersv1.CreateOrderRequest{
		CustomerId: 8, Items: []*ordersv1.CreateOrderItem{{ProductId: h.product.ID, Quantity: 1}},
	})
	st := status.Convert(err)
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("second CreateOrder: %v, want ResourceExhausted", err)
	}
	if ri, ok := detail[*errdetails.RetryInfo](st); !ok || ri.GetRetryDelay().AsDuration() != 2*time.Second {
		t.Errorf("RetryInfo = %v", ri)
	}

	rec := httptest.NewRecorder()
	h.rest.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/orders",
		strings.NewReader(`{"customer_id": 9, "items": [{"product_id": `+strconv.FormatInt(h.product.ID, 10)+`, "quantity": 1}]}`)))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
		t.Errorf("REST = %d Retry-After %q %s, want 429 and 2", rec.Code, rec.Header().Get("Retry-After"), rec.Body)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "RESOURCE_EXHAUSTED" {
		t.Errorf("REST body = %s", rec.Body)
	}

	// Чтение не ограничено.
	if _, err := h.client.ListOrders(t.Context(), &ordersv1.ListOrdersRequest{CustomerId: 7}); err != nil {
		t.Errorf("ListOrders during overload: %v", err)
	}
	close(bw.gate)
	if err := <-first; err != nil {
		t.Errorf("first CreateOrder: %v", err)
	}
}
