package task

import (
	"context"
	"net/http"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Server — сервис заказов в памяти. GetOrder и CancelOrder готовы.
type Server struct {
	UnimplementedOrderServiceServer
	mu     sync.Mutex
	orders []*Order
}

// NewServer копирует заказы; id уникальны.
func NewServer(orders ...*Order) *Server {
	s := &Server{}
	for _, o := range orders {
		s.orders = append(s.orders, proto.Clone(o).(*Order))
	}
	return s
}

func (s *Server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.GetId() == req.GetId() {
			return &GetOrderResponse{Order: proto.Clone(o).(*Order)}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "order %d not found", req.GetId())
}

func (s *Server) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.GetId() != req.GetId() {
			continue
		}
		if o.GetStatus() == OrderStatus_ORDER_STATUS_CANCELLED {
			return nil, status.Errorf(codes.FailedPrecondition, "order %d already cancelled", req.GetId())
		}
		o.Status = OrderStatus_ORDER_STATUS_CANCELLED
		return &CancelOrderResponse{Order: proto.Clone(o).(*Order)}, nil
	}
	return nil, status.Errorf(codes.NotFound, "order %d not found", req.GetId())
}

// ListOrders отдаёт заказы с фильтрами по клиенту и статусу, по возрастанию id.
func (s *Server) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	// TODO
	return nil, status.Error(codes.Unimplemented, "TODO")
}

// ErrorHandler отдаёт ошибку gateway в формате API.
func ErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	// TODO
}

// NewGateway собирает REST-обработчик поверх srv в том же процессе.
func NewGateway(ctx context.Context, srv OrderServiceServer) (http.Handler, error) {
	// TODO
	return http.NotFoundHandler(), nil
}
