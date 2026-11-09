package warehouse

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/platform/resilience"
)

// scriptedServer отвечает ошибками из очереди, потом — успехом.
type scriptedServer struct {
	warehousev1.UnimplementedWarehouseServiceServer
	mu     sync.Mutex
	errs   []error
	calls  int
	orders []int64
}

func (s *scriptedServer) next(orderID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.orders = append(s.orders, orderID)
	if len(s.errs) == 0 {
		return nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return err
}

func (s *scriptedServer) Reserve(_ context.Context, req *warehousev1.ReserveRequest) (*warehousev1.ReserveResponse, error) {
	if err := s.next(req.GetOrderId()); err != nil {
		return nil, err
	}
	return &warehousev1.ReserveResponse{}, nil
}

func (s *scriptedServer) Release(_ context.Context, req *warehousev1.ReleaseRequest) (*warehousev1.ReleaseResponse, error) {
	if err := s.next(req.GetOrderId()); err != nil {
		return nil, err
	}
	return &warehousev1.ReleaseResponse{}, nil
}

func (s *scriptedServer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newClient(t *testing.T, srv *scriptedServer, reg prometheus.Registerer) *Client {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	warehousev1.RegisterWarehouseServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return New(conn, Config{
		Registerer: reg,
		Retry: resilience.Retry{
			MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond,
			// Без настоящих пауз и случайности.
			Sleep:  func(context.Context, time.Duration) error { return nil },
			Jitter: func(time.Duration) time.Duration { return 0 },
		},
		Breaker: resilience.BreakerConfig{FailureThreshold: 5, OpenTimeout: time.Hour},
	})
}

var unavailable = status.Error(codes.Unavailable, "connection refused")

func TestRetriesUnavailable(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := &scriptedServer{errs: []error{unavailable, unavailable}}
	c := newClient(t, srv, reg)
	if err := c.Reserve(t.Context(), 42, []orders.ItemRequest{{ProductID: 1, Quantity: 1}}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if srv.callCount() != 3 {
		t.Errorf("calls = %d, want 3", srv.callCount())
	}
	// Повтор — с тем же order_id: на нём держится идемпотентность.
	if !slices.Equal(srv.orders, []int64{42, 42, 42}) {
		t.Errorf("order ids = %v", srv.orders)
	}
	expected := `
# HELP orders_warehouse_retries_total Retried warehouse calls (UNAVAILABLE).
# TYPE orders_warehouse_retries_total counter
orders_warehouse_retries_total 2
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "orders_warehouse_retries_total"); err != nil {
		t.Error(err)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	srv := &scriptedServer{errs: []error{unavailable, unavailable, unavailable, unavailable}}
	c := newClient(t, srv, nil)
	err := c.Release(t.Context(), 42)
	if !errors.Is(err, orders.ErrWarehouseUnavailable) {
		t.Fatalf("error = %v, want ErrWarehouseUnavailable", err)
	}
	if srv.callCount() != 3 {
		t.Errorf("calls = %d, want 3", srv.callCount())
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	srv := &scriptedServer{errs: []error{status.Error(codes.InvalidArgument, "bad")}}
	c := newClient(t, srv, nil)
	err := c.Release(t.Context(), 42)
	if err == nil || errors.Is(err, orders.ErrWarehouseUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if srv.callCount() != 1 {
		t.Errorf("calls = %d, want 1", srv.callCount())
	}
}

func outOfStockStatus(domain string, ids ...string) error {
	st := status.New(codes.FailedPrecondition, "not enough stock")
	pf := &errdetails.PreconditionFailure{}
	for _, id := range ids {
		pf.Violations = append(pf.Violations, &errdetails.PreconditionFailure_Violation{Type: "STOCK", Subject: "products/" + id})
	}
	st, _ = st.WithDetails(&errdetails.ErrorInfo{Reason: "OUT_OF_STOCK", Domain: domain}, pf)
	return st.Err()
}

func TestOutOfStockMapsToDomainError(t *testing.T) {
	srv := &scriptedServer{errs: []error{outOfStockStatus(errorDomain, "5", "3")}}
	c := newClient(t, srv, nil)
	err := c.Reserve(t.Context(), 42, []orders.ItemRequest{{ProductID: 3, Quantity: 9}, {ProductID: 5, Quantity: 9}})
	var oos *orders.OutOfStockError
	if !errors.As(err, &oos) || !slices.Equal(oos.ProductIDs, []int64{3, 5}) {
		t.Fatalf("error = %v, want OutOfStockError{3, 5}", err)
	}
	if srv.callCount() != 1 {
		t.Errorf("calls = %d: business refusal must not be retried", srv.callCount())
	}

	// Чужой домен ErrorInfo — не наш «нет товара».
	srv2 := &scriptedServer{errs: []error{outOfStockStatus("other.example", "3")}}
	err = newClient(t, srv2, nil).Reserve(t.Context(), 43, []orders.ItemRequest{{ProductID: 3, Quantity: 1}})
	if errors.Is(err, orders.ErrOutOfStock) {
		t.Errorf("foreign domain mapped to ErrOutOfStock: %v", err)
	}
}

func TestBreakerOpensAndFailsFast(t *testing.T) {
	errs := make([]error, 10)
	for i := range errs {
		errs[i] = unavailable
	}
	srv := &scriptedServer{errs: errs}
	c := newClient(t, srv, nil)
	// Два вызова по три попытки: на пятой неудаче выключатель размыкается,
	// шестая попытка уже не доходит до сервера.
	for range 2 {
		if err := c.Release(t.Context(), 1); !errors.Is(err, orders.ErrWarehouseUnavailable) {
			t.Fatalf("error = %v", err)
		}
	}
	if srv.callCount() != 5 {
		t.Fatalf("calls = %d, want 5", srv.callCount())
	}
	err := c.Release(t.Context(), 1)
	if !errors.Is(err, orders.ErrWarehouseUnavailable) || srv.callCount() != 5 {
		t.Errorf("open breaker: err = %v, calls = %d (want fail fast without a call)", err, srv.callCount())
	}
	if c.breaker.State() != resilience.StateOpen {
		t.Errorf("state = %v", c.breaker.State())
	}
}

func TestCallTimeout(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	srv := &blockingServer{block: block}
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	warehousev1.RegisterWarehouseServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := New(conn, Config{CallTimeout: 50 * time.Millisecond})
	if err := c.Release(t.Context(), 1); !errors.Is(err, orders.ErrWarehouseUnavailable) {
		t.Fatalf("error = %v, want ErrWarehouseUnavailable (deadline)", err)
	}
}

type blockingServer struct {
	warehousev1.UnimplementedWarehouseServiceServer
	block chan struct{}
}

func (s *blockingServer) Release(ctx context.Context, _ *warehousev1.ReleaseRequest) (*warehousev1.ReleaseResponse, error) {
	select {
	case <-s.block:
	case <-ctx.Done():
	}
	return nil, status.FromContextError(ctx.Err()).Err()
}
