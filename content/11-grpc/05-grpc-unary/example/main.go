// Сервер и клиент gRPC в одном процессе. Контракт —
// `orders.proto` рядом: buf сгенерировал из него сообщения
// (`orders.pb.go`) и gRPC-обвязку (`orders_grpc.pb.go`) —
// интерфейс `OrderServiceServer`, функцию регистрации и
// клиента `OrderServiceClient`.

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Реализация сервиса — обычная структура. Встроенная по
// значению `UnimplementedOrderServiceServer` даёт заглушки
// для всех методов контракта: `CancelOrder` мы не пишем, и
// код всё равно компилируется, а клиент получит
// `Unimplemented`. Новый метод в контракте не ломает сборку
// старого сервера — это и есть forward-compat.
type orderServer struct {
	UnimplementedOrderServiceServer

	mu     sync.Mutex
	orders map[string]*Order
	next   int
	// Только для демонстрации остановки: если канал задан,
	// GetOrder сообщает о входе и ждёт его закрытия.
	entered chan struct{}
	release chan struct{}
}

// Обработчики вызываются конкурентно — по горутине на
// вызов, поэтому общее состояние под мьютексом. Ошибка —
// всегда `status.Error` с кодом: его увидит клиент.
func (s *orderServer) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	if req.GetCustomer() == "" || len(req.GetItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "customer and items are required")
	}
	o := &Order{Customer: req.GetCustomer(), Items: req.GetItems()}
	for _, it := range req.GetItems() {
		o.TotalCents += int64(it.GetQuantity()) * it.GetPriceCents()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	o.Id = fmt.Sprintf("ord-%d", s.next)
	s.orders[o.Id] = o
	return &CreateOrderResponse{Order: o}, nil
}

func (s *orderServer) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	entered, release := s.entered, s.release
	s.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "order %q not found", req.GetId())
	}
	return &GetOrderResponse{Order: o}, nil
}

func main() {
	// Сервер: опции — при создании. Лимит входящего сообщения
	// по умолчанию 4 МБ; ставим 1 МБ — чем меньше лимит, тем
	// дешевле злонамеренный огромный запрос.
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(1 << 20))
	impl := &orderServer{orders: map[string]*Order{}}
	RegisterOrderServiceServer(srv, impl)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()

	// Клиент: `grpc.NewClient` ничего не подключает — канал
	// ленивый, соединение откроется при первом вызове.
	// `insecure` — только для loopback; TLS — в уроке 12.
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewOrderServiceClient(conn)

	// Каждый вызов — с дедлайном: без него зависший сервер
	// держит клиента вечно.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	created, err := client.CreateOrder(ctx, &CreateOrderRequest{
		Customer: "alice",
		Items:    []*Item{{Sku: "tea", Quantity: 2, PriceCents: 350}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("создан:", created.GetOrder().GetId(), created.GetOrder().GetTotalCents())

	got, err := client.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	fmt.Println("найден:", got.GetOrder().GetCustomer(), err)

	// Ошибка вызова — статус: код и сообщение сервера.
	_, err = client.GetOrder(ctx, &GetOrderRequest{Id: "ord-9"})
	fmt.Println("нет такого:", status.Code(err), status.Convert(err).Message())

	_, err = client.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
	fmt.Println("CancelOrder:", status.Code(err))

	// Запрос больше лимита сервер отвергает, не вызывая
	// обработчик: `ResourceExhausted`.
	_, err = client.CreateOrder(ctx, &CreateOrderRequest{
		Customer: strings.Repeat("x", 2<<20),
		Items:    []*Item{{Sku: "tea", Quantity: 1}},
	})
	fmt.Println("2 МБ:", status.Code(err))

	// `GracefulStop` закрывает слушатель, перестаёт принимать
	// новые вызовы и ждёт активные. Запускаем «долгий» вызов
	// и останавливаем сервер, пока он внутри обработчика.
	impl.mu.Lock()
	impl.entered, impl.release = make(chan struct{}), make(chan struct{})
	impl.mu.Unlock()
	slow := make(chan error, 1)
	go func() {
		_, err := client.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
		slow <- err
	}()
	<-impl.entered

	stopped := make(chan struct{})
	go func() { srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
		fmt.Println("GracefulStop не дождался вызова")
	case <-time.After(100 * time.Millisecond):
		fmt.Println("GracefulStop ждёт активный вызов")
	}
	close(impl.release)
	fmt.Println("долгий вызов:", status.Code(<-slow))
	<-stopped
	fmt.Println("сервер остановлен, Serve вернул:", <-served)
}
