package task

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHiddenBudgetBoundary(t *testing.T) {
	for _, tt := range []struct {
		left time.Duration
		want codes.Code
	}{
		{99 * time.Millisecond, codes.DeadlineExceeded},
		{100 * time.Millisecond, codes.OK},
		{0, codes.DeadlineExceeded},
		{-time.Second, codes.DeadlineExceeded},
	} {
		stock := newFakeStock()
		s := NewCheckoutServer(stock, testConfig())
		ctx, _ := incoming(t, "x-request-id", "r")
		ctx, cancel := context.WithDeadline(ctx, fixedNow.Add(tt.left))
		_, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1})
		cancel()
		if status.Code(err) != tt.want {
			t.Fatalf("остаток %v по часам Config.Now: код = %v, want %v", tt.left, status.Code(err), tt.want)
		}
	}
}

func TestHiddenGeneratedID(t *testing.T) {
	for _, kv := range [][]string{nil, {"x-request-id", ""}} {
		stock := newFakeStock()
		s := NewCheckoutServer(stock, testConfig())
		ctx, st := incoming(t, kv...)
		if _, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1}); err != nil {
			t.Fatalf("Checkout: %v", err)
		}
		_, sctx, _ := stock.snapshot()
		out, _ := metadata.FromOutgoingContext(sctx)
		if got := out.Get("x-request-id"); len(got) != 1 || got[0] != "gen-1" {
			t.Fatalf("metadata %v: исходящий x-request-id = %v, want [gen-1] из NewID", kv, got)
		}
		if got := st.header.Get("x-request-id"); len(got) != 1 || got[0] != "gen-1" {
			t.Fatalf("metadata %v: заголовок x-request-id = %v, want [gen-1]", kv, got)
		}
	}
}

func TestHiddenHeaderOnError(t *testing.T) {
	stock := newFakeStock()
	s := NewCheckoutServer(stock, testConfig())
	ctx, st := incoming(t, "x-request-id", "r9")
	ctx, cancel := context.WithDeadline(ctx, fixedNow.Add(time.Millisecond))
	defer cancel()
	if _, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1}); err == nil {
		t.Fatal("ожидалась ошибка DeadlineExceeded")
	}
	if got := st.header.Get("x-request-id"); len(got) != 1 || got[0] != "r9" {
		t.Fatalf("заголовок при ошибке = %v, want [r9]", got)
	}
	for _, req := range []*CheckoutRequest{
		{Sku: "s", Quantity: 1},
		{OrderId: "o", Sku: "s"},
		{OrderId: "o", Sku: "s", Quantity: -1},
	} {
		ctx, _ := incoming(t)
		if _, err := s.Checkout(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%v: код = %v, want InvalidArgument", req, status.Code(err))
		}
	}
}

func TestHiddenDefaultTimeout(t *testing.T) {
	stock := newFakeStock()
	s := NewCheckoutServer(stock, testConfig())
	ctx, _ := incoming(t, "x-request-id", "r")
	before := time.Now()
	if _, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1}); err != nil {
		t.Fatalf("Checkout без дедлайна: %v", err)
	}
	_, sctx, _ := stock.snapshot()
	dl, ok := sctx.Deadline()
	if !ok {
		t.Fatal("у вызова Stock нет дедлайна: без входящего нужен DefaultTimeout")
	}
	if dl.Before(before.Add(time.Second)) || dl.After(time.Now().Add(time.Second)) {
		t.Fatalf("дедлайн Stock через %v, want ≈ DefaultTimeout (1s)", dl.Sub(before))
	}
}

func TestHiddenStockErrorPassthrough(t *testing.T) {
	stock := newFakeStock()
	stock.err = status.Error(codes.ResourceExhausted, "out of stock")
	s := NewCheckoutServer(stock, testConfig())
	ctx, _ := incoming(t, "x-request-id", "r")
	_, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1})
	st := status.Convert(err)
	if st.Code() != codes.ResourceExhausted || st.Message() != "out of stock" {
		t.Fatalf("ошибка = %v, want статус Stock как есть", err)
	}
}

func TestHiddenCancelReachesStock(t *testing.T) {
	stock := newFakeStock()
	stock.block = true
	s := NewCheckoutServer(stock, testConfig())
	ctx, _ := incoming(t, "x-request-id", "r")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-stock.entered; cancel() }()
	_, err := s.Checkout(ctx, &CheckoutRequest{OrderId: "o", Sku: "s", Quantity: 1})
	if calls, _, _ := stock.snapshot(); calls != 1 {
		t.Fatalf("Stock вызван %d раз, want 1", calls)
	}
	if got := <-stock.canceled; !errors.Is(got, context.Canceled) {
		t.Fatalf("Stock увидел %v, want context.Canceled: отмена входящего вызова должна дойти до Stock", got)
	}
	if status.Code(err) != codes.Canceled {
		t.Fatalf("код = %v, want Canceled", status.Code(err))
	}
}

// stockServer — настоящий Stock за gRPC: проверяет, что приходит по сети.
type stockServer struct {
	UnimplementedStockServiceServer
	seen chan metadata.MD
	dl   chan time.Duration
}

func (s *stockServer) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.seen <- md
	if dl, ok := ctx.Deadline(); ok {
		s.dl <- time.Until(dl)
	} else {
		s.dl <- -1
	}
	return &ReserveResponse{ReservationId: "net-" + req.GetOrderId()}, nil
}

func dialBuf(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestHiddenOverNetwork(t *testing.T) {
	stockLis := bufconn.Listen(1 << 20)
	stockSrv := grpc.NewServer()
	ss := &stockServer{seen: make(chan metadata.MD, 1), dl: make(chan time.Duration, 1)}
	RegisterStockServiceServer(stockSrv, ss)
	go stockSrv.Serve(stockLis)
	t.Cleanup(stockSrv.Stop)

	cfg := testConfig()
	cfg.Now = time.Now
	checkoutLis := bufconn.Listen(1 << 20)
	checkoutSrv := grpc.NewServer()
	RegisterCheckoutServiceServer(checkoutSrv, NewCheckoutServer(NewStockServiceClient(dialBuf(t, stockLis)), cfg))
	go checkoutSrv.Serve(checkoutLis)
	t.Cleanup(checkoutSrv.Stop)

	client := NewCheckoutServiceClient(dialBuf(t, checkoutLis))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", "net-1", "authorization", "Bearer secret")
	var header metadata.MD
	res, err := client.Checkout(ctx, &CheckoutRequest{OrderId: "ord-7", Sku: "tea", Quantity: 1}, grpc.Header(&header))
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if res.GetReservationId() != "net-ord-7" {
		t.Fatalf("ответ = %v", res)
	}
	md := <-ss.seen
	if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "net-1" {
		t.Fatalf("Stock получил x-request-id = %v, want [net-1]", got)
	}
	if got := md.Get("authorization"); len(got) != 0 {
		t.Fatalf("Stock получил authorization = %v: входящую metadata нельзя пробрасывать целиком", got)
	}
	if left := <-ss.dl; left <= 0 || left > 3*time.Second {
		t.Fatalf("остаток у Stock = %v, want дедлайн клиента (≤ 3s)", left)
	}
	if got := header.Get("x-request-id"); len(got) != 1 || got[0] != "net-1" {
		t.Fatalf("клиент получил заголовок x-request-id = %v, want [net-1]", got)
	}
}
