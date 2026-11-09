// Package e2e_test — сквозные тесты саги в одном процессе: настоящий
// gRPC-сервер «Заказов» (те же interceptor'ы, что в main), настоящий
// «Склад» (warehousetest: его домен и gRPC-сервер на хранилище в памяти),
// оба на bufconn; «Заказы» на SQLite; Kafka — kfake. Сбои внедряются в
// склад (warehousetest.SetFault) и в хранилище (обёртка).
package e2e_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	ordersv1 "gotutor/labs/api/gen/orders/v1"
	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/orders/internal/grpcapi"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/outbox"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/warehouse"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/resilience"
	"gotutor/labs/warehouse/warehousetest"
)

const topic = "orders.events"

type system struct {
	orders   ordersv1.OrderServiceClient
	wh       *warehousetest.Warehouse
	store    *sqlite.Store
	relay    *outbox.Relay
	cluster  *kfake.Cluster
	failNext *atomic.Bool // следующая запись заказа в базу упадёт
	keyboard orders.Product
	monitor  orders.Product
}

func dial(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpcx.NewClient("passthrough:///bufnet", grpcx.ClientConfig{},
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newSystem(t *testing.T) *system {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	keyboard, _ := store.CreateProduct(t.Context(), orders.NewProduct{Name: "Клавиатура", PriceCents: 499000})
	monitor, _ := store.CreateProduct(t.Context(), orders.NewProduct{Name: "Монитор", PriceCents: 2499000})

	// «Склад»: те же id товаров, остатки 10 и 3.
	wh := warehousetest.New(map[int64]int32{keyboard.ID: 10, monitor.ID: 3})
	whLis := bufconn.Listen(1 << 20)
	wh.Serve(t, whLis)
	whClient := warehouse.New(dial(t, whLis), warehouse.Config{
		Retry: resilience.Retry{
			MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond,
			Sleep: func(context.Context, time.Duration) error { return nil },
		},
	})

	failNext := &atomic.Bool{}
	svc := orders.NewService(&flakyStore{Store: store, failNext: failNext}, whClient, orders.Options{Log: log})
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	srv := grpcx.NewServer(grpcx.ServerConfig{Logger: log, Validator: v})
	ordersv1.RegisterOrderServiceServer(srv, grpcapi.New(svc, log))
	ordersLis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(ordersLis) }()
	t.Cleanup(srv.Stop)

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, topic))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(producer.Close)

	return &system{
		orders:   ordersv1.NewOrderServiceClient(dial(t, ordersLis)),
		wh:       wh,
		store:    store,
		relay:    outbox.New(store, producer, outbox.Config{Topic: topic}),
		cluster:  cluster,
		failNext: failNext,
		keyboard: keyboard,
		monitor:  monitor,
	}
}

// flakyStore роняет запись заказа по флагу — сбой базы после резерва.
type flakyStore struct {
	orders.Store
	failNext *atomic.Bool
}

func (s *flakyStore) Begin(ctx context.Context) (orders.Tx, error) {
	tx, err := s.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &flakyTx{Tx: tx, fail: s.failNext.Swap(false)}, nil
}

type flakyTx struct {
	orders.Tx
	fail bool
}

func (t *flakyTx) CreateOrder(ctx context.Context, o orders.Order) error {
	if t.fail {
		return errors.New("connection reset by peer")
	}
	return t.Tx.CreateOrder(ctx, o)
}

func (s *system) create(t *testing.T, items ...*ordersv1.CreateOrderItem) (*ordersv1.Order, error) {
	t.Helper()
	resp, err := s.orders.CreateOrder(t.Context(), &ordersv1.CreateOrderRequest{CustomerId: 42, Items: items})
	return resp.GetOrder(), err
}

func item(p orders.Product, qty int32) *ordersv1.CreateOrderItem {
	return &ordersv1.CreateOrderItem{ProductId: p.ID, Quantity: qty}
}

func (s *system) assertStock(t *testing.T, p orders.Product, available, reserved int32) {
	t.Helper()
	if a, r := s.wh.Stock(p.ID); a != available || r != reserved {
		t.Errorf("stock of %s = %d available / %d reserved, want %d / %d", p.Name, a, r, available, reserved)
	}
}

func (s *system) assertReservation(t *testing.T, orderID int64, want warehousev1.ReservationStatus) {
	t.Helper()
	if got, ok := s.wh.Reservation(orderID); !ok || got != want {
		t.Errorf("reservation of order %d = %v (exists %v), want %v", orderID, got, ok, want)
	}
}

// events публикует outbox и читает все события топика по порядку.
func (s *system) events(t *testing.T, want int) []*eventsv1.OrderEvent {
	t.Helper()
	if _, err := s.relay.PublishOnce(t.Context()); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if want == 0 {
		return nil
	}
	cl, err := kgo.NewClient(kgo.SeedBrokers(s.cluster.ListenAddrs()...), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var out []*eventsv1.OrderEvent
	for len(out) < want {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("got %d events, want %d", len(out), want)
		}
		for _, r := range fetches.Records() {
			ev := &eventsv1.OrderEvent{}
			if err := proto.Unmarshal(r.Value, ev); err != nil {
				t.Fatal(err)
			}
			out = append(out, ev)
		}
	}
	return out
}

func TestSagaHappyPathAndCancel(t *testing.T) {
	s := newSystem(t)
	o, err := s.create(t, item(s.monitor, 2), item(s.keyboard, 1))
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	s.assertStock(t, s.monitor, 1, 2)
	s.assertStock(t, s.keyboard, 9, 1)
	s.assertReservation(t, o.GetOrderId(), warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED)

	if _, err := s.orders.CancelOrder(t.Context(), &ordersv1.CancelOrderRequest{OrderId: o.GetOrderId()}); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	s.assertStock(t, s.monitor, 3, 0)
	s.assertStock(t, s.keyboard, 10, 0)
	s.assertReservation(t, o.GetOrderId(), warehousev1.ReservationStatus_RESERVATION_STATUS_RELEASED)

	evs := s.events(t, 2)
	if evs[0].GetOrderCreated() == nil || evs[1].GetOrderCancelled() == nil ||
		evs[0].GetOrderId() != o.GetOrderId() || evs[0].GetOrderCreated().GetTotalCents() != 2*2499000+499000 {
		t.Errorf("events = %v", evs)
	}
}

func TestSagaOutOfStock(t *testing.T) {
	s := newSystem(t)
	_, err := s.create(t, item(s.keyboard, 1), item(s.monitor, 4))
	st := status.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("code = %v (%v), want FailedPrecondition", st.Code(), err)
	}
	// Всё или ничего: клавиатуру тоже не зарезервировали.
	s.assertStock(t, s.keyboard, 10, 0)
	s.assertStock(t, s.monitor, 3, 0)
	if s.wh.Calls("Release") != 0 {
		t.Error("Release after a definite refusal")
	}
	s.events(t, 0)
	if n := pending(t, s.store); n != 0 {
		t.Errorf("outbox = %d, want 0", n)
	}
}

func TestSagaCompensatesWriteFailure(t *testing.T) {
	s := newSystem(t)
	s.failNext.Store(true)
	_, err := s.create(t, item(s.monitor, 2))
	if st := status.Convert(err); st.Code() != codes.Internal || st.Message() != "internal error" {
		t.Fatalf("status = %v %q, want Internal without details", st.Code(), st.Message())
	}
	// Резерв сделан и отменён компенсацией.
	if s.wh.Calls("Reserve") != 1 || s.wh.Calls("Release") != 1 {
		t.Errorf("calls: reserve %d, release %d", s.wh.Calls("Reserve"), s.wh.Calls("Release"))
	}
	s.assertStock(t, s.monitor, 3, 0)
	if n := pending(t, s.store); n != 0 {
		t.Errorf("outbox = %d, want 0", n)
	}
	// Следующий заказ проходит.
	if _, err := s.create(t, item(s.monitor, 3)); err != nil {
		t.Fatalf("next order: %v", err)
	}
	s.assertStock(t, s.monitor, 0, 3)
}

func TestSagaCompensatesLostReserveResponse(t *testing.T) {
	s := newSystem(t)
	// Склад резервирует, но ответ теряется — на каждой попытке.
	s.wh.SetFault(func(method string, phase warehousetest.Phase) error {
		if method == "Reserve" && phase == warehousetest.After {
			return status.Error(codes.Unavailable, "connection reset")
		}
		return nil
	})
	_, err := s.create(t, item(s.monitor, 2))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("error = %v, want Unavailable", err)
	}
	// Повторы с тем же order_id не списали товар трижды (идемпотентность),
	// а компенсация вернула единственный резерв.
	if s.wh.Calls("Reserve") != 3 {
		t.Errorf("reserve calls = %d, want 3", s.wh.Calls("Reserve"))
	}
	s.assertStock(t, s.monitor, 3, 0)
}

func TestSagaRetriesTransientFailure(t *testing.T) {
	s := newSystem(t)
	var failed atomic.Bool
	s.wh.SetFault(func(method string, phase warehousetest.Phase) error {
		if method == "Reserve" && phase == warehousetest.Before && !failed.Swap(true) {
			return status.Error(codes.Unavailable, "restarting")
		}
		return nil
	})
	o, err := s.create(t, item(s.keyboard, 1))
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if s.wh.Calls("Reserve") != 2 || s.wh.Calls("Release") != 0 {
		t.Errorf("calls: reserve %d, release %d; want 2, 0", s.wh.Calls("Reserve"), s.wh.Calls("Release"))
	}
	s.assertReservation(t, o.GetOrderId(), warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED)
	s.events(t, 1)
}

func pending(t *testing.T, s *sqlite.Store) int64 {
	t.Helper()
	n, _, err := s.PendingStats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return n
}
