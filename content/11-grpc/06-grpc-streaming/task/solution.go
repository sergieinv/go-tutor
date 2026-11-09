package task

import (
	"errors"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type event struct {
	orderID, status string
}

// FeedServer — лента событий заказов в памяти.
type FeedServer struct {
	UnimplementedOrderFeedServiceServer

	mu     sync.Mutex
	events []event // events[i] имеет seq = i+1
	// changed закрывается и заменяется новым при каждой публикации:
	// закрытие канала будит всех ждущих подписчиков сразу.
	changed chan struct{}
}

// NewFeedServer возвращает пустую ленту.
func NewFeedServer() *FeedServer {
	return &FeedServer{changed: make(chan struct{})}
}

func (s *FeedServer) append(e event) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	close(s.changed)
	s.changed = make(chan struct{})
	return int64(len(s.events))
}

// since возвращает копию событий начиная с seq и канал, который
// закроется при следующей публикации.
func (s *FeedServer) since(seq int64) ([]event, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var batch []event
	if int(seq) <= len(s.events) {
		batch = append(batch, s.events[seq-1:]...)
	}
	return batch, s.changed
}

func (s *FeedServer) Publish(stream grpc.ClientStreamingServer[PublishRequest, PublishResponse]) error {
	resp := &PublishResponse{}
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(resp)
		}
		if err != nil {
			return err
		}
		if req.GetOrderId() == "" || req.GetStatus() == "" {
			return status.Error(codes.InvalidArgument, "order_id and status are required")
		}
		// Публикация не ждёт подписчиков: только запись в журнал.
		resp.LastSeq = s.append(event{req.GetOrderId(), req.GetStatus()})
		resp.Accepted++
	}
}

func (s *FeedServer) Subscribe(req *SubscribeRequest, stream grpc.ServerStreamingServer[SubscribeResponse]) error {
	ctx := stream.Context()
	next := max(req.GetFromSeq(), 1)
	for {
		batch, changed := s.since(next)
		for _, e := range batch {
			// Send блокируется, если подписчик не читает (flow control),
			// но держит только эту горутину, а не издателей.
			if err := stream.Send(&SubscribeResponse{Seq: next, OrderId: e.orderID, Status: e.status}); err != nil {
				return err
			}
			next++
		}
		if len(batch) > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case <-changed:
		}
	}
}
