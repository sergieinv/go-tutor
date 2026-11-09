// Бюджет времени через цепочку gateway (HTTP) → orders (gRPC) →
// warehouse (gRPC). Gateway задаёт дедлайн запроса, gRPC сам передаёт
// его дальше в заголовке `grpc-timeout`, каждый сервис видит остаток
// и отказывает сразу, если времени на работу не хватит. Отмена клиента
// доходит до последнего звена. Остаток печатается с точностью до 100 мс,
// чтобы вывод не зависел от скорости машины.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Журнал: каждый сервис пишет свои строки, а `main` печатает их
// по сервисам, когда набралось ожидаемое число — так порядок вывода
// не зависит от того, какая горутина успела первой.
type book struct {
	mu    sync.Mutex
	cond  *sync.Cond
	lines map[string][]string
	n     int
}

func (b *book) log(svc, format string, args ...any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines[svc] = append(b.lines[svc], fmt.Sprintf(format, args...))
	b.n++
	b.cond.Broadcast()
}

func (b *book) flush(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.n < n {
		b.cond.Wait()
	}
	for _, svc := range []string{"gateway", "orders", "warehouse"} {
		for _, l := range b.lines[svc] {
			fmt.Printf("  %-10s %s\n", svc+":", l)
		}
	}
	b.lines, b.n = map[string][]string{}, 0
}

// Остаток до дедлайна, округлённый вверх до 100 мс.
func left(ctx context.Context) (time.Duration, string) {
	dl, ok := ctx.Deadline()
	if !ok {
		return 0, "дедлайна нет"
	}
	d := time.Until(dl)
	return d, fmt.Sprintf("осталось ≈ %d мс", (d+100*time.Millisecond-1)/(100*time.Millisecond)*100)
}

// Склад: прежде чем работать, проверяет, хватит ли времени.
type warehouse struct {
	UnimplementedWarehouseServiceServer
	book  *book
	mu    sync.Mutex
	block bool // «долгая» работа: ждёт только отмены
}

func (w *warehouse) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	const need = 50 * time.Millisecond
	d, txt := left(ctx)
	if _, ok := ctx.Deadline(); ok && d < need {
		w.book.log("warehouse", "%s, нужно %v — отказ без работы", txt, need)
		return nil, status.Error(codes.DeadlineExceeded, "не хватит времени")
	}
	w.book.log("warehouse", "%s, нужно %v — работаем", txt, need)
	w.mu.Lock()
	block := w.block
	w.mu.Unlock()
	work := time.After(10 * time.Millisecond)
	if block {
		work = nil // канал nil: из него никогда не прочитать
	}
	select {
	case <-work:
		return &ReserveResponse{ReservationId: "r-" + req.GetSku()}, nil
	case <-ctx.Done():
		w.book.log("warehouse", "отмена дошла: %v", ctx.Err())
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

// Заказы: оставляют себе запас на работу после ответа склада и отдают
// складу остальное. Если складу достанется меньше минимума — отказ сразу.
type orders struct {
	UnimplementedOrderServiceServer
	book     *book
	stock    WarehouseServiceClient
	mu       sync.Mutex
	detached bool // ловушка: вызов склада с context.Background()
}

func (o *orders) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	const reserve, minChild = 200 * time.Millisecond, 150 * time.Millisecond
	d, txt := left(ctx)
	child := d - reserve
	if child < minChild {
		o.book.log("orders", "%s, складу осталось бы < %v — отказ без вызова", txt, minChild)
		return nil, status.Error(codes.DeadlineExceeded, "не хватит времени")
	}
	o.mu.Lock()
	detached := o.detached
	o.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, child)
	defer cancel()
	if detached {
		o.book.log("orders", "%s, но склад вызываем с context.Background()", txt)
		callCtx = context.Background()
	} else {
		o.book.log("orders", "%s, складу отдаём на %v меньше", txt, reserve)
	}
	if _, err := o.stock.Reserve(callCtx, &ReserveRequest{Sku: req.GetSku(), Quantity: req.GetQuantity()}); err != nil {
		o.book.log("orders", "склад ответил %v", status.Code(err))
		return nil, err
	}
	// Фоновая работа после ответа: WithoutCancel сохраняет значения
	// контекста, но не отменяется вместе с запросом и не несёт дедлайна.
	bg := context.WithoutCancel(ctx)
	go func() {
		<-ctx.Done() // gRPC отменяет ctx обработчика, когда ответ отправлен
		_, txt := left(bg)
		o.book.log("orders", "фон после ответа: ctx запроса — %v, у фона ошибка %v и %s", ctx.Err(), bg.Err(), txt)
	}()
	return &CreateOrderResponse{OrderId: "o-" + req.GetSku()}, nil
}

// Gateway: HTTP не передаёт дедлайн, поэтому бюджет задаёт он сам —
// подсказку клиента из заголовка, но не больше своего максимума.
type gateway struct {
	book   *book
	orders OrderServiceClient
	max    time.Duration
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	budget := g.max
	if d, err := time.ParseDuration(r.Header.Get("X-Request-Timeout")); err == nil && d > 0 {
		budget = min(d, g.max)
	}
	g.book.log("gateway", "бюджет запроса %v", budget)
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	resp, err := g.orders.CreateOrder(ctx, &CreateOrderRequest{Sku: r.URL.Query().Get("sku"), Quantity: 1})
	switch status.Code(err) {
	case codes.OK:
		g.book.log("gateway", "ответ 201")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, resp.GetOrderId())
	case codes.DeadlineExceeded:
		g.book.log("gateway", "ответ 504")
		http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
	case codes.Canceled:
		g.book.log("gateway", "клиент ушёл — отвечать некому")
	default:
		g.book.log("gateway", "ответ 502")
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}

func main() {
	b := &book{lines: map[string][]string{}}
	b.cond = sync.NewCond(&b.mu)

	wh := &warehouse{book: b}
	whConn := serve(func(s *grpc.Server) { RegisterWarehouseServiceServer(s, wh) })
	defer whConn.Close()
	ord := &orders{book: b, stock: NewWarehouseServiceClient(whConn)}
	ordConn := serve(func(s *grpc.Server) { RegisterOrderServiceServer(s, ord) })
	defer ordConn.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: &gateway{book: b, orders: NewOrderServiceClient(ordConn), max: time.Second}, ReadHeaderTimeout: time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	url := "http://" + ln.Addr().String() + "/orders?sku=apple"

	post := func(ctx context.Context, timeout string) string {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if timeout != "" {
			req.Header.Set("X-Request-Timeout", timeout)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "ошибка клиента: " + context.Cause(ctx).Error()
		}
		resp.Body.Close()
		return resp.Status
	}

	fmt.Println("1. обычный запрос (клиент просит 5s):")
	res := post(context.Background(), "5s")
	b.flush(5)
	fmt.Println("  клиент:   ", res)

	fmt.Println("2. мало времени (клиент просит 300ms):")
	res = post(context.Background(), "300ms")
	b.flush(3)
	fmt.Println("  клиент:   ", res)

	fmt.Println("3. клиент отменил запрос, пока склад работает:")
	wh.mu.Lock()
	wh.block = true
	wh.mu.Unlock()
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() {
		waitFor(b, "warehouse")
		cancel(fmt.Errorf("пользователь закрыл вкладку"))
	}()
	res = post(ctx, "")
	b.flush(6)
	fmt.Println("  клиент:   ", res)

	fmt.Println("4. ловушка: context.Background() в середине цепочки:")
	wh.mu.Lock()
	wh.block = false
	wh.mu.Unlock()
	ord.mu.Lock()
	ord.detached = true
	ord.mu.Unlock()
	res = post(context.Background(), "")
	b.flush(5)
	fmt.Println("  клиент:   ", res)
}

// waitFor ждёт первую строку журнала от сервиса svc.
func waitFor(b *book, svc string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.lines[svc]) == 0 {
		b.cond.Wait()
	}
}

// serve поднимает gRPC-сервер на bufconn и возвращает клиентское соединение.
func serve(register func(*grpc.Server)) *grpc.ClientConn {
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	register(s)
	go s.Serve(lis)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	return conn
}
