package task

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// OrderServer — сервис заказов в памяти.
type OrderServer struct {
	// Встроена по значению: CancelOrder отдаёт Unimplemented.
	UnimplementedOrderServiceServer

	mu     sync.Mutex
	orders []*Order // в порядке создания
	byID   map[string]*Order
}

// NewOrderServer возвращает готовый к работе сервис без заказов.
func NewOrderServer() *OrderServer {
	return &OrderServer{byID: map[string]*Order{}}
}

// NewGRPCServer создаёт gRPC-сервер с лимитом входящего сообщения
// 64 КиБ и регистрирует на нём s.
func NewGRPCServer(s *OrderServer) *grpc.Server {
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 10))
	RegisterOrderServiceServer(srv, s)
	return srv
}

func (s *OrderServer) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	if req.GetCustomer() == "" {
		return nil, status.Error(codes.InvalidArgument, "customer is required")
	}
	if len(req.GetItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items are required")
	}
	var total int64
	for i, it := range req.GetItems() {
		if it.GetSku() == "" || it.GetQuantity() <= 0 || it.GetPriceCents() < 0 {
			return nil, status.Errorf(codes.InvalidArgument, "item %d is invalid", i)
		}
		total += int64(it.GetQuantity()) * it.GetPriceCents()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	o := &Order{
		Id:         fmt.Sprintf("ord-%d", len(s.orders)+1),
		Customer:   req.GetCustomer(),
		Items:      req.GetItems(),
		TotalCents: total,
	}
	s.orders = append(s.orders, o)
	s.byID[o.Id] = o
	// Наружу — копия: сохранённый заказ не должен меняться вместе
	// с ответом (в процессе, без сериализации, это одно и то же).
	return &CreateOrderResponse{Order: proto.Clone(o).(*Order)}, nil
}

func (s *OrderServer) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.byID[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "order %q not found", req.GetId())
	}
	return &GetOrderResponse{Order: proto.Clone(o).(*Order)}, nil
}

func (s *OrderServer) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &ListOrdersResponse{}
	for _, o := range s.orders {
		if req.GetCustomer() == "" || o.GetCustomer() == req.GetCustomer() {
			resp.Orders = append(resp.Orders, proto.Clone(o).(*Order))
		}
	}
	return resp, nil
}
