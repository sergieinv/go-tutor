package task

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var fixedNow = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

type api struct {
	client OrderServiceClient
	base   string // адрес REST
}

// startAPI поднимает gRPC-сервер ученика на bufconn и gateway поверх
// настоящего gRPC-соединения на httptest-сервере.
func startAPI(t *testing.T) *api {
	t.Helper()
	gs := NewGRPCServer(NewService(func() time.Time { return fixedNow }))
	lis := bufconn.Listen(1 << 20)
	go gs.Serve(lis)
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	gw, err := NewGateway(ctx, conn)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	hs := httptest.NewServer(gw)
	t.Cleanup(hs.Close)
	return &api{client: NewOrderServiceClient(conn), base: hs.URL}
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func withKey(ctx context.Context, key string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "idempotency-key", key)
}

func item(sku string, qty int32, price int64) *OrderItem {
	return &OrderItem{Sku: sku, Quantity: qty, PriceCents: price}
}

// rest выполняет HTTP-запрос и возвращает код ответа и тело.
func (a *api) rest(t *testing.T, method, path, body string, headers ...string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(testCtx(t), method, a.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func decode(t *testing.T, raw []byte, m proto.Message) {
	t.Helper()
	if err := protojson.Unmarshal(raw, m); err != nil {
		t.Fatalf("тело ответа %s: %v", raw, err)
	}
}

// violations — поля из BadRequest в деталях статуса, по возрастанию.
func violations(err error) []string {
	var fields []string
	for _, d := range status.Convert(err).Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, fv := range br.GetFieldViolations() {
				fields = append(fields, fv.GetField())
			}
		}
	}
	slices.Sort(fields)
	return fields
}

func TestCreateAndGet(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	resp, err := a.client.CreateOrder(ctx, &CreateOrderRequest{
		CustomerId: "c-1",
		Items:      []*OrderItem{item("APPLE-1", 3, 120), item("PEAR-2", 1, 250)},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	o := resp.GetOrder()
	if o.GetId() != "ord-1" || o.GetCustomerId() != "c-1" || o.GetTotalCents() != 610 ||
		o.GetStatus() != OrderStatus_ORDER_STATUS_CREATED || !o.GetCreateTime().AsTime().Equal(fixedNow) || len(o.GetItems()) != 2 {
		t.Fatalf("CreateOrder = %v", o)
	}
	got, err := a.client.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	if err != nil || !proto.Equal(got.GetOrder(), o) {
		t.Fatalf("GetOrder(ord-1) = %v, %v; want %v", got.GetOrder(), err, o)
	}
	_, err = a.client.GetOrder(ctx, &GetOrderRequest{Id: "ord-9"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetOrder(ord-9): %v, want NotFound", err)
	}
}

func TestCreateValidation(t *testing.T) {
	a := startAPI(t)
	_, err := a.client.CreateOrder(testCtx(t), &CreateOrderRequest{
		Items: []*OrderItem{item("apple", 0, 10)},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
	}
	want := []string{"customer_id", "items[0].quantity", "items[0].sku"}
	if got := violations(err); !slices.Equal(got, want) {
		t.Fatalf("поля BadRequest = %v, want %v", got, want)
	}
}

func TestIdempotentCreate(t *testing.T) {
	a := startAPI(t)
	ctx := withKey(testCtx(t), "key-1")
	req := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 100)}}
	first, err := a.client.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	again, err := a.client.CreateOrder(ctx, req)
	if err != nil || again.GetOrder().GetId() != first.GetOrder().GetId() {
		t.Fatalf("повтор с тем же ключом = %v, %v; want тот же заказ %s", again.GetOrder().GetId(), err, first.GetOrder().GetId())
	}
	other := &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 2, 100)}}
	if _, err := a.client.CreateOrder(ctx, other); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("тот же ключ, другой запрос: %v, want AlreadyExists", err)
	}
}

func TestListPages(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	for range 3 {
		if _, err := a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	p1, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: 2})
	if err != nil || len(p1.GetOrders()) != 2 || p1.GetNextPageToken() == "" {
		t.Fatalf("первая страница = %v, %v; want 2 заказа и токен", p1, err)
	}
	p2, err := a.client.ListOrders(ctx, &ListOrdersRequest{CustomerId: "c-1", PageSize: 2, PageToken: p1.GetNextPageToken()})
	if err != nil || len(p2.GetOrders()) != 1 || p2.GetOrders()[0].GetId() != "ord-3" || p2.GetNextPageToken() != "" {
		t.Fatalf("вторая страница = %v, %v; want [ord-3] без токена", p2, err)
	}
}

func TestCancel(t *testing.T) {
	a := startAPI(t)
	ctx := testCtx(t)
	if _, err := a.client.CreateOrder(ctx, &CreateOrderRequest{CustomerId: "c-1", Items: []*OrderItem{item("APPLE-1", 1, 1)}}); err != nil {
		t.Fatal(err)
	}
	resp, err := a.client.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
	if err != nil || resp.GetOrder().GetStatus() != OrderStatus_ORDER_STATUS_CANCELLED {
		t.Fatalf("CancelOrder = %v, %v", resp.GetOrder(), err)
	}
	if _, err := a.client.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("повторная отмена: %v, want FailedPrecondition", err)
	}
}

func TestRESTCreateAndGet(t *testing.T) {
	a := startAPI(t)
	code, raw := a.rest(t, "POST", "/v1/orders", `{"customerId":"c-1","items":[{"sku":"APPLE-1","quantity":2,"priceCents":"150"}]}`)
	if code != http.StatusOK {
		t.Fatalf("POST /v1/orders → %d %s, want 200", code, raw)
	}
	var created CreateOrderResponse
	decode(t, raw, &created)
	if created.GetOrder().GetId() != "ord-1" || created.GetOrder().GetTotalCents() != 300 {
		t.Fatalf("POST /v1/orders = %v", created.GetOrder())
	}
	code, raw = a.rest(t, "GET", "/v1/orders/ord-1", "")
	var got GetOrderResponse
	if decode(t, raw, &got); code != http.StatusOK || got.GetOrder().GetId() != "ord-1" {
		t.Fatalf("GET /v1/orders/ord-1 → %d %s", code, raw)
	}
	if code, raw = a.rest(t, "GET", "/v1/orders/ord-7", ""); code != http.StatusNotFound {
		t.Fatalf("GET /v1/orders/ord-7 → %d %s, want 404", code, raw)
	}
}

func TestRESTIdempotencyKeyHeader(t *testing.T) {
	a := startAPI(t)
	body := `{"customerId":"c-1","items":[{"sku":"APPLE-1","quantity":1,"priceCents":"100"}]}`
	var ids []string
	for range 2 {
		code, raw := a.rest(t, "POST", "/v1/orders", body, "Idempotency-Key", "rest-key-1")
		if code != http.StatusOK {
			t.Fatalf("POST → %d %s", code, raw)
		}
		var r CreateOrderResponse
		decode(t, raw, &r)
		ids = append(ids, r.GetOrder().GetId())
	}
	if ids[0] != ids[1] {
		t.Fatalf("два POST с одним Idempotency-Key создали %v — заголовок не дошёл до сервера", ids)
	}
}
