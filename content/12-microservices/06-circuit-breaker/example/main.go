// Circuit breaker как клиентский interceptor gRPC. Breaker смотрит
// на последние 10 вызовов: если их не меньше 5 и неудач хотя бы половина,
// он размыкается на 5 секунд и отказывает сразу, не трогая сеть. Потом
// пропускает одну пробу: удалась — замыкается, нет — снова размыкается.
// Часы фейковые, чтобы «5 секунд» прошли мгновенно.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type state int

const (
	closed state = iota
	open
	halfOpen
)

func (s state) String() string { return [...]string{"closed", "open", "half-open"}[s] }

var errOpen = errors.New("circuit breaker is open")

// Окно — последние `size` результатов (true — неудача) в кольце.
// `gen` — поколение: растёт при каждой смене состояния, и результат
// вызова, начатого в другом поколении, не учитывается.
type breaker struct {
	mu       sync.Mutex
	size     int
	minCalls int
	ratio    float64
	openFor  time.Duration
	now      func() time.Time
	onChange func(from, to state)

	state    state
	gen      int
	window   []bool
	openedAt time.Time
	probing  bool
}

func (b *breaker) setState(to state) {
	if b.onChange != nil {
		b.onChange(b.state, to)
	}
	b.state, b.gen, b.window, b.probing = to, b.gen+1, nil, false
	if to == open {
		b.openedAt = b.now()
	}
}

// allow решает, пускать ли вызов, и возвращает функцию для его итога.
func (b *breaker) allow() (func(failed bool), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == open && b.now().Sub(b.openedAt) >= b.openFor {
		b.setState(halfOpen)
	}
	switch {
	case b.state == open, b.state == halfOpen && b.probing:
		return nil, errOpen
	case b.state == halfOpen:
		b.probing = true
	}
	gen := b.gen
	return func(failed bool) { b.record(gen, failed) }, nil
}

func (b *breaker) record(gen int, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.gen {
		return // вызов начался до смены состояния — его итог устарел
	}
	if b.state == halfOpen {
		if failed {
			b.setState(open)
		} else {
			b.setState(closed)
		}
		return
	}
	b.window = append(b.window, failed)
	if len(b.window) > b.size {
		b.window = b.window[1:]
	}
	fails := 0
	for _, f := range b.window {
		if f {
			fails++
		}
	}
	if len(b.window) >= b.minCalls && float64(fails) >= b.ratio*float64(len(b.window)) {
		b.setState(open)
	}
}

// Что считать неудачей соседа: он недоступен, перегружен, не уложился
// в дедлайн или упал. `InvalidArgument` и `NotFound` — нормальные ответы
// здорового сервиса, на них breaker размыкаться не должен.
func isFailure(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Internal, codes.Unknown:
		return true
	}
	return false
}

// Interceptor оборачивает каждый unary-вызов клиента. Отказ breaker —
// тоже `Unavailable`, но с понятным текстом и без похода в сеть.
func (b *breaker) unary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	done, err := b.allow()
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	err = invoker(ctx, method, req, reply, cc, opts...)
	done(isFailure(err))
	return err
}

// Склад, который можно «уронить»; считает дошедшие до него запросы.
type inventory struct {
	UnimplementedInventoryServiceServer
	mu    sync.Mutex
	down  bool
	calls int
}

func (s *inventory) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	switch {
	case s.down:
		return nil, status.Error(codes.Unavailable, "склад перезапускается")
	case req.GetQuantity() <= 0:
		return nil, status.Error(codes.InvalidArgument, "quantity <= 0")
	}
	return &ReserveResponse{ReservationId: "r-" + req.GetSku()}, nil
}

func (s *inventory) set(down bool) { s.mu.Lock(); s.down = down; s.mu.Unlock() }
func (s *inventory) count() int    { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

func main() {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	inv := &inventory{}
	RegisterInventoryServiceServer(srv, inv)
	go srv.Serve(lis)
	defer srv.Stop()

	var mu sync.Mutex
	now := time.Now()
	br := &breaker{
		size: 10, minCalls: 5, ratio: 0.5, openFor: 5 * time.Second,
		now:      func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
		onChange: func(from, to state) { fmt.Printf("   breaker: %v → %v\n", from, to) },
	}
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
		fmt.Printf("   …прошло %v\n", d)
	}

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(br.unary))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewInventoryServiceClient(conn)

	n := 0
	call := func(qty int32) {
		n++
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Reserve(ctx, &ReserveRequest{Sku: "apple", Quantity: qty})
		msg := "OK"
		if err != nil {
			msg = fmt.Sprintf("%v: %s", status.Code(err), status.Convert(err).Message())
		}
		fmt.Printf("%2d. %-45s до склада дошло: %d\n", n, msg, inv.count())
	}

	fmt.Println("склад работает:")
	call(1)
	call(1)
	call(0) // InvalidArgument — не неудача соседа
	call(1)
	fmt.Println("склад упал:")
	inv.set(true)
	for range 6 {
		call(1)
	}
	advance(5 * time.Second)
	call(1) // проба: склад всё ещё лежит
	call(1)
	inv.set(false)
	advance(5 * time.Second)
	fmt.Println("склад поднялся:")
	call(1) // проба удалась
	call(1)
}
