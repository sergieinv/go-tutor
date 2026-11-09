// Три вызова health-сервиса через цепочку interceptors: успешный, без
// токена и с паникой в обработчике. Каждый вызов клиента — задача
// trace.NewTask, на сервере — своя задача, а каждое звено цепочки —
// регион trace.WithRegion. Поменяйте порядок в ChainUnaryInterceptor
// (например, поставьте auth первым) и посмотрите, как изменятся полосы.
package main

import (
	"context"
	"fmt"
	"net"
	"runtime/trace"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// work — имитация работы, чтобы полосы были видны на схеме.
func work(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// serverTask — внешнее звено: открывает задачу сервера на весь вызов.
func serverTask(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	ctx, task := trace.NewTask(ctx, "server · "+info.FullMethod[len("/grpc.health.v1.Health/"):])
	defer task.End()
	return h(ctx, req)
}

func logging(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
	trace.WithRegion(ctx, "logging", func() {
		resp, err = h(ctx, req)
		trace.Log(ctx, "log", fmt.Sprintf("%s → %s", info.FullMethod, status.Code(err)))
	})
	return resp, err
}

func recovery(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
	defer trace.StartRegion(ctx, "recovery").End()
	defer func() {
		if p := recover(); p != nil {
			trace.Log(ctx, "panic", fmt.Sprint(p))
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return h(ctx, req)
}

func auth(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	r := trace.StartRegion(ctx, "auth")
	work(3) // проверка токена
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get("authorization")) == 0 {
		trace.Log(ctx, "auth", "нет токена → Unauthenticated, handler не вызван")
		r.End()
		return nil, status.Error(codes.Unauthenticated, "no token")
	}
	r.End()
	return h(ctx, req)
}

// checker — health-сервер, у которого Check("broken") паникует.
type checker struct{ *health.Server }

func (c checker) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	defer trace.StartRegion(ctx, "handler").End()
	work(4)
	if in.GetService() == "broken" {
		panic("nil map")
	}
	return c.Server.Check(ctx, in)
}

// clientRegion — клиентский interceptor: вызов по сети целиком.
func clientRegion(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	defer trace.StartRegion(ctx, "invoker: HTTP/2 → сервер → ответ").End()
	return invoker(ctx, method, req, reply, cc, opts...)
}

func main() {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(serverTask, logging, recovery, auth))
	hs := health.NewServer()
	hs.SetServingStatus("broken", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, checker{hs})
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(clientRegion))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := healthpb.NewHealthClient(conn)
	// Подключаемся заранее, чтобы первый вызов не включал установку соединения.
	conn.Connect()
	for st := conn.GetState(); st != connectivity.Ready; st = conn.GetState() {
		conn.WaitForStateChange(context.Background(), st)
	}

	withToken := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer t")
	calls := []struct {
		name, service string
		ctx           context.Context
	}{
		{"client · ok", "", withToken},
		{"client · без токена", "", context.Background()},
		{"client · паника", "broken", withToken},
	}
	for _, c := range calls {
		ctx, task := trace.NewTask(c.ctx, c.name)
		_, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: c.service})
		trace.Log(ctx, "status", status.Code(err).String())
		task.End()
		fmt.Printf("%s: %s\n", c.name, status.Code(err))
	}
}
