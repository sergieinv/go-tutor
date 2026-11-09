// Health — стандартный сервис `grpc.health.v1.Health`: балансировщик,
// k8s и соседние сервисы спрашивают у него, готов ли сервер
// принимать запросы. Reflection — сервис, через который grpcurl и
// grpcui узнают контракт сервера без .proto. Пример поднимает
// оба рядом с OrderService и ходит в них обычным gRPC-клиентом.

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

type orders struct {
	UnimplementedOrderServiceServer
}

const ordersName = "shop.v1.OrderService"

func main() {
	// `health.NewServer` — готовая реализация. Статусы хранятся
	// по имени сервиса; пустое имя — «сервер в целом», оно
	// SERVING сразу. Статус своего сервиса выставляем сами.
	// `reflection.Register` публикует контракты всех
	// зарегистрированных сервисов.
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	hs := health.NewServer()
	RegisterOrderServiceServer(srv, orders{})
	healthpb.RegisterHealthServer(srv, hs)
	reflection.Register(srv)
	hs.SetServingStatus(ordersName, healthpb.HealthCheckResponse_SERVING)
	go srv.Serve(lis)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	hc := healthpb.NewHealthClient(conn)

	// `Check` — разовый вопрос. Сервис, о котором health не
	// знает, — ошибка `NotFound`, а не статус.
	for _, name := range []string{"", ordersName, "shop.v1.Payments"} {
		resp, err := hc.Check(ctx, &healthpb.HealthCheckRequest{Service: name})
		fmt.Printf("Check(%q): %v %v\n", name, resp.GetStatus(), status.Code(err))
	}

	// `Watch` — серверный стрим: первое сообщение — текущий
	// статус, дальше — только изменения. Так клиент узнаёт о
	// проблеме сразу, без опроса. Здесь «отвалилась база»:
	// сервис перестаёт обслуживать, потом восстанавливается.
	wctx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	watch, err := hc.Watch(wctx, &healthpb.HealthCheckRequest{Service: ordersName})
	if err != nil {
		log.Fatal(err)
	}
	next := func() healthpb.HealthCheckResponse_ServingStatus {
		m, err := watch.Recv()
		if err != nil {
			log.Fatal(err)
		}
		return m.GetStatus()
	}
	fmt.Println("Watch:", next())
	hs.SetServingStatus(ordersName, healthpb.HealthCheckResponse_NOT_SERVING)
	fmt.Println("Watch: база недоступна →", next())
	hs.SetServingStatus(ordersName, healthpb.HealthCheckResponse_SERVING)
	fmt.Println("Watch: база вернулась →", next())

	// Reflection — двунаправленный стрим вопросов о контракте.
	// Так работает `grpcurl list`: спрашиваем список сервисов…
	rc, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		log.Fatal(err)
	}
	ask := func(req *reflectionpb.ServerReflectionRequest) *reflectionpb.ServerReflectionResponse {
		if err := rc.Send(req); err != nil {
			log.Fatal(err)
		}
		resp, err := rc.Recv()
		if err != nil {
			log.Fatal(err)
		}
		return resp
	}
	var names []string
	for _, s := range ask(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{},
	}).GetListServicesResponse().GetService() {
		names = append(names, s.GetName())
	}
	slices.Sort(names)
	fmt.Println("reflection:", names)

	// …а потом — дескриптор файла с нужным сервисом. Это
	// скомпилированный .proto: из него grpcurl узнаёт методы и
	// поля и умеет собрать запрос из JSON.
	fd := ask(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: ordersName},
	}).GetFileDescriptorResponse().GetFileDescriptorProto()
	var file descriptorpb.FileDescriptorProto
	if err := proto.Unmarshal(fd[0], &file); err != nil {
		log.Fatal(err)
	}
	for _, m := range file.GetService()[0].GetMethod() {
		fmt.Printf("  %s: %s(%s) %s\n", file.GetName(), m.GetName(), m.GetInputType(), m.GetOutputType())
	}
	rc.CloseSend()

	// Остановка: `hs.Shutdown()` переводит все сервисы в
	// NOT_SERVING и запрещает дальнейшие изменения. Балансировщик
	// перестаёт слать новые запросы, а `GracefulStop` дожидается
	// текущих. `GracefulStop` ждёт и открытые стримы — поэтому
	// сначала закрываем Watch.
	hs.Shutdown()
	fmt.Println("Watch: остановка →", next())
	stopWatch()
	srv.GracefulStop()
	fmt.Println("сервер остановлен")
}
