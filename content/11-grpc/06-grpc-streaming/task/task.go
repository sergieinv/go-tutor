package task

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FeedServer — лента событий заказов в памяти.
type FeedServer struct {
	UnimplementedOrderFeedServiceServer
	// TODO: состояние
}

// NewFeedServer возвращает пустую ленту.
func NewFeedServer() *FeedServer {
	return &FeedServer{}
}

func (s *FeedServer) Publish(stream grpc.ClientStreamingServer[PublishRequest, PublishResponse]) error {
	return status.Error(codes.Unimplemented, "TODO")
}

func (s *FeedServer) Subscribe(req *SubscribeRequest, stream grpc.ServerStreamingServer[SubscribeResponse]) error {
	return status.Error(codes.Unimplemented, "TODO")
}
