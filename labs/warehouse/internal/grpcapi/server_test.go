package grpcapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/httpx"
	"gotutor/labs/platform/logging"
	"gotutor/labs/warehouse/internal/grpcapi"
	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/internal/store/memstore"
)

// harness — сервер с теми же interceptor'ами, что в cmd/warehouse, на
// bufconn (соединение в памяти: настоящий HTTP/2 и сериализация, без порта).
type harness struct {
	client warehousev1.WarehouseServiceClient
	conn   *grpc.ClientConn
}

func newHarness(t *testing.T, levels map[int64]int32) *harness {
	t.Helper()
	log := logging.New(io.Discard, slog.LevelInfo, "warehouse")
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	svc := stock.NewService(memstore.New(levels), time.Now, nil)
	srv := grpcx.NewServer(grpcx.ServerConfig{Logger: log, Validator: v})
	warehousev1.RegisterWarehouseServiceServer(srv, grpcapi.New(svc, log))

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpcx.NewClient("passthrough:///bufnet", grpcx.ClientConfig{},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &harness{client: warehousev1.NewWarehouseServiceClient(conn), conn: conn}
}

func item(productID int64, qty int32) *warehousev1.ReserveItem {
	return &warehousev1.ReserveItem{ProductId: productID, Quantity: qty}
}

// errorInfo достаёт google.rpc.ErrorInfo из деталей статуса.
func errorInfo(t *testing.T, st *status.Status) *errdetails.ErrorInfo {
	t.Helper()
	for _, d := range st.Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			return ei
		}
	}
	t.Fatalf("status %v %q has no ErrorInfo; details: %v", st.Code(), st.Message(), st.Details())
	return nil
}

func TestReserveAndRelease(t *testing.T) {
	h := newHarness(t, map[int64]int32{1: 10, 2: 5})
	ctx := t.Context()

	resp, err := h.client.Reserve(ctx, &warehousev1.ReserveRequest{OrderId: 7, Items: []*warehousev1.ReserveItem{item(2, 1), item(1, 3)}})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	want := &warehousev1.Reservation{
		OrderId: 7,
		Status:  warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED,
		Items:   []*warehousev1.ReserveItem{item(1, 3), item(2, 1)},
	}
	if !proto.Equal(resp.GetReservation(), want) {
		t.Errorf("Reserve = %v, want %v", resp.GetReservation(), want)
	}

	st, err := h.client.GetStock(ctx, &warehousev1.GetStockRequest{ProductId: 1})
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	if !proto.Equal(st.GetStock(), &warehousev1.Stock{ProductId: 1, Available: 7, Reserved: 3}) {
		t.Errorf("GetStock = %v", st.GetStock())
	}

	rel, err := h.client.Release(ctx, &warehousev1.ReleaseRequest{OrderId: 7})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	want.Status = warehousev1.ReservationStatus_RESERVATION_STATUS_RELEASED
	if !proto.Equal(rel.GetReservation(), want) {
		t.Errorf("Release = %v, want %v", rel.GetReservation(), want)
	}
}

func TestOutOfStockDetails(t *testing.T) {
	h := newHarness(t, map[int64]int32{1: 10, 2: 1})
	_, err := h.client.Reserve(t.Context(), &warehousev1.ReserveRequest{
		OrderId: 1,
		Items:   []*warehousev1.ReserveItem{item(3, 2), item(1, 1), item(2, 4)},
	})
	st := status.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", st.Code())
	}
	ei := errorInfo(t, st)
	if ei.GetDomain() != grpcapi.ErrorDomain || ei.GetReason() != "OUT_OF_STOCK" {
		t.Errorf("ErrorInfo = %v, want domain %s reason OUT_OF_STOCK", ei, grpcapi.ErrorDomain)
	}
	var pf *errdetails.PreconditionFailure
	for _, d := range st.Details() {
		if v, ok := d.(*errdetails.PreconditionFailure); ok {
			pf = v
		}
	}
	want := &errdetails.PreconditionFailure{Violations: []*errdetails.PreconditionFailure_Violation{
		{Type: "STOCK", Subject: "products/2", Description: "requested 4, available 1"},
		{Type: "STOCK", Subject: "products/3", Description: "requested 2, available 0"},
	}}
	if !proto.Equal(pf, want) {
		t.Errorf("PreconditionFailure = %v, want %v", pf, want)
	}
}

func TestReservationErrors(t *testing.T) {
	h := newHarness(t, map[int64]int32{1: 10})
	ctx := t.Context()
	if _, err := h.client.Reserve(ctx, &warehousev1.ReserveRequest{OrderId: 1, Items: []*warehousev1.ReserveItem{item(1, 1)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.client.Release(ctx, &warehousev1.ReleaseRequest{OrderId: 2}); err != nil { // надгробие
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		req        *warehousev1.ReserveRequest
		wantCode   codes.Code
		wantReason warehousev1.ErrorReason
	}{
		{
			name:       "mismatch",
			req:        &warehousev1.ReserveRequest{OrderId: 1, Items: []*warehousev1.ReserveItem{item(1, 2)}},
			wantCode:   codes.AlreadyExists,
			wantReason: warehousev1.ErrorReason_ERROR_REASON_RESERVATION_MISMATCH,
		},
		{
			name:       "released",
			req:        &warehousev1.ReserveRequest{OrderId: 2, Items: []*warehousev1.ReserveItem{item(1, 1)}},
			wantCode:   codes.Aborted,
			wantReason: warehousev1.ErrorReason_ERROR_REASON_RESERVATION_RELEASED,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.Reserve(ctx, tt.req)
			st := status.Convert(err)
			if st.Code() != tt.wantCode {
				t.Fatalf("code = %v, want %v", st.Code(), tt.wantCode)
			}
			if ei := errorInfo(t, st); ei.GetReason() != grpcapi.Reason(tt.wantReason) || ei.GetDomain() != grpcapi.ErrorDomain {
				t.Errorf("ErrorInfo = %v, want reason %s", ei, grpcapi.Reason(tt.wantReason))
			}
		})
	}
}

func TestValidationByContract(t *testing.T) {
	h := newHarness(t, nil)
	for name, req := range map[string]*warehousev1.ReserveRequest{
		"no items":          {OrderId: 1},
		"zero order":        {OrderId: 0, Items: []*warehousev1.ReserveItem{item(1, 1)}},
		"duplicate product": {OrderId: 1, Items: []*warehousev1.ReserveItem{item(1, 1), item(1, 2)}},
		"zero quantity":     {OrderId: 1, Items: []*warehousev1.ReserveItem{item(1, 0)}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.client.Reserve(t.Context(), req)
			if code := status.Code(err); code != codes.InvalidArgument {
				t.Errorf("code = %v, want InvalidArgument", code)
			}
		})
	}
}

func TestUnknownProductIsZeroStock(t *testing.T) {
	h := newHarness(t, nil)
	resp, err := h.client.GetStock(t.Context(), &warehousev1.GetStockRequest{ProductId: 404})
	if err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	if !proto.Equal(resp.GetStock(), &warehousev1.Stock{ProductId: 404}) {
		t.Errorf("GetStock = %v, want zero stock", resp.GetStock())
	}
}

func TestReason(t *testing.T) {
	if got := grpcapi.Reason(warehousev1.ErrorReason_ERROR_REASON_OUT_OF_STOCK); got != "OUT_OF_STOCK" {
		t.Errorf("Reason = %q", got)
	}
}

// TestGateway — REST GetStock через grpc-gateway, который ходит в gRPC по
// соединению (как в cmd/warehouse), с общим форматом ответа и ошибок.
func TestGateway(t *testing.T) {
	h := newHarness(t, map[int64]int32{1: 10})
	gw := httpx.NewGatewayMux(logging.New(io.Discard, slog.LevelInfo, "warehouse"))
	if err := warehousev1.RegisterWarehouseServiceHandler(t.Context(), gw, h.conn); err != nil {
		t.Fatal(err)
	}

	get := func(path string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s: body %q is not JSON", path, rec.Body.String())
		}
		return rec.Code, body
	}

	code, body := get("/v1/stock/1")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/stock/1: status %d, body %v", code, body)
	}
	// protojson: int64 — строкой (JavaScript теряет точность на 2^53),
	// имена полей — из .proto, нули не опускаются.
	stockJSON, _ := body["stock"].(map[string]any)
	if stockJSON["product_id"] != "1" || stockJSON["available"] != float64(10) || stockJSON["reserved"] != float64(0) {
		t.Errorf("GET /v1/stock/1 = %v", body)
	}

	code, body = get("/v1/stock/0")
	errJSON, _ := body["error"].(map[string]any)
	if code != http.StatusBadRequest || errJSON["code"] != "INVALID_ARGUMENT" {
		t.Errorf("GET /v1/stock/0: status %d, body %v; want 400 INVALID_ARGUMENT", code, body)
	}

	// Reserve и Release наружу не выставлены: маршрутов нет.
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/reserve", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /v1/reserve: status %d, want 404", rec.Code)
	}
}
