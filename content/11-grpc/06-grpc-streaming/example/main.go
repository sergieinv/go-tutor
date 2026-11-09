// Три вида стримов на одном сервисе (`orders.proto` рядом).
// Обработчик стрима не возвращает ответ, а получает объект
// стрима: `Send` пишет сообщение, `Recv` читает, а возврат из
// обработчика завершает вызов — `nil` превращается в статус
// OK в трейлерах.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type streamServer struct {
	UnimplementedOrderStreamServiceServer
	// Чем закончился бесконечный стрим — для демонстрации отмены.
	followDone chan error
}

// Server streaming: обработчик шлёт сообщения, пока хочет.
// Ошибка `Send` означает, что клиент ушёл, — дальше слать
// некому, возвращаемся.
func (s *streamServer) WatchOrders(req *WatchOrdersRequest, stream grpc.ServerStreamingServer[WatchOrdersResponse]) error {
	statuses := []string{"created", "paid", "shipped"}
	for i := int64(1); req.GetFollow() || i <= 3; i++ {
		err := stream.Send(&WatchOrdersResponse{
			Seq: i, OrderId: "ord-1", Status: statuses[(i-1)%3],
		})
		if err != nil {
			// Контекст стрима отменён вместе с вызовом.
			s.followDone <- stream.Context().Err()
			return err
		}
	}
	return nil
}

// Client streaming: читаем до `io.EOF` — так сервер узнаёт,
// что клиент вызвал `CloseSend`. Ответ один — `SendAndClose`.
func (s *streamServer) UploadItems(stream grpc.ClientStreamingServer[UploadItemsRequest, UploadItemsResponse]) error {
	resp := &UploadItemsResponse{}
	for {
		it, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(resp)
		}
		if err != nil {
			return err
		}
		resp.Count++
		resp.TotalCents += int64(it.GetQuantity()) * it.GetPriceCents()
	}
}

// Bidi: чтение и запись независимы. Здесь — на каждый запрос
// один ответ; `io.EOF` от клиента — конец и нашего потока.
func (s *streamServer) TrackOrders(stream grpc.BidiStreamingServer[TrackOrdersRequest, TrackOrdersResponse]) error {
	known := map[string]string{"ord-1": "shipped", "ord-2": "paid"}
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		st, ok := known[req.GetOrderId()]
		if !ok {
			st = "unknown"
		}
		if err := stream.Send(&TrackOrdersResponse{OrderId: req.GetOrderId(), Status: st}); err != nil {
			return err
		}
	}
}

func main() {
	srv := grpc.NewServer()
	impl := &streamServer{followDone: make(chan error, 1)}
	RegisterOrderStreamServiceServer(srv, impl)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := NewOrderStreamServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Server streaming у клиента: `Recv` до `io.EOF` — это
	// нормальный конец, а не ошибка.
	watch, err := client.WatchOrders(ctx, &WatchOrdersRequest{Customer: "alice"})
	if err != nil {
		log.Fatal(err)
	}
	for {
		ev, err := watch.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("событие:", ev.GetSeq(), ev.GetStatus())
	}

	// Client streaming: несколько `Send`, затем
	// `CloseAndRecv` — закрыть свою сторону и ждать ответ.
	up, err := client.UploadItems(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, it := range []*UploadItemsRequest{
		{Sku: "tea", Quantity: 2, PriceCents: 350},
		{Sku: "cake", Quantity: 1, PriceCents: 500},
	} {
		if err := up.Send(it); err != nil {
			log.Fatal(err)
		}
	}
	sum, err := up.CloseAndRecv()
	fmt.Println("загружено:", sum.GetCount(), sum.GetTotalCents(), err)

	// Bidi: здесь пинг-понг по очереди, но можно и читать в
	// отдельной горутине. `CloseSend` говорит серверу «больше
	// не пишу», и его `Recv` получает `io.EOF`.
	track, err := client.TrackOrders(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, id := range []string{"ord-2", "ord-1", "ord-7"} {
		if err := track.Send(&TrackOrdersRequest{OrderId: id}); err != nil {
			log.Fatal(err)
		}
		r, err := track.Recv()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("статус:", r.GetOrderId(), r.GetStatus())
	}
	track.CloseSend()
	_, err = track.Recv()
	fmt.Println("конец bidi:", err)

	// Отмена посреди стрима. Сервер шлёт без остановки, а
	// HTTP/2 flow control не даёт ему уйти далеко вперёд:
	// когда окно получателя заполнено, `Send` блокируется.
	followCtx, stop := context.WithCancel(ctx)
	follow, err := client.WatchOrders(followCtx, &WatchOrdersRequest{Follow: true})
	if err != nil {
		log.Fatal(err)
	}
	for range 3 {
		if _, err := follow.Recv(); err != nil {
			log.Fatal(err)
		}
	}
	stop()
	// После отмены `Recv` может ещё отдать уже полученные
	// сообщения, но рано или поздно вернёт ошибку.
	for err == nil {
		_, err = follow.Recv()
	}
	fmt.Println("клиент:", status.Code(err))
	fmt.Println("сервер:", <-impl.followDone)
}
