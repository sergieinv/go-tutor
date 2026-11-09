package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeStock — клиент Stock в памяти: запоминает контекст и запрос.
type fakeStock struct {
	mu       sync.Mutex
	calls    int
	ctx      context.Context
	req      *ReserveRequest
	err      error
	block    bool          // ждать отмены контекста
	entered  chan struct{} // закрывается при входе, если block
	canceled chan error    // чем закончилось ожидание, если block
}

func newFakeStock() *fakeStock {
	return &fakeStock{entered: make(chan struct{}), canceled: make(chan error, 1)}
}

func (f *fakeStock) Reserve(ctx context.Context, req *ReserveRequest, _ ...grpc.CallOption) (*ReserveResponse, error) {
	f.mu.Lock()
	f.calls++
	f.ctx, f.req = ctx, req
	block, err := f.block, f.err
	f.mu.Unlock()
	if block {
		close(f.entered)
		select {
		case <-ctx.Done():
			f.canceled <- ctx.Err()
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-time.After(2 * time.Second):
			f.canceled <- nil
			return nil, status.Error(codes.Internal, "отмена не дошла до Stock")
		}
	}
	if err != nil {
		return nil, err
	}
	return &ReserveResponse{ReservationId: "res-" + req.GetOrderId()}, nil
}

func (f *fakeStock) snapshot() (int, context.Context, *ReserveRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.ctx, f.req
}

// fakeStream подменяет поток сервера, чтобы grpc.SetHeader работал
// при прямом вызове метода.
type fakeStream struct {
	mu     sync.Mutex
	header metadata.MD
}

func (s *fakeStream) Method() string { return "/orders.v1.CheckoutService/Checkout" }
func (s *fakeStream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.header = metadata.Join(s.header, md)
	return nil
}
func (s *fakeStream) SendHeader(md metadata.MD) error { return s.SetHeader(md) }
func (s *fakeStream) SetTrailer(metadata.MD) error    { return nil }

// fixedNow — момент «сейчас» для тестов: на час вперёд, чтобы
// контексты с дедлайном от него не истекли по настоящим часам.
var fixedNow = time.Now().Add(time.Hour)

func testConfig() Config {
	return Config{
		MinBudget:      100 * time.Millisecond,
		DefaultTimeout: time.Second,
		Now:            func() time.Time { return fixedNow },
		NewID:          func() string { return "gen-1" },
	}
}

// incoming собирает контекст входящего вызова.
func incoming(t *testing.T, kv ...string) (context.Context, *fakeStream) {
	t.Helper()
	st := &fakeStream{}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(kv...))
	ctx = grpc.NewContextWithServerTransportStream(ctx, st)
	return ctx, st
}

func TestForwardsRequestIDAndDeadline(t *testing.T) {
	stock := newFakeStock()
	s := NewCheckoutServer(stock, testConfig())
	ctx, st := incoming(t, "x-request-id", "r1", "authorization", "Bearer secret")
	deadline := fixedNow.Add(time.Second)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	res, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "ord-1", Sku: "tea", Quantity: 2})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if res.GetOrderId() != "ord-1" || res.GetReservationId() != "res-ord-1" {
		t.Fatalf("ответ = %v", res)
	}
	calls, sctx, sreq := stock.snapshot()
	if calls != 1 || sreq.GetSku() != "tea" || sreq.GetQuantity() != 2 || sreq.GetOrderId() != "ord-1" {
		t.Fatalf("Stock: вызовов %d, запрос %v", calls, sreq)
	}
	out, _ := metadata.FromOutgoingContext(sctx)
	if len(out) != 1 || len(out.Get("x-request-id")) != 1 || out.Get("x-request-id")[0] != "r1" {
		t.Fatalf("исходящая metadata = %v, want только x-request-id: r1", out)
	}
	if dl, ok := sctx.Deadline(); !ok || !dl.Equal(deadline) {
		t.Fatalf("дедлайн у Stock = %v, %v; want входящий %v", dl, ok, deadline)
	}
	if got := st.header.Get("x-request-id"); len(got) != 1 || got[0] != "r1" {
		t.Fatalf("заголовок ответа x-request-id = %v, want [r1]", got)
	}
}

func TestRejectsShortBudget(t *testing.T) {
	stock := newFakeStock()
	s := NewCheckoutServer(stock, testConfig())
	ctx, _ := incoming(t, "x-request-id", "r2")
	ctx, cancel := context.WithDeadline(ctx, fixedNow.Add(50*time.Millisecond))
	defer cancel()
	_, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "ord-1", Sku: "tea", Quantity: 1})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("код = %v, want DeadlineExceeded", status.Code(err))
	}
	if calls, _, _ := stock.snapshot(); calls != 0 {
		t.Fatalf("Stock вызван %d раз, want 0", calls)
	}
}

func TestInvalidRequest(t *testing.T) {
	stock := newFakeStock()
	s := NewCheckoutServer(stock, testConfig())
	ctx, _ := incoming(t, "x-request-id", "r3")
	_, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "ord-1", Quantity: 1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
	}
	if calls, _, _ := stock.snapshot(); calls != 0 {
		t.Fatalf("Stock вызван %d раз, want 0", calls)
	}
}
