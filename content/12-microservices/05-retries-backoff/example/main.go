// Повторы вызова gRPC: что повторять, сколько ждать между попытками
// и когда остановиться. Сервер склада отвечает по «расписанию»: сбои
// сети, перегрузка с подсказкой RetryInfo, неверный запрос. Клиент
// повторяет по кодам, ждёт с экспоненциальной паузой и не начинает
// паузу, которая не уложится в дедлайн. Время в примере — фейковые
// часы: паузы не ждут по-настоящему, поэтому вывод стабилен.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Сервер отдаёт ошибки из `script` по одной на вызов, а когда они
// кончились — успех. `calls` считает, сколько запросов до него дошло.
type inventory struct {
	UnimplementedInventoryServiceServer
	mu     sync.Mutex
	script []error
	calls  int
}

func (s *inventory) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.script) > 0 {
		err := s.script[0]
		s.script = s.script[1:]
		if err != nil {
			return nil, err
		}
	}
	return &ReserveResponse{ReservationId: "r-" + req.GetSku()}, nil
}

// Перегруженный сервер говорит не только «нет», но и «когда»:
// деталь `RetryInfo` едет к клиенту в трейлерах вместе со статусом.
func overloaded(after time.Duration) error {
	st, _ := status.New(codes.ResourceExhausted, "очередь заполнена").
		WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(after)})
	return st.Err()
}

// Повторять стоит только временные отказы: `Unavailable` (сосед
// недоступен, соединение порвалось), `ResourceExhausted` (перегрузка,
// лимит) и `Aborted` (конфликт — повторить всю операцию).
// `InvalidArgument`, `NotFound`, `PermissionDenied` повтор не исправит.
func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return true
	}
	return false
}

// Подсказка сервера важнее своей экспоненты: он знает, когда освободится.
func serverDelay(err error) (time.Duration, bool) {
	for _, d := range status.Convert(err).Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok && ri.GetRetryDelay().IsValid() {
			return ri.GetRetryDelay().AsDuration(), true
		}
	}
	return 0, false
}

// Фейковые часы: `sleep` не ждёт, а сдвигает «сейчас». Начинаются
// с настоящего времени, чтобы дедлайн контекста был в будущем.
type clock struct{ t, start time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) sleep(ctx context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return ctx.Err()
}

// Политика: число попыток, экспонента с потолком и полный джиттер —
// пауза случайна в [0, предел). `rand` в примере всегда 0.5, чтобы
// вывод не плавал; в жизни — `rand.Float64` из `math/rand/v2`.
type policy struct {
	attempts  int
	base, max time.Duration
	rand      func() float64
	clk       *clock
}

func (p policy) do(ctx context.Context, call func(context.Context) error) error {
	for attempt := 1; ; attempt++ {
		err := call(ctx)
		at := p.clk.now().Sub(p.clk.start)
		if err == nil {
			fmt.Printf("  +%v попытка %d: успех\n", at, attempt)
			return nil
		}
		fmt.Printf("  +%v попытка %d: %v", at, attempt, status.Code(err))
		if !retryable(err) {
			fmt.Println(" — не повторяем")
			return err
		}
		if attempt == p.attempts {
			fmt.Println(" — попытки кончились")
			return err
		}
		d, fromServer := serverDelay(err)
		if !fromServer {
			limit := min(p.base<<(attempt-1), p.max)
			d = time.Duration(p.rand() * float64(limit))
		}
		// Дедлайн важнее числа попыток: если пауза его переживёт,
		// ждать бессмысленно — вернём ошибку сейчас, а не через секунду.
		if dl, ok := ctx.Deadline(); ok && p.clk.now().Add(d).After(dl) {
			fmt.Printf(" — пауза %v не уложится в дедлайн, сдаёмся\n", d)
			return err
		}
		src := "экспонента"
		if fromServer {
			src = "RetryInfo"
		}
		fmt.Printf(" — пауза %v (%s)\n", d, src)
		if err := p.clk.sleep(ctx, d); err != nil {
			return err
		}
	}
}

func main() {
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	inv := &inventory{}
	RegisterInventoryServiceServer(srv, inv)
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewInventoryServiceClient(conn)

	unavailable := status.Error(codes.Unavailable, "склад перезапускается")
	run := func(name string, timeout time.Duration, script ...error) {
		inv.mu.Lock()
		inv.script, inv.calls = script, 0
		inv.mu.Unlock()
		clk := &clock{t: time.Now()}
		clk.start = clk.t
		ctx, cancel := context.WithDeadline(context.Background(), clk.now().Add(timeout))
		defer cancel()
		p := policy{attempts: 5, base: 100 * time.Millisecond, max: 2 * time.Second, rand: func() float64 { return 0.5 }, clk: clk}
		fmt.Printf("%s (дедлайн %v):\n", name, timeout)
		err := p.do(ctx, func(ctx context.Context) error {
			_, err := client.Reserve(ctx, &ReserveRequest{Sku: "apple", Quantity: 1})
			return err
		})
		inv.mu.Lock()
		fmt.Printf("  итог: %v, запросов дошло до сервера: %d\n", status.Code(err), inv.calls)
		inv.mu.Unlock()
	}

	run("сбой сети", 5*time.Second, unavailable, unavailable)
	run("перегрузка", 5*time.Second, overloaded(700*time.Millisecond))
	run("неверный запрос", 5*time.Second, status.Error(codes.InvalidArgument, "quantity <= 0"))
	run("склад лежит", 600*time.Millisecond, unavailable, unavailable, unavailable, unavailable, unavailable)

	// Шторм повторов: три слоя (gateway → orders → warehouse), каждый
	// повторяет до 3 раз. Склад лежит — и один запрос пользователя
	// превращается в 3·3·3 = 27 запросов к нему. Бюджет повторов
	// (как `retryThrottling` в service config gRPC) отключает повторы,
	// когда неудач слишком много, и нагрузка остаётся почти исходной.
	fmt.Println("шторм, 100 запросов, склад лежит:")
	fmt.Println("  без бюджета до склада дошло", storm(100, false))
	fmt.Println("  с бюджетом до склада дошло", storm(100, true))
}

// Бюджет — ведро токенов: неудача −1, успех +0.1; повторять можно,
// пока токенов больше половины ведра.
type budget struct{ tokens, max, ratio float64 }

func (b *budget) fail()            { b.tokens = max(0, b.tokens-1) }
func (b *budget) ok()              { b.tokens = min(b.max, b.tokens+b.ratio) }
func (b *budget) allowRetry() bool { return b.tokens > b.max/2 }

// storm считает, сколько вызовов получит лежащий склад от `n`
// запросов пользователей через три слоя с повторами.
func storm(n int, withBudget bool) int {
	hits := 0
	layer := func(b *budget, next func() bool) func() bool {
		return func() bool {
			for attempt := 1; attempt <= 3; attempt++ {
				if next() {
					b.ok()
					return true
				}
				b.fail()
				if withBudget && !b.allowRetry() {
					return false
				}
			}
			return false
		}
	}
	warehouse := func() bool { hits++; return false }
	newBudget := func() *budget { return &budget{tokens: 10, max: 10, ratio: 0.1} }
	call := layer(newBudget(), layer(newBudget(), layer(newBudget(), warehouse)))
	for range n {
		call()
	}
	return hits
}
