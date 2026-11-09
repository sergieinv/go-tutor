// Package grpcapi — gRPC-сервер WarehouseService поверх домена stock.
//
// Обработчики тонкие: перевести сообщение в доменные типы, вызвать
// stock.Service, перевести ответ обратно. Валидацию запросов по правилам
// контракта (protovalidate) уже сделал interceptor из platform/grpcx, а
// доменные ошибки переводятся в статусы в одном месте — errors.go.
package grpcapi

import (
	"context"
	"log/slog"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/warehouse/internal/stock"
)

// Server реализует warehousev1.WarehouseServiceServer.
type Server struct {
	// Встраивание Unimplemented… — требование protoc-gen-go-grpc: новый
	// метод в контракте не ломает сборку, а отвечает UNIMPLEMENTED.
	warehousev1.UnimplementedWarehouseServiceServer

	svc *stock.Service
	log *slog.Logger
}

// New создаёт сервер.
func New(svc *stock.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// Reserve резервирует позиции заказа.
func (s *Server) Reserve(ctx context.Context, req *warehousev1.ReserveRequest) (*warehousev1.ReserveResponse, error) {
	items := make([]stock.Item, len(req.GetItems()))
	for i, it := range req.GetItems() {
		items[i] = stock.Item{ProductID: it.GetProductId(), Quantity: it.GetQuantity()}
	}
	r, err := s.svc.Reserve(ctx, req.GetOrderId(), items)
	if err != nil {
		return nil, s.toStatus(ctx, "reserve", err)
	}
	return &warehousev1.ReserveResponse{Reservation: toProto(r)}, nil
}

// Release отменяет резерв заказа.
func (s *Server) Release(ctx context.Context, req *warehousev1.ReleaseRequest) (*warehousev1.ReleaseResponse, error) {
	r, err := s.svc.Release(ctx, req.GetOrderId())
	if err != nil {
		return nil, s.toStatus(ctx, "release", err)
	}
	return &warehousev1.ReleaseResponse{Reservation: toProto(r)}, nil
}

// GetStock возвращает остаток товара.
func (s *Server) GetStock(ctx context.Context, req *warehousev1.GetStockRequest) (*warehousev1.GetStockResponse, error) {
	l, err := s.svc.Level(ctx, req.GetProductId())
	if err != nil {
		return nil, s.toStatus(ctx, "get stock", err)
	}
	return &warehousev1.GetStockResponse{Stock: &warehousev1.Stock{
		ProductId: l.ProductID,
		Available: l.Available,
		Reserved:  l.Reserved,
	}}, nil
}

// StatusToProto переводит доменный статус резерва в enum контракта.
func StatusToProto(s stock.Status) warehousev1.ReservationStatus {
	switch s {
	case stock.StatusReserved:
		return warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED
	case stock.StatusReleased:
		return warehousev1.ReservationStatus_RESERVATION_STATUS_RELEASED
	}
	return warehousev1.ReservationStatus_RESERVATION_STATUS_UNSPECIFIED
}

func toProto(r stock.Reservation) *warehousev1.Reservation {
	items := make([]*warehousev1.ReserveItem, len(r.Items))
	for i, it := range r.Items {
		items[i] = &warehousev1.ReserveItem{ProductId: it.ProductID, Quantity: it.Quantity}
	}
	return &warehousev1.Reservation{OrderId: r.OrderID, Status: StatusToProto(r.Status), Items: items}
}
