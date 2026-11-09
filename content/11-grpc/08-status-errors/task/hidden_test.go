package task

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestHiddenNil(t *testing.T) {
	if st := ToStatus(nil); st != nil {
		t.Fatalf("ToStatus(nil) = %v, want nil", st)
	}
	if retry, after := ShouldRetry(nil); retry || after != 0 {
		t.Fatalf("ShouldRetry(nil) = %v, %v", retry, after)
	}
	if retry, after := ShouldRetry(errors.New("plain")); retry || after != 0 {
		t.Fatalf("ShouldRetry(ошибка без статуса) = %v, %v; want false, 0", retry, after)
	}
}

func TestHiddenContextErrors(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code codes.Code
		msg  string
	}{
		{fmt.Errorf("query: %w", context.Canceled), codes.Canceled, "canceled"},
		{fmt.Errorf("query: %w", context.DeadlineExceeded), codes.DeadlineExceeded, "deadline exceeded"},
	} {
		st := mustStatus(t, tt.err)
		if st.Code() != tt.code || st.Message() != tt.msg || len(st.Details()) != 0 {
			t.Errorf("ToStatus(%v) = %v %q %v, want %v %q без деталей", tt.err, st.Code(), st.Message(), st.Details(), tt.code, tt.msg)
		}
	}
}

func TestHiddenPriority(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{errors.Join(ErrNotFound, context.Canceled), codes.Canceled},
		{errors.Join(&ValidationError{}, ErrNotFound), codes.InvalidArgument},
		{errors.Join(ErrConflict, &PreconditionError{}), codes.FailedPrecondition},
		{errors.Join(ErrConflict, ErrAlreadyExists), codes.AlreadyExists},
		{errors.Join(&UnavailableError{}, ErrConflict), codes.Aborted},
		{fmt.Errorf("wrap: %w", &UnavailableError{RetryAfter: time.Second}), codes.Unavailable},
	}
	for _, tt := range tests {
		if got := mustStatus(t, tt.err).Code(); got != tt.code {
			t.Errorf("ToStatus(%v) = %v, want %v (первое совпадение по порядку правил)", tt.err, got, tt.code)
		}
	}
}

func TestHiddenErrorInfo(t *testing.T) {
	for _, tt := range []struct {
		err    error
		reason string
	}{
		{ErrNotFound, "NOT_FOUND"},
		{ErrAlreadyExists, "ALREADY_EXISTS"},
		{ErrConflict, "CONFLICT"},
	} {
		d := mustStatus(t, tt.err).Details()
		if len(d) != 1 {
			t.Fatalf("%v: деталей %d, want 1", tt.err, len(d))
		}
		ei, ok := d[0].(*errdetails.ErrorInfo)
		if !ok || ei.GetReason() != tt.reason || ei.GetDomain() != Domain || len(ei.GetMetadata()) != 0 {
			t.Fatalf("%v: деталь %v, want ErrorInfo{%s, %s}", tt.err, d[0], tt.reason, Domain)
		}
	}
}

func TestHiddenPreconditionAndRetryInfo(t *testing.T) {
	d := mustStatus(t, &PreconditionError{Type: "STATUS", Subject: "orders/ord-7", Description: "order is cancelled"}).Details()
	if len(d) != 1 {
		t.Fatalf("деталей %d, want 1", len(d))
	}
	pf, ok := d[0].(*errdetails.PreconditionFailure)
	if !ok || len(pf.GetViolations()) != 1 {
		t.Fatalf("деталь %v, want PreconditionFailure с одним нарушением", d[0])
	}
	v := pf.GetViolations()[0]
	if v.GetType() != "STATUS" || v.GetSubject() != "orders/ord-7" || v.GetDescription() != "order is cancelled" {
		t.Fatalf("нарушение = %v", v)
	}

	d = mustStatus(t, &UnavailableError{RetryAfter: 1500 * time.Millisecond}).Details()
	if len(d) != 1 {
		t.Fatalf("Unavailable: деталей %d, want 1", len(d))
	}
	ri, ok := d[0].(*errdetails.RetryInfo)
	if !ok || ri.GetRetryDelay().AsDuration() != 1500*time.Millisecond {
		t.Fatalf("деталь %v, want RetryInfo 1.5s", d[0])
	}
	for _, after := range []time.Duration{0, -time.Second} {
		if d := mustStatus(t, &UnavailableError{RetryAfter: after}).Details(); len(d) != 0 {
			t.Fatalf("RetryAfter %v: детали %v, want нет", after, d)
		}
	}
	if d := mustStatus(t, &ValidationError{}).Details(); len(d) != 1 {
		t.Fatalf("пустой ValidationError: деталей %d, want 1 (пустой BadRequest)", len(d))
	}
}

func TestHiddenInternalDoesNotLeak(t *testing.T) {
	secret := "pq: password authentication failed for user orders at 10.0.3.7"
	for _, err := range []error{
		errors.New(secret),
		fmt.Errorf("repo: %w", errors.New(secret)),
		status.Error(codes.NotFound, secret), // статус соседа — тоже чужая ошибка
	} {
		st := mustStatus(t, err)
		if st.Code() != codes.Internal || strings.Contains(st.Message(), "10.0.3.7") || len(st.Details()) != 0 {
			t.Fatalf("ToStatus(%v) = %v %q %v, want Internal без текста ошибки и деталей", err, st.Code(), st.Message(), st.Details())
		}
	}
}

func TestHiddenShouldRetryEdgeCases(t *testing.T) {
	withDelay := func(c codes.Code, d *durationpb.Duration) error {
		st, _ := status.New(c, "x").WithDetails(&errdetails.ErrorInfo{Reason: "R"}, &errdetails.RetryInfo{RetryDelay: d})
		return st.Err()
	}
	tests := []struct {
		name  string
		err   error
		retry bool
		after time.Duration
	}{
		{"ResourceExhausted без RetryInfo", status.Error(codes.ResourceExhausted, "quota"), false, 0},
		{"ResourceExhausted с RetryInfo", withDelay(codes.ResourceExhausted, durationpb.New(2*time.Second)), true, 2 * time.Second},
		{"ResourceExhausted с отрицательной задержкой", withDelay(codes.ResourceExhausted, durationpb.New(-time.Second)), false, 0},
		{"Unavailable с отрицательной задержкой", withDelay(codes.Unavailable, durationpb.New(-time.Second)), true, 0},
		{"Unavailable с невалидной задержкой", withDelay(codes.Unavailable, &durationpb.Duration{Seconds: 1, Nanos: -5}), true, 0},
		{"Aborted с RetryInfo без задержки", withDelay(codes.Aborted, nil), true, 0},
		{"InvalidArgument с RetryInfo", withDelay(codes.InvalidArgument, durationpb.New(time.Second)), false, 0},
		{"Internal", status.Error(codes.Internal, "x"), false, 0},
		{"Canceled", status.Error(codes.Canceled, "x"), false, 0},
		{"обёрнутый статус", fmt.Errorf("call: %w", status.Error(codes.Unavailable, "x")), true, 0},
	}
	for _, tt := range tests {
		retry, after := ShouldRetry(tt.err)
		if retry != tt.retry || after != tt.after {
			t.Errorf("%s: ShouldRetry = %v, %v; want %v, %v", tt.name, retry, after, tt.retry, tt.after)
		}
	}
}

// healthErrors — сервер, который на Check(service) возвращает
// ToStatus(errs[service]): детали проходят через настоящий gRPC.
type healthErrors struct {
	healthpb.UnimplementedHealthServer
	errs map[string]error
}

func (h *healthErrors) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return nil, ToStatus(h.errs[req.GetService()]).Err()
}

func TestHiddenRoundTrip(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, &healthErrors{errs: map[string]error{
		"busy":    &UnavailableError{RetryAfter: 250 * time.Millisecond},
		"invalid": &ValidationError{Violations: []FieldViolation{{"id", "required"}}},
		"broken":  errors.New("disk /dev/sda1 failed"),
	}})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := healthpb.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Check(ctx, &healthpb.HealthCheckRequest{Service: "busy"})
	if retry, after := ShouldRetry(err); !retry || after != 250*time.Millisecond {
		t.Fatalf("busy: ShouldRetry = %v, %v; want true, 250ms", retry, after)
	}
	_, err = client.Check(ctx, &healthpb.HealthCheckRequest{Service: "invalid"})
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument || len(st.Details()) != 1 {
		t.Fatalf("invalid: %v %v", st.Code(), st.Details())
	}
	if retry, _ := ShouldRetry(err); retry {
		t.Fatal("invalid: ShouldRetry = true, want false")
	}
	_, err = client.Check(ctx, &healthpb.HealthCheckRequest{Service: "broken"})
	if st := status.Convert(err); st.Code() != codes.Internal || strings.Contains(st.Message(), "sda1") {
		t.Fatalf("broken: %v %q", st.Code(), st.Message())
	}
}
