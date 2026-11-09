package grpcx

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/logging"
)

// fakeWarehouse — обработчик для проверки interceptor'ов: запоминает, что
// увидел, и может паниковать.
type fakeWarehouse struct {
	warehousev1.UnimplementedWarehouseServiceServer

	mu          sync.Mutex
	requestID   string
	hasDeadline bool
	panic       bool
}

func (f *fakeWarehouse) GetStock(ctx context.Context, req *warehousev1.GetStockRequest) (*warehousev1.GetStockResponse, error) {
	f.mu.Lock()
	f.requestID = logging.RequestID(ctx)
	_, f.hasDeadline = ctx.Deadline()
	shouldPanic := f.panic
	f.mu.Unlock()
	if shouldPanic {
		panic("boom")
	}
	return &warehousev1.GetStockResponse{Stock: &warehousev1.Stock{ProductId: req.GetProductId(), Available: 7}}, nil
}

type harness struct {
	impl   *fakeWarehouse
	client warehousev1.WarehouseServiceClient
	reg    *prometheus.Registry
	logBuf *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	logBuf := &syncBuffer{}
	log := logging.New(logBuf, slog.LevelDebug, "test")
	reg := prometheus.NewRegistry()
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}

	impl := &fakeWarehouse{}
	srv := NewServer(ServerConfig{Logger: log, Metrics: NewServerMetrics(reg), Validator: v})
	warehousev1.RegisterWarehouseServiceServer(srv, impl)

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := NewClient("passthrough:///bufnet", ClientConfig{Metrics: NewClientMetrics(reg)},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &harness{impl: impl, client: warehousev1.NewWarehouseServiceClient(conn), reg: reg, logBuf: logBuf}
}

func TestRequestIDPropagatesAndDefaultDeadline(t *testing.T) {
	h := newHarness(t)
	ctx := logging.WithRequestID(context.Background(), "req-42")
	var header metadata.MD
	if _, err := h.client.GetStock(ctx, &warehousev1.GetStockRequest{ProductId: 1}, grpc.Header(&header)); err != nil {
		t.Fatalf("GetStock: %v", err)
	}
	h.impl.mu.Lock()
	defer h.impl.mu.Unlock()
	if h.impl.requestID != "req-42" {
		t.Errorf("server saw request id %q, want req-42", h.impl.requestID)
	}
	if got := header.Get(RequestIDKey); len(got) != 1 || got[0] != "req-42" {
		t.Errorf("response header x-request-id = %q", got)
	}
	if !h.impl.hasDeadline {
		t.Error("handler has no deadline: TimeoutUnary did not apply the default")
	}
}

func TestInvalidRequestIDIsReplaced(t *testing.T) {
	h := newHarness(t)
	ctx := metadata.AppendToOutgoingContext(context.Background(), RequestIDKey, "bad id <script>")
	if _, err := h.client.GetStock(ctx, &warehousev1.GetStockRequest{ProductId: 1}); err != nil {
		t.Fatal(err)
	}
	h.impl.mu.Lock()
	defer h.impl.mu.Unlock()
	if id := h.impl.requestID; !logging.ValidRequestID(id) || strings.Contains(id, "bad") {
		t.Errorf("request id = %q, want a fresh valid id", id)
	}
}

func TestValidationReturnsBadRequest(t *testing.T) {
	h := newHarness(t)
	_, err := h.client.GetStock(context.Background(), &warehousev1.GetStockRequest{ProductId: 0})
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", st.Code())
	}
	var br *errdetails.BadRequest
	for _, d := range st.Details() {
		if v, ok := d.(*errdetails.BadRequest); ok {
			br = v
		}
	}
	if br == nil || len(br.GetFieldViolations()) != 1 || br.GetFieldViolations()[0].GetField() != "product_id" {
		t.Fatalf("details = %v, want BadRequest for product_id", st.Details())
	}
	if br.GetFieldViolations()[0].GetReason() != "int64.gt" {
		t.Errorf("reason = %q, want int64.gt", br.GetFieldViolations()[0].GetReason())
	}
}

func TestPanicBecomesInternalWithoutDetails(t *testing.T) {
	h := newHarness(t)
	h.impl.mu.Lock()
	h.impl.panic = true
	h.impl.mu.Unlock()

	_, err := h.client.GetStock(context.Background(), &warehousev1.GetStockRequest{ProductId: 1})
	st := status.Convert(err)
	if st.Code() != codes.Internal || st.Message() != "internal error" {
		t.Fatalf("status = %v %q, want Internal \"internal error\"", st.Code(), st.Message())
	}
	logs := h.logBuf.String()
	if !strings.Contains(logs, `"msg":"panic"`) || !strings.Contains(logs, `"code":"Internal"`) {
		t.Errorf("logs lack panic record or Internal code:\n%s", logs)
	}
}

func TestMetricsCountCalls(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = h.client.GetStock(ctx, &warehousev1.GetStockRequest{ProductId: 1})
	_, _ = h.client.GetStock(ctx, &warehousev1.GetStockRequest{ProductId: 0})

	expected := `
# HELP grpc_server_handled_total Completed gRPC calls by service, method and status code.
# TYPE grpc_server_handled_total counter
grpc_server_handled_total{grpc_code="InvalidArgument",grpc_method="GetStock",grpc_service="warehouse.v1.WarehouseService"} 1
grpc_server_handled_total{grpc_code="OK",grpc_method="GetStock",grpc_service="warehouse.v1.WarehouseService"} 1
`
	if err := testutil.GatherAndCompare(h.reg, strings.NewReader(expected), "grpc_server_handled_total"); err != nil {
		t.Error(err)
	}
	if n := testutil.CollectAndCount(h.reg, "grpc_client_handled_total"); n != 2 {
		t.Errorf("client series = %d, want 2", n)
	}
}

func TestSplitMethod(t *testing.T) {
	s, m := splitMethod("/orders.v1.OrderService/CreateOrder")
	if s != "orders.v1.OrderService" || m != "CreateOrder" {
		t.Errorf("splitMethod = %q, %q", s, m)
	}
}
