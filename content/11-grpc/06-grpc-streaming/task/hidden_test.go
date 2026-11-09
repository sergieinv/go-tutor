package task

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHiddenInvalidKeepsEarlier(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	_, err := publish(ctx, c, ev("ord-1", "created"), ev("ord-2", "created"), ev("ord-3", ""), ev("ord-4", "created"))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("пустой status: код = %v, want InvalidArgument", status.Code(err))
	}
	resp, err := publish(ctx, c, ev("ord-5", "created"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetAccepted() != 1 || resp.GetLastSeq() != 3 {
		t.Fatalf("после ошибки Publish = %v, want accepted 1, last_seq 3 (два события до ошибки опубликованы)", resp)
	}
	sub, err := c.Subscribe(ctx, &SubscribeRequest{FromSeq: -5})
	if err != nil {
		t.Fatal(err)
	}
	got := recvN(t, sub, 1, 3)
	if got[0].GetOrderId() != "ord-1" || got[1].GetOrderId() != "ord-2" || got[2].GetOrderId() != "ord-5" {
		t.Fatalf("события = %v, want ord-1, ord-2, ord-5", got)
	}
}

func TestHiddenEmptyPublish(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	resp, err := publish(ctx, c)
	if err != nil || resp.GetAccepted() != 0 || resp.GetLastSeq() != 0 {
		t.Fatalf("пустой Publish = %v, %v; want accepted 0, last_seq 0", resp, err)
	}
}

func TestHiddenVisibleBeforeClose(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	sub, err := c.Subscribe(ctx, &SubscribeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := c.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := pub.Send(ev("ord-1", "created")); err != nil {
		t.Fatal(err)
	}
	// Издатель ещё не закрыл поток, а подписчик уже видит событие.
	recvN(t, sub, 1, 1)
	resp, err := pub.CloseAndRecv()
	if err != nil || resp.GetAccepted() != 1 {
		t.Fatalf("CloseAndRecv = %v, %v", resp, err)
	}
}

func TestHiddenFutureFromSeq(t *testing.T) {
	f := startFeed(t)
	c := f.client(t)
	ctx := testCtx(t)
	if _, err := publish(ctx, c, ev("a", "1"), ev("b", "2")); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(ctx, &SubscribeRequest{FromSeq: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publish(ctx, c, ev("c", "3"), ev("d", "4"), ev("e", "5")); err != nil {
		t.Fatal(err)
	}
	if got := recvN(t, sub, 4, 2); got[0].GetOrderId() != "d" || got[1].GetOrderId() != "e" {
		t.Fatalf("события = %v, want d, e", got)
	}
}

func TestHiddenSlowSubscriberDoesNotBlock(t *testing.T) {
	f := startFeed(t)
	ctx := testCtx(t)
	slowClient := f.client(t)
	slow, err := slowClient.Subscribe(ctx, &SubscribeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Дождаться, что подписка открыта на сервере: первое событие дошло.
	pubClient := f.client(t)
	if _, err := publish(ctx, pubClient, ev("first", "x")); err != nil {
		t.Fatal(err)
	}
	recvN(t, slow, 1, 1)

	// Подписчик не читает, а издатель шлёт ~600 КиБ — в десять раз
	// больше окна flow control.
	const n = 3000
	big := strings.Repeat("s", 200)
	events := make([]*PublishRequest, n)
	for i := range events {
		events[i] = ev(fmt.Sprintf("ord-%d", i), big)
	}
	done := make(chan error, 1)
	go func() {
		resp, err := publish(ctx, pubClient, events...)
		if err == nil && resp.GetAccepted() != n {
			err = fmt.Errorf("accepted = %d, want %d", resp.GetAccepted(), n)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Publish не завершился, пока подписчик не читает: издатель ждёт подписчика")
	}
	// Медленный подписчик ничего не потерял.
	got := recvN(t, slow, 2, n)
	if got[n-1].GetOrderId() != fmt.Sprintf("ord-%d", n-1) {
		t.Fatalf("последнее событие = %v", got[n-1].GetOrderId())
	}
}

func TestHiddenCancelReleasesHandler(t *testing.T) {
	// Свой сервер без Stop в Cleanup: если GracefulStop завис, Stop
	// тоже не вернётся, и тест не смог бы сообщить об ошибке.
	f := &feed{lis: bufconn.Listen(1 << 20), srv: grpc.NewServer()}
	RegisterOrderFeedServiceServer(f.srv, NewFeedServer())
	go f.srv.Serve(f.lis)
	c := f.client(t)
	ctx := testCtx(t)
	if _, err := publish(ctx, c, ev("a", "1")); err != nil {
		t.Fatal(err)
	}
	var cancels []context.CancelFunc
	for range 3 {
		subCtx, cancel := context.WithCancel(ctx)
		cancels = append(cancels, cancel)
		sub, err := c.Subscribe(subCtx, &SubscribeRequest{})
		if err != nil {
			t.Fatal(err)
		}
		recvN(t, sub, 1, 1) // обработчик дошёл до ожидания новых событий
	}
	for _, cancel := range cancels {
		cancel()
	}
	// GracefulStop ждёт все обработчики: если подписка не заметила
	// отмену, он не вернётся.
	stopped := make(chan struct{})
	go func() { f.srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop не вернулся: обработчик Subscribe не завершился после отмены клиента")
	}
}

func TestHiddenConcurrent(t *testing.T) {
	f := startFeed(t)
	ctx := testCtx(t)
	const pubs, per = 4, 50
	type sub struct {
		got []*SubscribeResponse
		err error
	}
	subs := make([]sub, 3)
	var subWG sync.WaitGroup
	for i := range subs {
		s, err := f.client(t).Subscribe(ctx, &SubscribeRequest{})
		if err != nil {
			t.Fatal(err)
		}
		subWG.Go(func() {
			for len(subs[i].got) < pubs*per {
				m, err := s.Recv()
				if err != nil {
					subs[i].err = err
					return
				}
				subs[i].got = append(subs[i].got, m)
			}
		})
	}
	var pubWG sync.WaitGroup
	errs := make(chan error, pubs)
	for p := range pubs {
		c := f.client(t)
		pubWG.Go(func() {
			var events []*PublishRequest
			for i := range per {
				events = append(events, ev(fmt.Sprintf("p%d", p), fmt.Sprint(i)))
			}
			_, err := publish(ctx, c, events...)
			errs <- err
		})
	}
	pubWG.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	subWG.Wait()
	for i, s := range subs {
		if s.err != nil {
			t.Fatalf("подписчик %d: %v", i, s.err)
		}
		last := map[string]int{}
		for j, m := range s.got {
			if m.GetSeq() != int64(j+1) {
				t.Fatalf("подписчик %d: событие %d с seq %d", i, j, m.GetSeq())
			}
			var n int
			fmt.Sscan(m.GetStatus(), &n)
			if prev, ok := last[m.GetOrderId()]; ok && n != prev+1 {
				t.Fatalf("подписчик %d: у %s после %d пришло %d — порядок издателя нарушен", i, m.GetOrderId(), prev, n)
			}
			last[m.GetOrderId()] = n
		}
	}
}
