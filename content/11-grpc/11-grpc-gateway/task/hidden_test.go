package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// failing — сервер, у которого GetOrder возвращает заданную ошибку.
type failing struct {
	UnimplementedOrderServiceServer
	err error
}

func (f failing) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	if f.err == nil {
		// Без ошибки — возвращаем request id, который дошёл до gRPC.
		md, _ := metadata.FromIncomingContext(ctx)
		return nil, status.Errorf(codes.NotFound, "rid=%v", md.Get("x-request-id"))
	}
	return nil, f.err
}

func TestHiddenListFilters(t *testing.T) {
	h := gateway(t, seed())
	cases := []struct {
		path string
		want []string
	}{
		{"/v1/orders", []string{"1", "2", "3", "5"}},
		{"/v1/orders?customer_id=c1&status=ORDER_STATUS_PAID", []string{"3"}},
		{"/v1/orders?status=ORDER_STATUS_PAID", []string{"2", "3"}},
		{"/v1/customers/c2/orders", []string{"2"}},
		{"/v1/customers/c1/orders?status=ORDER_STATUS_CANCELLED", []string{"5"}},
	}
	for _, c := range cases {
		r := do(t, h, "GET", c.path, "", "")
		if r.code != 200 {
			t.Errorf("%s: статус %d: %s", c.path, r.code, r.raw)
			continue
		}
		if got := ids(t, r); !slices.Equal(got, c.want) {
			t.Errorf("%s: id = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestHiddenEmptyListAndJSONShape(t *testing.T) {
	h := gateway(t, NewServer(&Order{Id: 9, CustomerId: "c9"}))
	r := do(t, h, "GET", "/v1/customers/nobody/orders", "", "")
	if r.code != 200 || !strings.Contains(strings.ReplaceAll(r.raw, " ", ""), `"orders":[]`) {
		t.Errorf("пустой список: %d %s, want 200 и \"orders\": [] (EmitUnpopulated)", r.code, r.raw)
	}
	r = do(t, h, "GET", "/v1/orders/9", "", "")
	o, _ := r.body["order"].(map[string]any)
	want := map[string]any{"id": "9", "customer_id": "c9", "total_cents": "0", "status": "ORDER_STATUS_UNSPECIFIED"}
	for k, v := range want {
		if o[k] != v {
			t.Errorf("order.%s = %v, want %v (имена как в .proto, нулевые значения не пропускаются): %s", k, o[k], v, r.raw)
		}
	}
}

func TestHiddenCancel(t *testing.T) {
	h := gateway(t, seed())
	r := do(t, h, "POST", "/v1/orders/1:cancel", `{"reason":"changed mind"}`, "c-1")
	o, _ := r.body["order"].(map[string]any)
	if r.code != 200 || o["status"] != "ORDER_STATUS_CANCELLED" {
		t.Fatalf("отмена: %d %s", r.code, r.raw)
	}
	r = do(t, h, "POST", "/v1/orders/1:cancel", `{}`, "c-2")
	if r.code != 400 {
		t.Fatalf("повторная отмена: статус %d, want 400: %s", r.code, r.raw)
	}
	code, msg, rid := apiError(t, r)
	if code != "FAILED_PRECONDITION" || msg != "order 1 already cancelled" || rid != "c-2" {
		t.Errorf("error = {%s, %q, %q}", code, msg, rid)
	}
}

func TestHiddenCodesToHTTP(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		code     string
		message  string
		hideBody bool
	}{
		{status.Error(codes.Unauthenticated, "token expired"), 401, "UNAUTHENTICATED", "token expired", false},
		{status.Error(codes.PermissionDenied, "not your order"), 403, "PERMISSION_DENIED", "not your order", false},
		{status.Error(codes.AlreadyExists, "duplicate"), 409, "ALREADY_EXISTS", "duplicate", false},
		{status.Error(codes.Unavailable, "try later"), 503, "UNAVAILABLE", "try later", false},
		{status.Error(codes.DeadlineExceeded, "slow"), 504, "DEADLINE_EXCEEDED", "slow", false},
		{status.Error(codes.InvalidArgument, "bad id"), 400, "INVALID_ARGUMENT", "bad id", false},
		{status.Error(codes.Internal, "pq: relation \"orders\" does not exist"), 500, "INTERNAL", "internal error", true},
		{errors.New("dial tcp 10.0.0.5:5432: connection refused"), 500, "UNKNOWN", "internal error", true},
		{status.Error(codes.DataLoss, "page checksum mismatch"), 500, "DATA_LOSS", "internal error", true},
	}
	for _, c := range cases {
		r := do(t, gateway(t, failing{err: c.err}), "GET", "/v1/orders/1", "", "r-9")
		if r.code != c.status {
			t.Errorf("%v: статус %d, want %d", c.err, r.code, c.status)
		}
		code, msg, rid := apiError(t, r)
		if code != c.code || msg != c.message || rid != "r-9" {
			t.Errorf("%v: error = {%s, %q, %q}, want {%s, %q, \"r-9\"}", c.err, code, msg, rid, c.code, c.message)
		}
		if c.hideBody && (strings.Contains(r.raw, "pq:") || strings.Contains(r.raw, "10.0.0.5") || strings.Contains(r.raw, "checksum")) {
			t.Errorf("детали внутренней ошибки в ответе: %s", r.raw)
		}
		if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	}
}

func TestHiddenRequestIDReachesServer(t *testing.T) {
	h := gateway(t, failing{})
	r := do(t, h, "GET", "/v1/orders/1", "", "r-7")
	if _, msg, rid := apiError(t, r); msg != "rid=[r-7]" || rid != "r-7" {
		t.Errorf("message %q, request_id %q: X-Request-Id должен дойти до сервера как metadata x-request-id", msg, rid)
	}
	r = do(t, h, "GET", "/v1/orders/1", "", "")
	if _, msg, rid := apiError(t, r); msg != "rid=[]" || rid != "" {
		t.Errorf("без заголовка: message %q, request_id %q, want \"rid=[]\" и \"\"", msg, rid)
	}
}

func TestHiddenRoutingErrors(t *testing.T) {
	h := gateway(t, seed())
	cases := []struct {
		method, path string
		status       int
		code, msg    string
	}{
		{"GET", "/v1/nope", 404, "NOT_FOUND", "Not Found"},
		{"DELETE", "/v1/orders/1", 405, "UNIMPLEMENTED", "Method Not Allowed"},
	}
	for _, c := range cases {
		r := do(t, h, c.method, c.path, "", "x-1")
		if r.code != c.status {
			t.Errorf("%s %s: статус %d, want %d: %s", c.method, c.path, r.code, c.status, r.raw)
		}
		code, msg, rid := apiError(t, r)
		if code != c.code || msg != c.msg || rid != "x-1" {
			t.Errorf("%s %s: error = {%s, %q, %q}, want {%s, %q, \"x-1\"}", c.method, c.path, code, msg, rid, c.code, c.msg)
		}
	}
}

func TestHiddenBadInput(t *testing.T) {
	h := gateway(t, seed())
	cases := []struct{ name, method, path, body string }{
		{"id не число", "GET", "/v1/orders/abc", ""},
		{"битый JSON", "POST", "/v1/orders/1:cancel", `{"reason":`},
		{"неизвестное поле", "POST", "/v1/orders/1:cancel", `{"reson":"typo"}`},
		{"тело больше 64 КиБ", "POST", "/v1/orders/1:cancel", `{"reason":"` + strings.Repeat("x", 70<<10) + `"}`},
		{"JSON и хвост пробелов больше 64 КиБ", "POST", "/v1/orders/1:cancel", "{}" + strings.Repeat(" ", 65536)},
		{"большое тело на неизвестном пути", "POST", "/v1/nope", strings.Repeat(" ", 70000)},
	}
	for _, c := range cases {
		r := do(t, h, c.method, c.path, c.body, "b-1")
		if r.code != 400 {
			t.Errorf("%s: статус %d, want 400: %.200s", c.name, r.code, r.raw)
			continue
		}
		if code, _, rid := apiError(t, r); code != "INVALID_ARGUMENT" || rid != "b-1" {
			t.Errorf("%s: code %s, request_id %q", c.name, code, rid)
		}
	}
	// Заказ не отменён ни одним из плохих запросов.
	r := do(t, h, "GET", "/v1/orders/1", "", "")
	if o, _ := r.body["order"].(map[string]any); o["status"] != "ORDER_STATUS_NEW" {
		t.Errorf("после плохих запросов заказ изменился: %s", r.raw)
	}
}

func TestHiddenHTTPStatusError(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/orders", nil)
	req.Header.Set("X-Request-Id", "h-1")
	rec := httptest.NewRecorder()
	err := &runtime.HTTPStatusError{HTTPStatus: http.StatusTeapot, Err: status.Error(codes.Aborted, "retry")}
	ErrorHandler(context.Background(), runtime.NewServeMux(), &runtime.JSONPb{}, rec, req, err)
	r := reply{code: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	if rec.Code != http.StatusTeapot {
		t.Errorf("статус %d, want 418 из HTTPStatusError", rec.Code)
	}
	if !strings.Contains(r.raw, `"ABORTED"`) || !strings.Contains(r.raw, `"retry"`) || !strings.Contains(r.raw, `"h-1"`) {
		t.Errorf("тело %s, want code ABORTED, message retry, request_id h-1", r.raw)
	}
}
