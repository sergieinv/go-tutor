package task

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

// scripted — health-сервер, который отвечает ошибками из списка по очереди,
// а когда список кончился — успехом. Записывает metadata каждой попытки.
type scripted struct {
	healthpb.UnimplementedHealthServer
	mu       sync.Mutex
	errs     []error
	attempts [][]string // значения x-attempt каждой попытки
	reqIDs   [][]string // значения x-request-id каждой попытки
}

func (s *scripted) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, md.Get("x-attempt"))
	s.reqIDs = append(s.reqIDs, md.Get("x-request-id"))
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return nil, err
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attempts)
}

// fakeTime — управляемые часы и sleep: Sleep не ждёт, а сдвигает часы
// и записывает паузу.
type fakeTime struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	err    error // что вернуть из Sleep
}

func newFakeTime() *fakeTime { return &fakeTime{now: time.Now()} }

func (f *fakeTime) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeTime) Sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	if f.err != nil {
		return f.err
	}
	f.now = f.now.Add(d)
	return nil
}

func (f *fakeTime) slept() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sleeps)
}

// start поднимает scripted-сервер на bufconn и клиента с UnaryRetry.
// Встроенные повторы grpc-go выключены, чтобы считать только свои.
func start(t *testing.T, cfg Config, errs ...error) (healthpb.HealthClient, *scripted) {
	t.Helper()
	srv := &scripted{errs: errs}
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	healthpb.RegisterHealthServer(s, srv)
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(),
		grpc.WithUnaryInterceptor(UnaryRetry(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return healthpb.NewHealthClient(conn), srv
}

func config(ft *fakeTime, attempts int) Config {
	return Config{
		MaxAttempts:    attempts,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     time.Second,
		Sleep:          ft.Sleep,
		Now:            ft.Now,
	}
}

func unavailable() error { return status.Error(codes.Unavailable, "try later") }

func withRetryInfo(c codes.Code, d time.Duration) error {
	st, err := status.New(c, "slow down").WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(d)})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func ctx5s(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRetriesUnavailableWithBackoff(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 4), unavailable(), unavailable())
	if _, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check: %v, want успех на третьей попытке", err)
	}
	if n := srv.count(); n != 3 {
		t.Fatalf("попыток = %d, want 3", n)
	}
	if got, want := ft.slept(), []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}; !slices.Equal(got, want) {
		t.Fatalf("паузы = %v, want %v", got, want)
	}
}

func TestRetryInfoDelay(t *testing.T) {
	ft := newFakeTime()
	client, _ := start(t, config(ft, 3), withRetryInfo(codes.Unavailable, 1500*time.Millisecond))
	if _, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Пауза из RetryInfo важнее своего backoff и не режется MaxBackoff.
	if got, want := ft.slept(), []time.Duration{1500 * time.Millisecond}; !slices.Equal(got, want) {
		t.Fatalf("паузы = %v, want %v", got, want)
	}
}

func TestNotRetried(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"InvalidArgument", status.Error(codes.InvalidArgument, "bad sku")},
		{"NotFound", status.Error(codes.NotFound, "no such order")},
		{"ResourceExhausted без RetryInfo", status.Error(codes.ResourceExhausted, "message too large")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := newFakeTime()
			client, srv := start(t, config(ft, 3), tt.err)
			_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
			if status.Code(err) != status.Code(tt.err) {
				t.Fatalf("код = %v, want %v", status.Code(err), status.Code(tt.err))
			}
			if n := srv.count(); n != 1 {
				t.Fatalf("попыток = %d, want 1", n)
			}
		})
	}
}

func TestAttemptMetadata(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3), unavailable(), unavailable())
	ctx := metadata.AppendToOutgoingContext(ctx5s(t), "x-request-id", "req-42")
	if _, err := client.Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	want := [][]string{{"1"}, {"2"}, {"3"}}
	if !slices.EqualFunc(srv.attempts, want, slices.Equal) {
		t.Fatalf("x-attempt по попыткам = %v, want %v", srv.attempts, want)
	}
	for i, ids := range srv.reqIDs {
		if !slices.Equal(ids, []string{"req-42"}) {
			t.Fatalf("попытка %d: x-request-id = %v, want [req-42]", i+1, ids)
		}
	}
}
