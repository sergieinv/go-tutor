// API заказов целиком: контракт `orders.v1` с правилами
// protovalidate и маршрутами `google.api.http`, gRPC-сервер
// с interceptors и REST через grpc-gateway — в одном
// процессе на loopback. Время зафиксировано, чтобы вывод
// был одинаковым.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Домен говорит своими ошибками, а не кодами gRPC.
var (
	errNotFound  = errors.New("order not found")
	errCancelled = errors.New("order already cancelled")
)

type server struct {
	UnimplementedOrderServiceServer
	now    func() time.Time
	mu     sync.Mutex
	orders []*Order
}

func (s *server) find(id string) (*Order, error) {
	for _, o := range s.orders {
		if o.GetId() == id {
			return o, nil
		}
	}
	return nil, errNotFound
}

// Обработчики получают уже проверенные запросы: валидация —
// в interceptor. Наружу отдают копии (`proto.Clone`), чтобы
// никто не держал ссылку на данные под мьютексом.
func (s *server) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := &Order{
		Id:         fmt.Sprintf("ord-%d", len(s.orders)+1),
		CustomerId: req.GetCustomerId(),
		Items:      proto.CloneOf(req).GetItems(),
		Status:     OrderStatus_ORDER_STATUS_CREATED,
		CreateTime: timestamppb.New(s.now()),
	}
	for _, it := range o.Items {
		o.TotalCents += int64(it.GetQuantity()) * it.GetPriceCents()
	}
	s.orders = append(s.orders, o)
	return &CreateOrderResponse{Order: proto.CloneOf(o)}, nil
}

func (s *server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.find(req.GetId())
	if err != nil {
		return nil, toStatus(err, req.GetId())
	}
	return &GetOrderResponse{Order: proto.CloneOf(o)}, nil
}

func (s *server) ListOrders(req *ListOrdersRequest, stream grpc.ServerStreamingServer[ListOrdersResponse]) error {
	s.mu.Lock()
	var list []*Order
	for _, o := range s.orders {
		if o.GetCustomerId() == req.GetCustomerId() {
			list = append(list, proto.CloneOf(o))
		}
	}
	s.mu.Unlock()
	for _, o := range list {
		if err := stream.Send(&ListOrdersResponse{Order: o}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.find(req.GetId())
	if err == nil && o.GetStatus() == OrderStatus_ORDER_STATUS_CANCELLED {
		err = errCancelled
	}
	if err != nil {
		return nil, toStatus(err, req.GetId())
	}
	o.Status = OrderStatus_ORDER_STATUS_CANCELLED
	return &CancelOrderResponse{Order: proto.CloneOf(o)}, nil
}

// Перевод доменных ошибок в статусы — в одном месте. Всё
// неизвестное — `Internal` без подробностей.
func toStatus(err error, id string) error {
	switch {
	case errors.Is(err, errNotFound):
		return status.Errorf(codes.NotFound, "order %s not found", id)
	case errors.Is(err, errCancelled):
		st, _ := status.New(codes.FailedPrecondition, "order already cancelled").WithDetails(
			&errdetails.PreconditionFailure{Violations: []*errdetails.PreconditionFailure_Violation{
				{Type: "STATUS", Subject: "orders/" + id, Description: "order is cancelled"},
			}})
		return st.Err()
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

// Валидация по правилам из контракта для всех методов:
// нарушения → `InvalidArgument` + `BadRequest` с путями полей.
func invalid(err error) error {
	var verr *protovalidate.ValidationError
	if !errors.As(err, &verr) {
		return status.Error(codes.Internal, "internal error")
	}
	br := &errdetails.BadRequest{}
	for _, v := range verr.Violations {
		br.FieldViolations = append(br.FieldViolations, &errdetails.BadRequest_FieldViolation{
			Field:       protovalidate.FieldPathString(v.Proto.GetField()),
			Description: v.Proto.GetMessage(),
		})
	}
	st, _ := status.New(codes.InvalidArgument, "invalid request").WithDetails(br)
	return st.Err()
}

func unaryValidate(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if m, ok := req.(proto.Message); ok {
			if err := v.Validate(m); err != nil {
				return nil, invalid(err)
			}
		}
		return h(ctx, req)
	}
}

// Для стримов запрос приходит через `RecvMsg` — проверяем там.
type validatingStream struct {
	grpc.ServerStream
	v protovalidate.Validator
}

func (s validatingStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if pm, ok := m.(proto.Message); ok {
		if err := s.v.Validate(pm); err != nil {
			return invalid(err)
		}
	}
	return nil
}

func streamValidate(v protovalidate.Validator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		return h(srv, validatingStream{ServerStream: ss, v: v})
	}
}

// Журнал вызовов: метод и код. REST-запросы тоже попадут
// сюда — gateway вызывает сервер по gRPC.
func unaryLog(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	resp, err := h(ctx, req)
	fmt.Printf("  [grpc] %s → %s\n", strings.TrimPrefix(info.FullMethod, "/orders.v1.OrderService/"), status.Code(err))
	return resp, err
}

func main() {
	v, err := protovalidate.New()
	if err != nil {
		log.Fatal(err)
	}
	fixed := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	gs := grpc.NewServer(
		grpc.MaxRecvMsgSize(1<<20),
		// Журнал — снаружи: видит и отказы валидации.
		grpc.ChainUnaryInterceptor(unaryLog, unaryValidate(v)),
		grpc.ChainStreamInterceptor(streamValidate(v)),
	)
	RegisterOrderServiceServer(gs, &server{now: func() time.Time { return fixed }})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go gs.Serve(lis)
	defer gs.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Gateway ходит в сервер через то же соединение, поэтому
	// interceptors работают и для REST. HTTP-сервер — с
	// таймаутами и лимитом тела.
	mux := runtime.NewServeMux()
	if err := RegisterOrderServiceHandler(ctx, mux, conn); err != nil {
		log.Fatal(err)
	}
	hs := &http.Server{
		Handler:           http.MaxBytesHandler(mux, 1<<20),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go hs.Serve(hl)
	defer hs.Close()
	base := "http://" + hl.Addr().String()

	// gRPC-клиент: создание и отказ валидации с деталями.
	client := NewOrderServiceClient(conn)
	created, err := client.CreateOrder(ctx, &CreateOrderRequest{
		CustomerId: "c-1",
		Items:      []*OrderItem{{Sku: "APPLE-1", Quantity: 3, PriceCents: 120}},
	})
	fmt.Println("gRPC create:", created.GetOrder().GetId(), created.GetOrder().GetTotalCents(), err)

	_, err = client.CreateOrder(ctx, &CreateOrderRequest{Items: []*OrderItem{{Sku: "apple", Quantity: 0}}})
	fmt.Println("gRPC create:", status.Code(err))
	for _, d := range status.Convert(err).Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, fv := range br.GetFieldViolations() {
				fmt.Printf("  %s: %s\n", fv.GetField(), fv.GetDescription())
			}
		}
	}

	// REST: тот же контракт, JSON в camelCase, коды gRPC →
	// HTTP-статусы, поток — JSON по строке на сообщение.
	rest := func(method, path, body string) {
		req, _ := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		fmt.Println("REST", method, path, "→", resp.StatusCode)
		for line := range bytes.Lines(bytes.TrimSpace(raw)) {
			var out bytes.Buffer
			json.Compact(&out, line) // protojson нарочно не стабилен в пробелах
			fmt.Println(" ", out.String())
		}
	}
	rest("POST", "/v1/orders", `{"customerId":"c-1","items":[{"sku":"PEAR-2","quantity":1,"priceCents":250}]}`)
	rest("GET", "/v1/orders/ord-404", "")
	rest("GET", "/v1/customers/c-1/orders", "")
	rest("POST", "/v1/orders/ord-1:cancel", "{}")
	rest("POST", "/v1/orders/ord-1:cancel", "{}")
}
