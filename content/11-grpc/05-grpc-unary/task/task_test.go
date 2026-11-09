package task

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// startServer поднимает сервер на bufconn — «сеть» в памяти — и
// возвращает клиента к нему.
func startServer(t *testing.T) OrderServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := NewGRPCServer(NewOrderServer())
	go srv.Serve(lis)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		srv.Stop()
	})
	return NewOrderServiceClient(conn)
}

func callCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestCreateAndGet(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	resp, err := c.CreateOrder(ctx, &CreateOrderRequest{
		Customer: "alice",
		Items: []*Item{
			{Sku: "tea", Quantity: 2, PriceCents: 350},
			{Sku: "cake", Quantity: 1, PriceCents: 500},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	o := resp.GetOrder()
	if o.GetId() != "ord-1" || o.GetCustomer() != "alice" || o.GetTotalCents() != 1200 || len(o.GetItems()) != 2 {
		t.Fatalf("CreateOrder = %v, want ord-1 alice 1200 с двумя позициями", o)
	}
	got, err := c.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got.GetOrder().GetTotalCents() != 1200 || len(got.GetOrder().GetItems()) != 2 || got.GetOrder().GetItems()[1].GetSku() != "cake" {
		t.Fatalf("GetOrder = %v", got.GetOrder())
	}
}

func TestErrors(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	tests := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"нет покупателя", func() error {
			_, err := c.CreateOrder(ctx, &CreateOrderRequest{Items: []*Item{{Sku: "tea", Quantity: 1}}})
			return err
		}, codes.InvalidArgument},
		{"нет позиций", func() error {
			_, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: "bob"})
			return err
		}, codes.InvalidArgument},
		{"нет заказа", func() error {
			_, err := c.GetOrder(ctx, &GetOrderRequest{Id: "ord-42"})
			return err
		}, codes.NotFound},
		{"отмена не реализована", func() error {
			_, err := c.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
			return err
		}, codes.Unimplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(tt.call()); got != tt.want {
				t.Fatalf("код = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestList(t *testing.T) {
	c := startServer(t)
	ctx := callCtx(t)
	for _, who := range []string{"alice", "bob", "alice"} {
		if _, err := c.CreateOrder(ctx, &CreateOrderRequest{Customer: who, Items: []*Item{{Sku: "tea", Quantity: 1, PriceCents: 100}}}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := c.ListOrders(ctx, &ListOrdersRequest{Customer: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range resp.GetOrders() {
		ids = append(ids, o.GetId())
	}
	if len(ids) != 2 || ids[0] != "ord-1" || ids[1] != "ord-3" {
		t.Fatalf("ListOrders(alice) = %v, want [ord-1 ord-3]", ids)
	}
}
