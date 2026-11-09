// Package grpcapi — gRPC-сервер OrderService (контракт orders.v1). REST
// получается из него же: grpc-gateway по аннотациям google.api.http
// (см. cmd/orders).
//
// Слой тонкий: перевести сообщение protobuf в доменный запрос, вызвать
// orders.Service, перевести ответ или ошибку обратно. Валидацию по
// правилам контракта делает interceptor (protovalidate) до обработчика.
package grpcapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	ordersv1 "gotutor/labs/api/gen/orders/v1"
	"gotutor/labs/orders/internal/orders"
)

// DefaultPageSize — размер страницы ListOrders, если клиент не задал.
const DefaultPageSize = 20

// Server реализует ordersv1.OrderServiceServer.
type Server struct {
	// Встраивание Unimplemented… — forward compatibility: метод, добавленный
	// в контракт позже, отвечает UNIMPLEMENTED, а не ломает сборку.
	ordersv1.UnimplementedOrderServiceServer

	svc *orders.Service
	log *slog.Logger
}

// New создаёт сервер.
func New(svc *orders.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// CreateOrder создаёт заказ.
func (s *Server) CreateOrder(ctx context.Context, req *ordersv1.CreateOrderRequest) (*ordersv1.CreateOrderResponse, error) {
	in := orders.CreateOrderRequest{
		CustomerID: req.GetCustomerId(),
		Items:      make([]orders.ItemRequest, len(req.GetItems())),
	}
	for i, it := range req.GetItems() {
		in.Items[i] = orders.ItemRequest{ProductID: it.GetProductId(), Quantity: int(it.GetQuantity())}
	}
	o, err := s.svc.CreateOrder(ctx, in)
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &ordersv1.CreateOrderResponse{
		Order: toProto(o),
		// Токен — после записи: позиция основной базы уже включает заказ.
		ConsistencyToken: string(s.svc.WritePosition(ctx, o.CustomerID)),
	}, nil
}

// GetOrder возвращает заказ.
func (s *Server) GetOrder(ctx context.Context, req *ordersv1.GetOrderRequest) (*ordersv1.GetOrderResponse, error) {
	o, err := s.svc.Order(ctx, req.GetOrderId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &ordersv1.GetOrderResponse{Order: toProto(o)}, nil
}

// ListOrders возвращает страницу заказов покупателя.
func (s *Server) ListOrders(ctx context.Context, req *ordersv1.ListOrdersRequest) (*ordersv1.ListOrdersResponse, error) {
	size := int(req.GetPageSize())
	if size == 0 {
		size = DefaultPageSize
	}
	var before int64
	if tok := req.GetPageToken(); tok != "" {
		var err error
		if before, err = decodePageToken(tok, req.GetCustomerId()); err != nil {
			return nil, s.toStatus(ctx, &orders.ValidationError{Field: "page_token", Message: "invalid page token"})
		}
	}
	list, more, err := s.svc.ListOrdersAfter(ctx, req.GetCustomerId(), before, size, orders.Position(req.GetConsistencyToken()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &ordersv1.ListOrdersResponse{Orders: make([]*ordersv1.Order, len(list))}
	for i, o := range list {
		resp.Orders[i] = toProto(o)
	}
	if more {
		resp.NextPageToken = encodePageToken(req.GetCustomerId(), list[len(list)-1].ID)
	}
	return resp, nil
}

// CancelOrder отменяет заказ.
func (s *Server) CancelOrder(ctx context.Context, req *ordersv1.CancelOrderRequest) (*ordersv1.CancelOrderResponse, error) {
	o, err := s.svc.CancelOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &ordersv1.CancelOrderResponse{
		Order:            toProto(o),
		ConsistencyToken: string(s.svc.WritePosition(ctx, o.CustomerID)),
	}, nil
}

// GetProduct возвращает товар каталога (через кеш, если он включён).
func (s *Server) GetProduct(ctx context.Context, req *ordersv1.GetProductRequest) (*ordersv1.GetProductResponse, error) {
	p, err := s.svc.Product(ctx, req.GetProductId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &ordersv1.GetProductResponse{Product: &ordersv1.Product{
		ProductId: p.ID, Name: p.Name, PriceCents: p.PriceCents,
	}}, nil
}

func toProto(o orders.Order) *ordersv1.Order {
	p := &ordersv1.Order{
		OrderId:    o.ID,
		CustomerId: o.CustomerID,
		Status:     toProtoStatus(o.Status),
		Items:      make([]*ordersv1.OrderItem, len(o.Items)),
		TotalCents: o.TotalCents,
		CreateTime: timestamppb.New(o.CreatedAt),
		UpdateTime: timestamppb.New(o.UpdatedAt),
	}
	for i, it := range o.Items {
		p.Items[i] = &ordersv1.OrderItem{ProductId: it.ProductID, Quantity: int32(it.Quantity), PriceCents: it.PriceCents}
	}
	return p
}

func toProtoStatus(s orders.Status) ordersv1.OrderStatus {
	switch s {
	case orders.StatusReserved:
		return ordersv1.OrderStatus_ORDER_STATUS_RESERVED
	case orders.StatusCancelled:
		return ordersv1.OrderStatus_ORDER_STATUS_CANCELLED
	}
	return ordersv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
}

// Курсор страницы — непрозрачная для клиента строка: «v1:покупатель:id».
// Версия в начале позволяет поменять формат, не ломая выданные курсоры;
// покупатель внутри не даёт подставить курсор от чужого запроса. Это не
// защита (подписи нет — курсор можно собрать руками), а защита от ошибок.
func encodePageToken(customerID, lastID int64) string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "v1:%d:%d", customerID, lastID))
}

func decodePageToken(tok string, customerID int64) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != strconv.FormatInt(customerID, 10) {
		return 0, fmt.Errorf("malformed page token")
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("malformed page token")
	}
	return id, nil
}
