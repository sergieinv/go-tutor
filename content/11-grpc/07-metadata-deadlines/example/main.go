// Metadata и дедлайны на цепочке из двух сервисов:
// клиент → Checkout → Stock. Оба сервиса живут на одном
// gRPC-сервере, и Checkout зовёт Stock через тот же канал —
// как зовут соседний сервис по сети.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type checkout struct {
	UnimplementedCheckoutServiceServer
	stock StockServiceClient
}

// Входящая metadata — `metadata.FromIncomingContext`: ключи
// в нижнем регистре, значения — срезы строк. Ключ с
// суффиксом `-bin` несёт байты (в HTTP/2 — base64, но API
// отдаёт исходные байты).
func (c *checkout) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	reqID := first(md.Get("x-request-id"))
	fmt.Printf("checkout: x-request-id=%s trace-bin=%x\n", reqID, first(md.Get("trace-bin")))

	// Дедлайн клиента пришёл заголовком `grpc-timeout`, и gRPC
	// уже положил его в `ctx`. Если времени заведомо не хватит,
	// лучше отказать сразу, чем начать работу и бросить её.
	dl, ok := ctx.Deadline()
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "deadline is required")
	}
	if time.Until(dl) < 100*time.Millisecond {
		fmt.Println("checkout: меньше 100 мс — отказ без вызова Stock")
		return nil, status.Error(codes.DeadlineExceeded, "not enough time")
	}

	// Заголовки ответа уходят перед первым сообщением, трейлеры —
	// после последнего, вместе со статусом.
	grpc.SetHeader(ctx, metadata.Pairs("x-request-id", reqID))

	// Следующий вызов — с тем же `ctx`: дедлайн и отмена
	// перейдут дальше сами. Metadata — нет: входящая и исходящая
	// хранятся в контексте раздельно, пробрасываем нужное явно.
	out := metadata.AppendToOutgoingContext(ctx, "x-request-id", reqID)
	res, err := c.stock.Reserve(out, &ReserveRequest{
		OrderId: req.GetOrderId(), Sku: req.GetSku(), Quantity: req.GetQuantity(),
	})
	if err != nil {
		return nil, err // статус Stock уходит клиенту как есть
	}
	grpc.SetTrailer(ctx, metadata.Pairs("x-stock-left", "7"))
	return &CheckoutResponse{OrderId: req.GetOrderId(), ReservationId: res.GetReservationId()}, nil
}

type stock struct {
	UnimplementedStockServiceServer
	slowDone chan error // чем закончился «медленный» резерв
	entered  chan struct{}
}

func (s *stock) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	if req.GetSku() == "slow" {
		// Склад «завис». Обработчик слушает `ctx.Done()` — и узнаёт,
		// что ждать больше некому: дедлайн истёк или клиент отменил.
		s.entered <- struct{}{}
		select {
		case <-ctx.Done():
			s.slowDone <- ctx.Err()
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-time.After(10 * time.Second):
		}
	}
	fmt.Printf("stock: x-request-id=%v authorization=%v дедлайн=%v\n",
		md.Get("x-request-id"), md.Get("authorization"), hasDeadline)
	return &ReserveResponse{ReservationId: "res-" + req.GetOrderId()}, nil
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func main() {
	srv := grpc.NewServer()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	st := &stock{slowDone: make(chan error, 1), entered: make(chan struct{}, 1)}
	RegisterCheckoutServiceServer(srv, &checkout{stock: NewStockServiceClient(conn)})
	RegisterStockServiceServer(srv, st)
	go srv.Serve(lis)
	defer srv.Stop()
	client := NewCheckoutServiceClient(conn)

	// Исходящая metadata клиента. `authorization` дойдёт до
	// Checkout, но не дальше: Checkout его не пробрасывает.
	base := metadata.AppendToOutgoingContext(context.Background(),
		"x-request-id", "req-42",
		"authorization", "Bearer demo-token",
		"trace-bin", string([]byte{0xca, 0xfe}))

	ctx, cancel := context.WithTimeout(base, 2*time.Second)
	defer cancel()
	var header, trailer metadata.MD
	res, err := client.Checkout(ctx, &CheckoutRequest{OrderId: "ord-1", Sku: "tea", Quantity: 1},
		grpc.Header(&header), grpc.Trailer(&trailer))
	fmt.Println("клиент:", res.GetReservationId(), err)
	fmt.Println("клиент: заголовок", header.Get("x-request-id"), "трейлер", trailer.Get("x-stock-left"))

	// Дедлайн 50 мс: Checkout отказывает, не тратя время Stock.
	short, cancelShort := context.WithTimeout(base, 50*time.Millisecond)
	defer cancelShort()
	_, err = client.Checkout(short, &CheckoutRequest{OrderId: "ord-2", Sku: "tea"})
	fmt.Println("клиент:", status.Code(err))

	// Дедлайн истекает, пока Stock работает. Клиент получает
	// `DeadlineExceeded`, а Stock — либо `DeadlineExceeded` от
	// своего таймера, либо `Canceled`: вызывающий мог успеть
	// оборвать вызов раньше (RST_STREAM). Важно одно — работа
	// прервана по всей цепочке.
	slow, cancelSlow := context.WithTimeout(base, 300*time.Millisecond)
	defer cancelSlow()
	_, err = client.Checkout(slow, &CheckoutRequest{OrderId: "ord-3", Sku: "slow"})
	<-st.entered
	fmt.Println("клиент:", status.Code(err))
	stockErr := <-st.slowDone
	fmt.Println("stock прервал работу:",
		errors.Is(stockErr, context.DeadlineExceeded) || errors.Is(stockErr, context.Canceled))

	// Клиент сам отменил вызов — это `Canceled`, а не дедлайн.
	cctx, cancelNow := context.WithTimeout(base, 5*time.Second)
	go func() { <-st.entered; cancelNow() }()
	_, err = client.Checkout(cctx, &CheckoutRequest{OrderId: "ord-4", Sku: "slow"})
	fmt.Println("клиент:", status.Code(err))
	fmt.Println("stock увидел:", <-st.slowDone)
}
