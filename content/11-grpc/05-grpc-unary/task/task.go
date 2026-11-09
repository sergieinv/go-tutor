package task

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OrderServer — сервис заказов в памяти.
type OrderServer struct {
	UnimplementedOrderServiceServer
	// TODO: состояние
}

// NewOrderServer возвращает готовый к работе сервис без заказов.
func NewOrderServer() *OrderServer {
	return &OrderServer{}
}

// NewGRPCServer создаёт gRPC-сервер с лимитом входящего сообщения
// 64 КиБ и регистрирует на нём s.
func NewGRPCServer(s *OrderServer) *grpc.Server {
	// TODO
	return grpc.NewServer()
}

func (s *OrderServer) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "TODO")
}

func (s *OrderServer) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "TODO")
}

func (s *OrderServer) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	return nil, status.Error(codes.Unimplemented, "TODO")
}
