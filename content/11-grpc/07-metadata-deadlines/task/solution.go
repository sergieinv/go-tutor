package task

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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
	stock StockServiceClient
	cfg   Config
}

// NewCheckoutServer возвращает сервис, который зовёт stock.
func NewCheckoutServer(stock StockServiceClient, cfg Config) *CheckoutServer {
	return &CheckoutServer{stock: stock, cfg: cfg}
}

func (s *CheckoutServer) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error) {
	var reqID string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-request-id"); len(v) > 0 {
			reqID = v[0]
		}
	}
	if reqID == "" {
		reqID = s.cfg.NewID()
	}
	// Заголовок — в любом исходе. Ошибка SetHeader означает, что
	// вызов идёт не через gRPC-сервер, — ответить всё равно можно.
	_ = grpc.SetHeader(ctx, metadata.Pairs("x-request-id", reqID))

	if req.GetOrderId() == "" || req.GetSku() == "" || req.GetQuantity() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "order_id, sku and positive quantity are required")
	}

	if dl, ok := ctx.Deadline(); ok {
		if dl.Sub(s.cfg.Now()) < s.cfg.MinBudget {
			return nil, status.Error(codes.DeadlineExceeded, "not enough time to reserve")
		}
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.DefaultTimeout)
		defer cancel()
	}

	// Новая исходящая metadata только с id запроса: входящую
	// (authorization и прочее) дальше не отдаём.
	out := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", reqID))
	res, err := s.stock.Reserve(out, &ReserveRequest{
		OrderId:  req.GetOrderId(),
		Sku:      req.GetSku(),
		Quantity: req.GetQuantity(),
	})
	if err != nil {
		return nil, err
	}
	return &CheckoutResponse{OrderId: req.GetOrderId(), ReservationId: res.GetReservationId()}, nil
}
