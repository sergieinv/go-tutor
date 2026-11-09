// Ошибки gRPC — это статус: код, сообщение и детали
// (`google.rpc.Status`). Сервис внутри работает с доменными
// ошибками, а в статусы их переводит одна функция на краю.
// Клиент разбирает статус и решает, повторять ли вызов.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Доменные ошибки ничего не знают о gRPC.
var (
	ErrNotFound    = errors.New("order not found")
	ErrConflict    = errors.New("order was modified concurrently")
	ErrUnavailable = errors.New("storage is overloaded")
)

type FieldError struct{ Field, Reason string }

type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string { return "validation failed" }

type NotPayableError struct{ OrderID, Status string }

func (e *NotPayableError) Error() string { return "order is " + e.Status }

const domain = "orders.example.com"

// Единственное место, где домен встречается с gRPC. Код
// выбирается по смыслу: InvalidArgument — запрос плох сам по
// себе, FailedPrecondition — запрос нормален, но состояние
// системы не то, Aborted — конфликт, повторите всю операцию.
// Неизвестное — Internal без подробностей: их видит только лог.
func toStatus(err error) *status.Status {
	var ve *ValidationError
	var np *NotPayableError
	switch {
	case errors.As(err, &ve):
		br := &errdetails.BadRequest{}
		for _, f := range ve.Fields {
			br.FieldViolations = append(br.FieldViolations,
				&errdetails.BadRequest_FieldViolation{Field: f.Field, Description: f.Reason})
		}
		return withDetails(codes.InvalidArgument, "invalid request", br)
	case errors.As(err, &np):
		return withDetails(codes.FailedPrecondition, "order cannot be paid",
			&errdetails.PreconditionFailure{Violations: []*errdetails.PreconditionFailure_Violation{
				{Type: "STATUS", Subject: "orders/" + np.OrderID, Description: "order is " + np.Status},
			}})
	case errors.Is(err, ErrNotFound):
		return withDetails(codes.NotFound, "order not found",
			&errdetails.ErrorInfo{Reason: "ORDER_NOT_FOUND", Domain: domain})
	case errors.Is(err, ErrConflict):
		return withDetails(codes.Aborted, "concurrent modification",
			&errdetails.ErrorInfo{Reason: "CONFLICT", Domain: domain})
	case errors.Is(err, ErrUnavailable):
		return withDetails(codes.Unavailable, "temporarily unavailable",
			&errdetails.RetryInfo{RetryDelay: durationpb.New(200 * time.Millisecond)})
	default:
		fmt.Println("  лог сервера:", err) // подробности — только сюда
		return status.New(codes.Internal, "internal error")
	}
}

// `WithDetails` упаковывает сообщения в `Any` и возвращает
// новый статус; ошибка возможна только при сбое сериализации
// (и при коде OK) — тогда отдаём статус без деталей.
func withDetails(c codes.Code, msg string, details ...protoadapt.MessageV1) *status.Status {
	st := status.New(c, msg)
	if withD, err := st.WithDetails(details...); err == nil {
		return withD
	}
	return st
}

type server struct {
	UnimplementedOrderServiceServer
}

func (server) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	var ve ValidationError
	if req.GetCustomer() == "" {
		ve.Fields = append(ve.Fields, FieldError{"customer", "must not be empty"})
	}
	if req.GetQuantity() <= 0 {
		ve.Fields = append(ve.Fields, FieldError{"quantity", "must be positive"})
	}
	if len(ve.Fields) > 0 {
		return nil, &ve
	}
	return &CreateOrderResponse{Order: &Order{Id: "ord-1", Customer: req.GetCustomer()}}, nil
}

func (server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	switch req.GetId() {
	case "ord-busy":
		return nil, fmt.Errorf("get %s: %w", req.GetId(), ErrUnavailable)
	case "ord-db":
		return nil, errors.New("pq: password authentication failed for user \"orders\" at 10.0.3.7")
	}
	return nil, fmt.Errorf("get %s: %w", req.GetId(), ErrNotFound)
}

func (server) PayOrder(ctx context.Context, req *PayOrderRequest) (*PayOrderResponse, error) {
	if req.GetId() == "ord-race" {
		return nil, ErrConflict
	}
	return nil, &NotPayableError{OrderID: req.GetId(), Status: "cancelled"}
}

// Перевод — в одном interceptor (подробно — урок 09): каждый
// обработчик возвращает доменную ошибку, наружу уходит статус.
func mapErrors(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	resp, err := h(ctx, req)
	if err != nil {
		return nil, toStatus(err).Err()
	}
	return resp, nil
}

// Клиент: `status.Convert` достаёт статус из ошибки вызова,
// `Details()` возвращает распакованные сообщения деталей.
func describe(err error) {
	st := status.Convert(err)
	fmt.Printf("%v (HTTP %d): %s\n", st.Code(), runtime.HTTPStatusFromCode(st.Code()), st.Message())
	for _, d := range st.Details() {
		switch d := d.(type) {
		case *errdetails.BadRequest:
			for _, v := range d.GetFieldViolations() {
				fmt.Printf("  поле %s: %s\n", v.GetField(), v.GetDescription())
			}
		case *errdetails.ErrorInfo:
			fmt.Printf("  причина %s@%s\n", d.GetReason(), d.GetDomain())
		case *errdetails.PreconditionFailure:
			for _, v := range d.GetViolations() {
				fmt.Printf("  условие %s %s: %s\n", v.GetType(), v.GetSubject(), v.GetDescription())
			}
		case *errdetails.RetryInfo:
			fmt.Printf("  повторить через %v\n", d.GetRetryDelay().AsDuration())
		}
	}
	retry, after := shouldRetry(st)
	fmt.Println("  повторять:", retry, after)
}

// Повторяют то, что может пройти со второй попытки без
// изменений в запросе. Aborted — повторить всю операцию
// (прочитать заново и снова записать), а не тот же запрос.
func shouldRetry(st *status.Status) (bool, time.Duration) {
	switch st.Code() {
	case codes.Unavailable, codes.Aborted:
		for _, d := range st.Details() {
			if ri, ok := d.(*errdetails.RetryInfo); ok {
				return true, ri.GetRetryDelay().AsDuration()
			}
		}
		return true, 0
	}
	return false, 0
}

func main() {
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(mapErrors))
	RegisterOrderServiceServer(srv, server{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(lis)
	defer srv.Stop()
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewOrderServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.CreateOrder(ctx, &CreateOrderRequest{})
	describe(err)
	_, err = client.GetOrder(ctx, &GetOrderRequest{Id: "ord-404"})
	describe(err)
	_, err = client.PayOrder(ctx, &PayOrderRequest{Id: "ord-7"})
	describe(err)
	_, err = client.PayOrder(ctx, &PayOrderRequest{Id: "ord-race"})
	describe(err)
	_, err = client.GetOrder(ctx, &GetOrderRequest{Id: "ord-busy"})
	describe(err)
	// Пароль и адрес базы остались в логе сервера.
	_, err = client.GetOrder(ctx, &GetOrderRequest{Id: "ord-db"})
	describe(err)
}
