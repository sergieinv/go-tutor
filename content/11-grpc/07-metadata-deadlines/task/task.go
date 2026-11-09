package task

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config — настройки Checkout.
type Config struct {
	MinBudget      time.Duration    // меньше этого остатка — отказ без вызова Stock
	DefaultTimeout time.Duration    // если у входящего вызова нет дедлайна
	Now            func() time.Time // часы
	NewID          func() string    // id запроса, если клиент его не прислал
}

// CheckoutServer принимает заказ и резервирует товар в Stock.
type CheckoutServer struct {
	UnimplementedCheckoutServiceServer
	// TODO
}

// NewCheckoutServer возвращает сервис, который зовёт stock.
func NewCheckoutServer(stock StockServiceClient, cfg Config) *CheckoutServer {
	return &CheckoutServer{}
}

func (s *CheckoutServer) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error) {
	return nil, status.Error(codes.Unimplemented, "TODO")
}
