// Логи сервиса — это JSON-строки, которые собирает Loki,
// Elasticsearch или CloudWatch и по которым ищут: «все
// записи запроса req-1 во всех сервисах». Для этого каждая
// запись несёт request_id, а сервисы передают его друг другу
// в gRPC-metadata. Пример: «Заказы» зовут «Склад», оба пишут
// в один поток, и записи одного запроса связаны.

package main

import (
	"context"
	"crypto/rand"
	"log"
	"log/slog"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Атрибуты запроса живут в контексте: их кладёт тот, кто
// принял запрос, а логгер достаёт сам. Функциям между ними
// не нужно передавать request_id параметром.
type attrsKey struct{}

func withAttrs(ctx context.Context, as ...slog.Attr) context.Context {
	old, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return context.WithValue(ctx, attrsKey{}, append(old[:len(old):len(old)], as...))
}

// ctxHandler — обёртка над любым `slog.Handler`: дописывает
// атрибуты из контекста к записи. Методы `*Context`
// (`InfoContext`, `Log`) передают контекст в `Handle`.
type ctxHandler struct{ slog.Handler }

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if as, ok := ctx.Value(attrsKey{}).([]slog.Attr); ok {
		r = r.Clone()
		r.AddAttrs(as...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h ctxHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return ctxHandler{h.Handler.WithAttrs(as)}
}

func (h ctxHandler) WithGroup(name string) slog.Handler {
	return ctxHandler{h.Handler.WithGroup(name)}
}

// Interceptor сервера: request_id из metadata (или новый,
// если клиент не прислал или прислал мусор), одна запись на
// вызов. Уровень — по коду: ошибка клиента — WARN, сбой
// сервера — ERROR, health-проверки — DEBUG, чтобы не
// заливать логи пробами k8s.
func serverLog(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		id := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok && len(md.Get("x-request-id")) > 0 {
			id = md.Get("x-request-id")[0]
		}
		if id == "" || len(id) > 64 {
			id = rand.Text()
		}
		ctx = withAttrs(ctx, slog.String("request_id", id))
		resp, err := h(ctx, req)
		code := status.Code(err)
		level := slog.LevelInfo
		switch {
		case info.FullMethod == healthpb.Health_Check_FullMethodName:
			level = slog.LevelDebug
		case code == codes.InvalidArgument || code == codes.FailedPrecondition || code == codes.NotFound:
			level = slog.LevelWarn
		case code != codes.OK:
			level = slog.LevelError
		}
		logger.Log(ctx, level, "grpc call", "method", info.FullMethod, "code", code.String())
		return resp, err
	}
}

// Interceptor клиента переносит request_id из контекста в
// исходящую metadata — так он доходит до соседа.
func propagateID(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	as, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	for _, a := range as {
		if a.Key == "request_id" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", a.Value.String())
		}
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

type warehouse struct {
	UnimplementedWarehouseServiceServer
	log *slog.Logger
}

func (w *warehouse) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	for _, l := range req.GetLines() {
		if l.GetQty() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "qty must be positive")
		}
	}
	// Группа складывает атрибуты под общий ключ; `LogValuer`
	// у `reserveLog` решает, что из запроса попадёт в лог.
	w.log.DebugContext(ctx, "reserve", "req", reserveLog{req})
	return &ReserveResponse{}, nil
}

// reserveLog логирует запрос выборочно: id и число позиций,
// а не всё сообщение. Так в лог не попадёт поле, которое
// завтра добавят в контракт, — например, адрес покупателя.
type reserveLog struct{ r *ReserveRequest }

func (l reserveLog) LogValue() slog.Value {
	return slog.GroupValue(slog.String("order_id", l.r.GetOrderId()), slog.Int("lines", len(l.r.GetLines())))
}

func main() {
	// Уровень можно менять на лету — `LevelVar` безопасен
	// для конкурентного доступа. `ReplaceAttr` убирает время,
	// чтобы вывод примера был одинаковым при каждом запуске.
	var level slog.LevelVar
	jh := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: &level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	base := slog.New(ctxHandler{jh})

	// «Склад»: gRPC-сервер с interceptor логирования и health.
	gs := grpc.NewServer(grpc.ChainUnaryInterceptor(serverLog(base.With("service", "warehouse"))))
	RegisterWarehouseServiceServer(gs, &warehouse{log: base.With("service", "warehouse")})
	hs := health.NewServer()
	healthpb.RegisterHealthServer(gs, hs)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go gs.Serve(ln)
	defer gs.GracefulStop()

	// «Заказы»: клиент с interceptor, переносящим request_id.
	conn, err := grpc.NewClient(ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(propagateID))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	stock := NewWarehouseServiceClient(conn)
	logger := base.With("service", "orders")

	// Два входящих запроса. Middleware «Заказов» положило бы
	// request_id в контекст — здесь делаем это сами.
	for i, qty := range []int32{2, 0} {
		ctx := withAttrs(context.Background(), slog.String("request_id", []string{"req-1", "req-2"}[i]))
		logger.InfoContext(ctx, "create order", slog.Group("order", "customer", "alice", "qty", qty))
		_, err := stock.Reserve(ctx, &ReserveRequest{OrderId: "o-1", Lines: []*Line{{Sku: "tea", Qty: qty}}})
		if err != nil {
			logger.WarnContext(ctx, "reserve failed", "err", err)
		}
	}

	// Проба health на уровне INFO не видна. Включили DEBUG —
	// видны и проба, и отладочная запись из `Reserve`.
	hc := healthpb.NewHealthClient(conn)
	hc.Check(context.Background(), &healthpb.HealthCheckRequest{})
	level.Set(slog.LevelDebug)
	hc.Check(withAttrs(context.Background(), slog.String("request_id", "probe-1")), &healthpb.HealthCheckRequest{})
	stock.Reserve(withAttrs(context.Background(), slog.String("request_id", "req-3")),
		&ReserveRequest{OrderId: "o-3", Lines: []*Line{{Sku: "tea", Qty: 1}, {Sku: "cup", Qty: 1}}})
}
