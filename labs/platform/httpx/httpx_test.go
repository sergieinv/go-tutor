package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/health"
	"gotutor/labs/platform/logging"
)

type stockServer struct {
	warehousev1.UnimplementedWarehouseServiceServer
	err error
}

func (s *stockServer) GetStock(_ context.Context, req *warehousev1.GetStockRequest) (*warehousev1.GetStockResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &warehousev1.GetStockResponse{Stock: &warehousev1.Stock{ProductId: req.GetProductId(), Available: 3}}, nil
}

// newGateway собирает ту же цепочку, что и сервисы: RequestID → Instrument
// → Recover → gateway. Регистрация в процессе (HandlerServer) — только для
// теста: в сервисах gateway ходит в свой gRPC-сервер по сети, чтобы
// работали interceptor'ы.
func newGateway(t *testing.T, srv warehousev1.WarehouseServiceServer) (http.Handler, *prometheus.Registry, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	log := logging.New(&logs, slog.LevelInfo, "test")
	mux := NewGatewayMux(log)
	if err := warehousev1.RegisterWarehouseServiceHandlerServer(context.Background(), mux, srv); err != nil {
		t.Fatal(err)
	}
	reg := prometheus.NewRegistry()
	h := RequestID(Instrument(log, NewMetrics(reg))(Recover(log)(mux)))
	return h, reg, &logs
}

func decodeError(t *testing.T, body []byte) ErrorBody {
	t.Helper()
	var resp struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, body)
	}
	return resp.Error
}

func TestGatewaySuccessUsesProtoNames(t *testing.T) {
	h, reg, _ := newGateway(t, &stockServer{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stock/5", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	// Имена из .proto и нулевые значения (reserved: 0) в ответе.
	if body := rec.Body.String(); !strings.Contains(body, `"product_id":"5"`) || !strings.Contains(body, `"reserved":0`) {
		t.Errorf("body = %s", body)
	}
	for k := range rec.Header() {
		if strings.HasPrefix(k, "Grpc-") {
			t.Errorf("internal gRPC metadata leaked as header %s", k)
		}
	}
	if !logging.ValidRequestID(rec.Header().Get("X-Request-ID")) {
		t.Errorf("X-Request-ID = %q", rec.Header().Get("X-Request-ID"))
	}
	expected := `
# HELP http_server_requests_total HTTP requests by method, route template and status code.
# TYPE http_server_requests_total counter
http_server_requests_total{code="200",method="GET",route="/v1/stock/{product_id=*}"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "http_server_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestGatewayErrorFormat(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{"not found", status.Error(codes.NotFound, "order 7 not found"), http.StatusNotFound, "NOT_FOUND", "order 7 not found"},
		// Внутренние адреса из сообщений UNAVAILABLE/INTERNAL наружу не уходят.
		{"unavailable", status.Error(codes.Unavailable, "dial tcp 10.0.0.5:9090: connection refused"), http.StatusServiceUnavailable, "UNAVAILABLE", "service unavailable, retry later"},
		{"internal", errors.New("pq: relation \"orders\" does not exist"), http.StatusInternalServerError, "UNKNOWN", "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, logs := newGateway(t, &stockServer{err: tt.err})
			req := httptest.NewRequest(http.MethodGet, "/v1/stock/5", nil)
			req.Header.Set("X-Request-ID", "req-1")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			got := decodeError(t, rec.Body.Bytes())
			if got.Code != tt.wantCode || got.Message != tt.wantMessage || got.RequestID != "req-1" {
				t.Errorf("error = %+v", got)
			}
			if tt.wantStatus >= 500 && !strings.Contains(logs.String(), "gateway error") {
				t.Errorf("5xx not logged: %s", logs)
			}
		})
	}
}

// TestGatewayRetryAfter: RetryInfo в деталях → заголовок Retry-After
// (секунды, вверх); RESOURCE_EXHAUSTED → 429.
func TestGatewayRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		code       codes.Code
		delay      time.Duration
		wantStatus int
		want       string
	}{
		{codes.ResourceExhausted, 1500 * time.Millisecond, http.StatusTooManyRequests, "2"},
		{codes.Unavailable, 100 * time.Millisecond, http.StatusServiceUnavailable, "1"},
	} {
		st, err := status.New(tc.code, "busy").WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(tc.delay)})
		if err != nil {
			t.Fatal(err)
		}
		h, _, _ := newGateway(t, &stockServer{err: st.Err()})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stock/5", nil))
		if rec.Code != tc.wantStatus || rec.Header().Get("Retry-After") != tc.want {
			t.Errorf("%v: status %d Retry-After %q, want %d %q", tc.code, rec.Code, rec.Header().Get("Retry-After"), tc.wantStatus, tc.want)
		}
	}
	// Без RetryInfo заголовка нет.
	h, _, _ := newGateway(t, &stockServer{err: status.Error(codes.NotFound, "no")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stock/5", nil))
	if v := rec.Header().Get("Retry-After"); v != "" {
		t.Errorf("Retry-After without RetryInfo = %q", v)
	}
}

func TestGatewayValidationOfJSONAndRouting(t *testing.T) {
	h, reg, _ := newGateway(t, &stockServer{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stock/abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id: status = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/nothing", nil))
	if rec.Code != http.StatusNotFound || decodeError(t, rec.Body.Bytes()).Code != "NOT_FOUND" {
		t.Errorf("unknown route: status = %d, body %s", rec.Code, rec.Body)
	}
	expected := `
# HELP http_server_requests_total HTTP requests by method, route template and status code.
# TYPE http_server_requests_total counter
http_server_requests_total{code="400",method="GET",route="/v1/stock/{product_id=*}"} 1
http_server_requests_total{code="404",method="GET",route="unmatched"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "http_server_requests_total"); err != nil {
		t.Error(err)
	}
}

func TestRecoverHidesPanic(t *testing.T) {
	var logs bytes.Buffer
	log := logging.New(&logs, slog.LevelInfo, "test")
	h := RequestID(Instrument(log, nil)(Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret internal state")
	}))))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("panic value leaked to client: %s", rec.Body)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != "INTERNAL" || got.RequestID == "" {
		t.Errorf("error = %+v", got)
	}
	if !strings.Contains(logs.String(), "secret internal state") {
		t.Error("panic not logged")
	}
}

func TestTimeoutSetsDeadline(t *testing.T) {
	var deadline time.Time
	h := Timeout(time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, _ = r.Context().Deadline()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if deadline.IsZero() {
		t.Error("no deadline in handler context")
	}
}

func TestOpsHandler(t *testing.T) {
	var logs bytes.Buffer
	log := logging.New(&logs, slog.LevelInfo, "test")
	reg := prometheus.NewRegistry()
	ready := &health.Readiness{}
	dbDown := errors.New("dial tcp 10.0.0.7:5432: connection refused")
	var dbErr error
	ready.Add("db", func(context.Context) error { return dbErr })
	h := OpsHandler(reg, ready, log)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/healthz"); rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d", rec.Code)
	}
	if rec := get("/readyz"); rec.Code != http.StatusOK {
		t.Errorf("/readyz = %d", rec.Code)
	}
	dbErr = dbDown
	rec := get("/readyz")
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("/readyz with db down = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "10.0.0.7") {
		t.Error("readiness failure reason not logged")
	}
	dbErr = nil
	ready.SetDraining()
	if rec := get("/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz while draining = %d", rec.Code)
	}
	if rec := get("/metrics"); rec.Code != http.StatusOK {
		t.Errorf("/metrics = %d", rec.Code)
	}
}
