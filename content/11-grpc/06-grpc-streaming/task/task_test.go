package task

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type feed struct {
	lis *bufconn.Listener
	srv *grpc.Server
}

// startFeed поднимает ленту на bufconn.
func startFeed(t *testing.T) *feed {
	t.Helper()
	f := &feed{lis: bufconn.Listen(1 << 20), srv: grpc.NewServer()}
	RegisterOrderFeedServiceServer(f.srv, NewFeedServer())
	go f.srv.Serve(f.lis)
	t.Cleanup(f.srv.Stop)
	return f
}

// client открывает отдельное соединение. Окно flow control
// фиксировано в 64 КиБ: так нечитающий подписчик быстро
// упирается в него, как в настоящей сети.
func (f *feed) client(t *testing.T) OrderFeedServiceClient {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return f.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(64<<10),
		grpc.WithInitialConnWindowSize(64<<10))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewOrderFeedServiceClient(conn)
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// publish отправляет события одним вызовом Publish. Если сервер
// уже завершил вызов с ошибкой, Send вернёт io.EOF — настоящую
// ошибку покажет CloseAndRecv.
func publish(ctx context.Context, c OrderFeedServiceClient, events ...*PublishRequest) (*PublishResponse, error) {
	stream, err := c.Publish(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if err := stream.Send(e); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	return stream.CloseAndRecv()
}

func ev(order, st string) *PublishRequest { return &PublishRequest{OrderId: order, Status: st} }

// recvN читает n событий и проверяет, что seq идут подряд с first.
func recvN(t *testing.T, s grpc.ServerStreamingClient[SubscribeResponse], first int64, n int) []*SubscribeResponse {
	t.Helper()
	var got []*SubscribeResponse
	for i := range n {
		m, err := s.Recv()
		if err != nil {
			t.Fatalf("Recv #%d: %v", i, err)
		}
		if want := first + int64(i); m.GetSeq() != want {
			t.Fatalf("seq = %d, want %d", m.GetSeq(), want)
		}
		got = append(got, m)
	}
	return got
}

func TestPublishAndSubscribe(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)

	resp, err := publish(ctx, c, ev("ord-1", "created"), ev("ord-1", "paid"), ev("ord-2", "created"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.GetAccepted() != 3 || resp.GetLastSeq() != 3 {
		t.Fatalf("Publish = %v, want accepted 3, last_seq 3", resp)
	}

	sub, err := c.Subscribe(ctx, &SubscribeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := recvN(t, sub, 1, 3)
	if got[1].GetOrderId() != "ord-1" || got[1].GetStatus() != "paid" {
		t.Fatalf("второе событие = %v, want ord-1 paid", got[1])
	}

	// Новые события приходят в уже открытый стрим.
	resp, err = publish(ctx, c, ev("ord-2", "paid"))
	if err != nil || resp.GetLastSeq() != 4 {
		t.Fatalf("Publish = %v, %v; want last_seq 4", resp, err)
	}
	if m := recvN(t, sub, 4, 1)[0]; m.GetOrderId() != "ord-2" || m.GetStatus() != "paid" {
		t.Fatalf("живое событие = %v, want ord-2 paid", m)
	}
}

func TestPublishInvalid(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	_, err := publish(ctx, c, ev("ord-1", "created"), ev("", "paid"), ev("ord-1", "paid"))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
	}
}

func TestSubscribeFromSeq(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	if _, err := publish(ctx, c, ev("a", "1"), ev("b", "2"), ev("c", "3")); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(ctx, &SubscribeRequest{FromSeq: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := recvN(t, sub, 2, 2); got[0].GetOrderId() != "b" {
		t.Fatalf("первое событие = %v, want b", got[0])
	}
}
