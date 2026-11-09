// Клиент к сервису из трёх экземпляров: одно соединение
// `grpc.ClientConn` на весь сервис, адреса — от resolver,
// балансировка `round_robin` и повторы — из service config.
// Вместо DNS — `manual` resolver: адреса трёх серверов на
// loopback мы отдаём ему сами.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
)

// Экземпляры делят счётчик попыток для SKU `flaky`: первые
// две попытки — `Unavailable`, третья — успех, на каком бы
// экземпляре она ни оказалась.
type shared struct {
	mu    sync.Mutex
	flaky int
	last  string // grpc-previous-rpc-attempts успешной попытки
	calls map[string]int
}

type stockServer struct {
	UnimplementedStockServiceServer
	name string
	s    *shared
}

func (srv *stockServer) GetStock(ctx context.Context, req *GetStockRequest) (*GetStockResponse, error) {
	srv.s.mu.Lock()
	defer srv.s.mu.Unlock()
	srv.s.calls[srv.name]++
	switch req.GetSku() {
	case "":
		return nil, status.Error(codes.InvalidArgument, "sku is required")
	case "flaky":
		srv.s.flaky++
		if srv.s.flaky < 3 {
			return nil, status.Error(codes.Unavailable, "warming up")
		}
		md, _ := metadata.FromIncomingContext(ctx)
		srv.s.last = fmt.Sprint(md.Get("grpc-previous-rpc-attempts"))
	}
	return &GetStockResponse{Sku: req.GetSku(), Quantity: 7, Backend: srv.name}, nil
}

// Service config — JSON, который обычно приходит вместе с
// адресами (из DNS TXT или xDS) или задаётся клиентом по
// умолчанию. Здесь балансировщик и политика повторов:
// не больше 3 попыток, паузы 10 мс → 20 мс с джиттером,
// повторяем только `UNAVAILABLE`.
const serviceConfig = `{
  "loadBalancingConfig": [{"round_robin": {}}],
  "methodConfig": [{
    "name": [{"service": "stock.v1.StockService"}],
    "timeout": "2s",
    "retryPolicy": {
      "maxAttempts": 3,
      "initialBackoff": "0.01s",
      "maxBackoff": "0.1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

func main() {
	s := &shared{calls: map[string]int{}}
	var addrs []resolver.Address
	for _, name := range []string{"a", "b", "c"} {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
		srv := grpc.NewServer()
		RegisterStockServiceServer(srv, &stockServer{name: name, s: s})
		go srv.Serve(lis)
		defer srv.Stop()
		addrs = append(addrs, resolver.Address{Addr: lis.Addr().String()})
	}

	// `NewClient` ничего не подключает: соединение ленивое,
	// канал в состоянии `IDLE` до первого вызова или `Connect`.
	r := manual.NewBuilderWithScheme("demo")
	r.InitialState(resolver.State{Addresses: addrs})
	conn, err := grpc.NewClient("demo:///stock",
		grpc.WithResolvers(r),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(serviceConfig))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("после NewClient:", conn.GetState())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Connect()
	for st := conn.GetState(); st != connectivity.Ready; st = conn.GetState() {
		if !conn.WaitForStateChange(ctx, st) {
			log.Fatal("канал не стал READY")
		}
	}
	fmt.Println("после Connect:", conn.GetState())

	// Канал `READY`, как только готов хотя бы один адрес.
	// Прогреваем, пока не ответят все три, — дальше
	// `round_robin` идёт по кругу строго.
	client := NewStockServiceClient(conn)
	seen := map[string]bool{}
	for len(seen) < 3 {
		resp, err := client.GetStock(ctx, &GetStockRequest{Sku: "apple"})
		if err != nil {
			log.Fatal(err)
		}
		seen[resp.GetBackend()] = true
	}
	got := map[string]int{}
	for range 6 {
		resp, err := client.GetStock(ctx, &GetStockRequest{Sku: "apple"})
		if err != nil {
			log.Fatal(err)
		}
		got[resp.GetBackend()]++
	}
	fmt.Printf("6 вызовов round_robin: a=%d b=%d c=%d\n", got["a"], got["b"], got["c"])

	// Повторы делает сам канал: приложение видит один вызов.
	// Каждая следующая попытка несёт заголовок
	// `grpc-previous-rpc-attempts`.
	resp, err := client.GetStock(ctx, &GetStockRequest{Sku: "flaky"})
	fmt.Println("flaky:", resp.GetQuantity(), err, "previous attempts =", s.last)

	// `InvalidArgument` нет в `retryableStatusCodes`: одна попытка.
	s.mu.Lock()
	before := s.calls["a"] + s.calls["b"] + s.calls["c"]
	s.mu.Unlock()
	_, err = client.GetStock(ctx, &GetStockRequest{})
	s.mu.Lock()
	after := s.calls["a"] + s.calls["b"] + s.calls["c"]
	s.mu.Unlock()
	fmt.Println("пустой sku:", status.Code(err), "попыток:", after-before)

	// Сервер недоступен. По умолчанию вызов падает сразу
	// с `Unavailable`; с `WaitForReady(true)` — ждёт
	// подключения до дедлайна.
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := dead.Addr().String()
	dead.Close()
	down, err := grpc.NewClient(deadAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer down.Close()
	short, cancelShort := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelShort()
	_, err = NewStockServiceClient(down).GetStock(short, &GetStockRequest{Sku: "apple"})
	fmt.Println("сервер лежит:", status.Code(err))
	_, err = NewStockServiceClient(down).GetStock(short, &GetStockRequest{Sku: "apple"}, grpc.WaitForReady(true))
	fmt.Println("сервер лежит, WaitForReady:", status.Code(err))

	conn.Close()
	fmt.Println("после Close:", conn.GetState())
}
