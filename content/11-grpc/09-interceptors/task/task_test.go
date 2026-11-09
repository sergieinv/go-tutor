package task

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// env — сервер с цепочкой Chain и тестовым методом /test.Echo/Say
// (StringValue → StringValue) плюс health-сервис, клиент по bufconn.
type env struct {
	conn  *grpc.ClientConn
	mu    sync.Mutex
	lines []string
	ids   int
}

func (e *env) log(line string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lines = append(e.lines, line)
}

func (e *env) newID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ids++
	return fmt.Sprintf("gen-%d", e.ids)
}

func (e *env) logs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.lines)
}

const sayMethod = "/test.Echo/Say"

// safetyNet — внешнее звено тестов: если паника прошла через всю цепочку,
// тест получит DataLoss, а не уронит весь пакет.
func safetyNet(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = status.Errorf(codes.DataLoss, "паника не перехвачена цепочкой: %v", p)
		}
	}()
	return handler(ctx, req)
}

// newEnv поднимает сервер; h — обработчик метода Say.
func newEnv(t *testing.T, tokens map[string]string, h func(ctx context.Context, in string) (string, error)) *env {
	t.Helper()
	e := &env{}
	lis := bufconn.Listen(1 << 20)
	chain := append([]grpc.UnaryServerInterceptor{safetyNet}, Chain(Config{Log: e.log, NewID: e.newID, Tokens: tokens})...)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(chain...))
	desc := grpc.ServiceDesc{
		ServiceName: "test.Echo",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Say",
			Handler: func(srv any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
				in := new(wrapperspb.StringValue)
				if err := dec(in); err != nil {
					return nil, err
				}
				call := func(ctx context.Context, req any) (any, error) {
					out, err := h(ctx, req.(*wrapperspb.StringValue).GetValue())
					if err != nil {
						return nil, err
					}
					return wrapperspb.String(out), nil
				}
				if ic == nil {
					return call(ctx, in)
				}
				return ic(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: sayMethod}, call)
			},
		}},
	}
	srv.RegisterService(&desc, struct{}{})
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	e.conn = conn
	return e
}

// say вызывает Say с парами metadata и возвращает ответ, заголовки ответа и ошибку.
func (e *env) say(t *testing.T, in string, kv ...string) (string, metadata.MD, error) {
	t.Helper()
	ctx := metadata.AppendToOutgoingContext(context.Background(), kv...)
	out := new(wrapperspb.StringValue)
	var header metadata.MD
	err := e.conn.Invoke(ctx, sayMethod, wrapperspb.String(in), out, grpc.Header(&header))
	return out.GetValue(), header, err
}

var tokens = map[string]string{"alice": "a-token", "bob": "b-token"}

// whoami отвечает «пользователь/request id» из контекста.
func whoami(ctx context.Context, in string) (string, error) {
	return UserFrom(ctx) + "/" + RequestIDFrom(ctx), nil
}

func TestAuthorizedCall(t *testing.T) {
	e := newEnv(t, tokens, whoami)
	got, _, err := e.say(t, "hi", "authorization", "Bearer b-token", "x-request-id", "r-1")
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if got != "bob/r-1" {
		t.Errorf("обработчик увидел %q, want %q", got, "bob/r-1")
	}
	want := []string{"/test.Echo/Say OK rid=r-1"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}

func TestMissingToken(t *testing.T) {
	var called atomic.Bool
	e := newEnv(t, tokens, func(ctx context.Context, in string) (string, error) {
		called.Store(true)
		return "", nil
	})
	_, _, err := e.say(t, "hi", "x-request-id", "r-2")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("код %v, want Unauthenticated", status.Code(err))
	}
	if called.Load() {
		t.Error("обработчик вызван без токена")
	}
	want := []string{"/test.Echo/Say Unauthenticated rid=r-2"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}

func TestPanicBecomesInternal(t *testing.T) {
	e := newEnv(t, tokens, func(ctx context.Context, in string) (string, error) {
		panic("boom")
	})
	_, _, err := e.say(t, "hi", "authorization", "Bearer a-token", "x-request-id", "r-3")
	st := status.Convert(err)
	if st.Code() != codes.Internal || st.Message() != "internal error" {
		t.Fatalf("ошибка %v, want Internal «internal error»", err)
	}
	want := []string{"panic /test.Echo/Say rid=r-3: boom", "/test.Echo/Say Internal rid=r-3"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}
