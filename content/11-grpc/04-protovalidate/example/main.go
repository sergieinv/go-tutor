// Правила валидации записаны в `orders.proto` опциями
// `buf.validate`. protovalidate читает их из дескриптора
// сообщения во время работы — генерировать валидаторы не нужно.
// Пример проверяет запросы напрямую, переводит нарушения в
// gRPC-статус с деталями и ставит проверку в interceptor
// сервера, чтобы она шла один раз для всех методов.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"sync/atomic"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func main() {
	// Валидатор создают один раз и делят между горутинами:
	// он компилирует правила при первой встрече типа и кеширует.
	v, err := protovalidate.New()
	if err != nil {
		log.Fatal(err)
	}

	good := &CreateOrderRequest{
		CustomerEmail: "ann@example.com",
		Items:         []*Item{{Sku: "MUG-1", Quantity: 2, PriceCents: 45000}},
		Delivery:      &CreateOrderRequest_PickupPointId{PickupPointId: "PP-0042"},
	}
	fmt.Println("корректный:", v.Validate(good))

	// Ошибка — `*protovalidate.ValidationError` со списком
	// нарушений. У каждого — путь к полю, id правила и текст.
	// Правило уровня сообщения (CEL) пути не имеет.
	bad := &CreateOrderRequest{
		CustomerEmail:   "not-an-email",
		Items:           []*Item{{Sku: "mug", Quantity: 0, PriceCents: 45050}},
		DiscountPercent: 30,
	}
	err = v.Validate(bad)
	var verr *protovalidate.ValidationError
	if errors.As(err, &verr) {
		for _, fv := range fieldViolations(verr) {
			fmt.Printf("  %-26s %-28s %s\n", orDash(fv.GetField()), fv.GetReason(), fv.GetDescription())
		}
	}

	// gRPC-сервер на `bufconn` — соединение в памяти вместо
	// сети. Interceptor проверяет каждый запрос до обработчика.
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(validate(v)))
	impl := &orders{}
	RegisterOrderServiceServer(srv, impl)
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewOrderServiceClient(conn)
	ctx := context.Background()

	// Клиент получает `InvalidArgument` и детали `BadRequest`:
	// по ним форма на клиенте подсветит конкретные поля.
	_, err = client.CreateOrder(ctx, bad)
	st := status.Convert(err)
	fmt.Println("CreateOrder(bad):", st.Code(), st.Message())
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			fmt.Println("  нарушений в BadRequest:", len(br.GetFieldViolations()))
		}
	}

	_, err = client.GetOrder(ctx, &GetOrderRequest{Id: -5})
	fmt.Println("GetOrder(-5):", status.Code(err))

	resp, err := client.CreateOrder(ctx, good)
	fmt.Println("CreateOrder(good):", status.Code(err), resp.GetId())
	fmt.Println("вызовов обработчиков:", impl.calls.Load())
}

// validate — unary interceptor сервера: проверка до обработчика.
func validate(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			return next(ctx, req)
		}
		err := v.Validate(msg)
		var verr *protovalidate.ValidationError
		switch {
		case err == nil:
			return next(ctx, req)
		case errors.As(err, &verr):
			st, derr := status.New(codes.InvalidArgument, "invalid request").
				WithDetails(&errdetails.BadRequest{FieldViolations: fieldViolations(verr)})
			if derr != nil {
				return nil, status.Error(codes.Internal, "internal error")
			}
			return nil, st.Err()
		default:
			// Ошибка компиляции правил или CEL — баг сервера,
			// а не клиента: подробности — в лог, наружу — Internal.
			log.Printf("validate: %v", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}
}

// fieldViolations переводит нарушения в FieldViolation в
// стабильном порядке: по пути поля, затем по id правила.
func fieldViolations(verr *protovalidate.ValidationError) []*errdetails.BadRequest_FieldViolation {
	out := make([]*errdetails.BadRequest_FieldViolation, 0, len(verr.Violations))
	for _, viol := range verr.Violations {
		out = append(out, &errdetails.BadRequest_FieldViolation{
			Field:       protovalidate.FieldPathString(viol.Proto.GetField()),
			Reason:      viol.Proto.GetRuleId(),
			Description: viol.Proto.GetMessage(),
		})
	}
	slices.SortFunc(out, func(a, b *errdetails.BadRequest_FieldViolation) int {
		return cmp.Or(cmp.Compare(a.Field, b.Field), cmp.Compare(a.Reason, b.Reason))
	})
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// orders — обработчики видят только проверенные запросы.
type orders struct {
	UnimplementedOrderServiceServer
	calls atomic.Int64
}

func (o *orders) CreateOrder(context.Context, *CreateOrderRequest) (*CreateOrderResponse, error) {
	o.calls.Add(1)
	return &CreateOrderResponse{Id: 1001}, nil
}

func (o *orders) GetOrder(_ context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	o.calls.Add(1)
	return &GetOrderResponse{Id: req.GetId()}, nil
}
