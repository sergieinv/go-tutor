// Interceptor — это middleware gRPC: функция, которая получает
// запрос и `handler` (следующее звено цепочки) и решает, звать
// ли его. Пример собирает серверную цепочку logging → recovery →
// auth, клиентский interceptor с request id и stream-interceptor
// с обёрткой `grpc.ServerStream`. Сервер и клиент — в одном
// процессе, соединение — `bufconn` (сеть в памяти).

package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Обработчики сервиса ничего не знают про логи, токены и паники:
// это сквозная логика, она живёт в interceptors. Заказ 13 —
// «баг» в обработчике, который роняет его паникой.
type orders struct {
	UnimplementedOrderServiceServer
}

func (orders) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	fmt.Printf("        handler: GetOrder(%d), user=%s\n", req.GetId(), userFrom(ctx))
	if req.GetId() == 13 {
		var m map[string]int
		m["boom"]++ // паника: запись в nil map
	}
	return &GetOrderResponse{Id: req.GetId(), Status: "paid"}, nil
}

func (orders) ListOrders(req *ListOrdersRequest, stream grpc.ServerStreamingServer[ListOrdersResponse]) error {
	for _, id := range []int64{7, 8} {
		if err := stream.Send(&ListOrdersResponse{Id: id, Status: "new"}); err != nil {
			return err
		}
	}
	return nil
}

// Unary-interceptor сервера: `info.FullMethod` — полное имя
// метода, `handler` — остаток цепочки. Код до вызова `handler`
// работает «на входе», после — «на выходе», как у HTTP-middleware.
// Logging стоит снаружи и потому видит итоговый код ответа —
// в том числе `Internal` от recovery и `Unauthenticated` от auth.
func logging(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	rid := first(ctx, "x-request-id")
	fmt.Printf("  logging: → %s rid=%s\n", info.FullMethod, rid)
	resp, err := handler(ctx, req)
	fmt.Printf("  logging: ← %s rid=%s code=%s\n", info.FullMethod, rid, status.Code(err))
	return resp, err
}

// Паника в обработчике без recovery роняет весь процесс: в grpc-go
// нет встроенного перехвата. Recovery превращает её в `Internal`
// с общим текстом — детали только в лог, наружу ни стека, ни значения.
func recovery(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Printf("    recovery: паника в %s: %v\n", info.FullMethod, p)
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// Auth достаёт токен из metadata — аналога HTTP-заголовков —
// и сравнивает за постоянное время. Отказ — `Unauthenticated`,
// и цепочка дальше не идёт: `handler` не вызывается. Найденного
// пользователя кладём в контекст для обработчика.
type userKey struct{}

func auth(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	token, ok := strings.CutPrefix(first(ctx, "authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte("s3cret")) != 1 {
		fmt.Println("      auth: нет или неверный токен")
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	fmt.Println("      auth: ok")
	return handler(context.WithValue(ctx, userKey{}, "alice"), req)
}

// Stream-interceptor получает не запрос, а сам стрим. Чтобы
// подменить контекст или посчитать сообщения, стрим оборачивают:
// встраиваем `grpc.ServerStream` и переопределяем нужные методы.
type countingStream struct {
	grpc.ServerStream
	sent int
}

func (s *countingStream) SendMsg(m any) error {
	s.sent++
	return s.ServerStream.SendMsg(m)
}

func streamLogging(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	cs := &countingStream{ServerStream: ss}
	err := handler(srv, cs)
	fmt.Printf("  stream: %s отправлено=%d code=%s\n", info.FullMethod, cs.sent, status.Code(err))
	return err
}

// Клиентский interceptor оборачивает `invoker` — настоящий
// вызов. Здесь он добавляет request id в исходящую metadata,
// чтобы каждый вызов не делал это руками.
func withRequestID() grpc.UnaryClientInterceptor {
	n := 0
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		n++ // вызовы в примере идут по очереди, иначе нужен atomic
		ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", fmt.Sprintf("req-%d", n))
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// `ChainUnaryInterceptor` выполняет звенья в порядке перечисления:
// первое — внешнее. `UnaryInterceptor` (без Chain) принимает
// только одно звено, а второй вызов вызывает панику при старте.
func main() {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(logging, recovery, auth),
		grpc.ChainStreamInterceptor(streamLogging),
		grpc.MaxRecvMsgSize(1<<20), // лимит входящего сообщения (по умолчанию 4 МБ)
	)
	RegisterOrderServiceServer(srv, orders{})
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // только для bufconn/loopback
		grpc.WithChainUnaryInterceptor(withRequestID()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewOrderServiceClient(conn)
	authed := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer s3cret")

	// Три unary-вызова: успешный, без токена и с паникой.
	for _, c := range []struct {
		ctx context.Context
		id  int64
	}{{authed, 1}, {context.Background(), 2}, {authed, 13}} {
		fmt.Printf("GetOrder(%d)\n", c.id)
		resp, err := client.GetOrder(c.ctx, &GetOrderRequest{Id: c.id})
		if err != nil {
			st := status.Convert(err)
			fmt.Printf("клиент: ошибка %s: %s\n\n", st.Code(), st.Message())
			continue
		}
		fmt.Printf("клиент: заказ %d %s\n\n", resp.GetId(), resp.GetStatus())
	}

	// Стрим идёт через stream-цепочку, unary-interceptors его
	// не видят: auth для стримов пишут отдельно. Конец стрима —
	// `io.EOF`; он приходит после трейлеров, то есть когда
	// stream-interceptor на сервере уже отработал.
	fmt.Println("ListOrders")
	stream, err := client.ListOrders(authed, &ListOrdersRequest{Customer: "alice"})
	if err != nil {
		log.Fatal(err)
	}
	var got []string
	for {
		m, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d %s", m.GetId(), m.GetStatus()))
	}
	fmt.Println("клиент: заказы", strings.Join(got, ", "))
}

// first возвращает первое значение ключа из входящей metadata.
// Ключи metadata всегда в нижнем регистре.
func first(ctx context.Context, key string) string {
	if v := metadata.ValueFromIncomingContext(ctx, key); len(v) > 0 {
		return v[0]
	}
	return ""
}

func userFrom(ctx context.Context) string {
	u, _ := ctx.Value(userKey{}).(string)
	return u
}
