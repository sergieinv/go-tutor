package task

import (
	"context"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// Service — реализация OrderService из orders.proto.
type Service struct {
	UnimplementedOrderServiceServer
	now func() time.Time
	// TODO: хранилище заказов и ключей идемпотентности.
}

// NewService создаёт сервис; now — часы для create_time.
func NewService(now func() time.Time) *Service {
	return &Service{now: now}
}

// TODO: методы CreateOrder, GetOrder, ListOrders, CancelOrder.

// NewGRPCServer создаёт gRPC-сервер с сервисом svc и interceptors.
func NewGRPCServer(svc *Service) *grpc.Server {
	s := grpc.NewServer()
	RegisterOrderServiceServer(s, svc)
	return s
}

// NewGateway возвращает REST-обработчик, который вызывает сервер через conn.
func NewGateway(ctx context.Context, conn *grpc.ClientConn) (http.Handler, error) {
	mux := runtime.NewServeMux()
	if err := RegisterOrderServiceHandler(ctx, mux, conn); err != nil {
		return nil, err
	}
	return mux, nil
}
