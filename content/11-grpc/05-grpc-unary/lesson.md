---
title: Unary-вызовы gRPC
summary: "Сервис из контракта: Unimplemented…Server и forward-compat, grpc.NewServer с лимитами, grpc.NewClient на loopback, bufconn для тестов, GracefulStop против Stop."
difficulty: 3
minutes: 35
runner:
  race: true
prerequisites:
  - 11-grpc/01-protobuf
  - 11-grpc/03-buf
  - 05-concurrency/18-context
  - 08-web/08-graceful-shutdown
docs:
  - title: "gRPC: Basics tutorial (Go)"
    url: https://grpc.io/docs/languages/go/basics/
  - title: "Пакет google.golang.org/grpc"
    url: https://pkg.go.dev/google.golang.org/grpc
  - title: "Пакет grpc/test/bufconn"
    url: https://pkg.go.dev/google.golang.org/grpc/test/bufconn
---

Unary-вызов — один запрос, один ответ: самый частый вид RPC. Из `rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse)` плагин `protoc-gen-go-grpc` делает три вещи: интерфейс сервера `OrderServiceServer` с методом `CreateOrder(ctx, *CreateOrderRequest) (*CreateOrderResponse, error)`, функцию `RegisterOrderServiceServer` и клиента `OrderServiceClient` с тем же методом, но с `...grpc.CallOption` в конце. Сервер — ваша структура с этими методами; клиент уже готов, писать его не нужно.

В интерфейсе сервера есть скрытый метод `mustEmbedUnimplementedOrderServiceServer()`, и выполнить его можно только одним способом: встроить в свою структуру `UnimplementedOrderServiceServer` — **по значению**. Встроенный тип отвечает на каждый метод контракта статусом `Unimplemented`. Смысл — forward-compat: в контракт добавили `CancelOrder`, код перегенерировали, а старый сервер собирается и отвечает «не реализовано» вместо ошибки компиляции.

Жизненный цикл сервера: `grpc.NewServer(opts...)` → регистрация сервисов → `srv.Serve(lis)` (блокирует, поэтому в горутине) → `GracefulStop()` или `Stop()`. Опции задаются только при создании. Стоит знать хотя бы эти:

- `grpc.MaxRecvMsgSize(n)` — лимит входящего сообщения, по умолчанию 4 МБ. Больше — сервер отвечает `ResourceExhausted`, не разбирая сообщение и не вызывая обработчик. Сервису, который принимает заказы, хватит десятков килобайт: чем меньше лимит, тем дешевле атака огромными запросами. У клиента свой лимит на ответы — `grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(n))`, тоже 4 МБ по умолчанию.
- `grpc.KeepaliveParams(keepalive.ServerParameters{...})` — пинги простаивающих соединений и их ротация: `MaxConnectionIdle` закрывает соединение без вызовов, `MaxConnectionAge` — любое соединение по возрасту (клиенты переподключаются и заново распределяются по репликам за балансировщиком). `grpc.KeepaliveEnforcementPolicy` защищает от слишком частых пингов: по умолчанию клиент не должен пинговать чаще раза в 5 минут, иначе после нескольких (в grpc-go — трёх) слишком частых пингов получит `GOAWAY` с кодом `ENHANCE_YOUR_CALM` и `too_many_pings`. Значения клиента (`grpc.WithKeepaliveParams`) согласуют с этой политикой — подробнее в уроке 13.
- `grpc.ConnectionTimeout`, `grpc.MaxConcurrentStreams` — время на рукопожатие нового соединения и число одновременных вызовов на одном соединении.

Клиент — `grpc.NewClient(target, opts...)`. Он **ничего не подключает**: разбирает адрес, создаёт канал (`*grpc.ClientConn`) и возвращается. Соединение откроется при первом вызове, а после обрыва канал сам переподключится. Канал — дорогой и долгоживущий объект, безопасный для конкурентного использования: один на целевой сервис на всё время жизни процесса. `insecure.NewCredentials()` отключает TLS — допустимо только на loopback и в тестах; TLS и mTLS — в уроке 12. Прежний `grpc.Dial` объявлен устаревшим: у него другой резолвер по умолчанию (`passthrough` вместо `dns`), а с `WithBlock` он ждал подключения — это и стало источником путаницы.

Для тестов есть `google.golang.org/grpc/test/bufconn`: слушатель в памяти. Сервер обслуживает `bufconn.Listen(size)`, клиент подключается через `grpc.WithContextDialer`, вызывающий `lis.DialContext`. Портов, сети и гонок за адреса нет, а код проходит весь путь gRPC — сериализацию, HTTP/2, статусы, — в отличие от прямого вызова метода структуры.

`GracefulStop()` закрывает слушатели, отправляет клиентам `GOAWAY` (новые вызовы идите в другое место), ждёт, пока завершатся **все** активные вызовы, и только потом закрывает соединения. `Stop()` рвёт всё сразу: активные вызовы получают у клиента `Unavailable`, а контексты обработчиков отменяются. На практике их сочетают: `GracefulStop` с ограничением по времени, а по таймауту — `Stop` (см. ловушки).

## Как это в C#

- Контракт тот же, генерация другая. В .NET её делает MSBuild: пакет `Grpc.Tools` и строка `<Protobuf Include="orders.proto" GrpcServices="Both" />` в `.csproj`, код появляется в `obj/` при сборке. В Go `buf generate` кладёт `*.pb.go` рядом с `.proto`, и их коммитят — сборка Go про `.proto` не знает.
- Базовый класс `OrderService.OrderServiceBase` с виртуальными методами, бросающими `RpcException(StatusCode.Unimplemented)`, ↔ встраивание `UnimplementedOrderServiceServer`. Grpc.AspNetCore по умолчанию создаёт экземпляр сервиса на каждый вызов (через `ActivatorUtilities`, зависимости — из scope запроса; если зарегистрировать класс в DI самому, время жизни будет вашим), поэтому состояние держат в singleton. В Go обработчики — методы одной структуры, которую вы создали и зарегистрировали сами; конкурентный доступ к её полям защищаете тоже сами.
- `builder.Services.AddGrpc(o => o.MaxReceiveMessageSize = ...)` и `app.MapGrpcService<T>()` ↔ `grpc.NewServer(grpc.MaxRecvMsgSize(...))` и `RegisterOrderServiceServer`. gRPC в Go — не часть веб-фреймворка: свой сервер со своим HTTP/2, без Kestrel и конвейера middleware.
- `GrpcChannel.ForAddress` ↔ `grpc.NewClient`: оба ленивые и рассчитаны на переиспользование. `deadline:` у вызова ↔ `context.WithTimeout`; ошибка вызова — не исключение `RpcException`, а значение `error`, из которого код достаёт `status.Code(err)`. `WebApplicationFactory` для интеграционных тестов ↔ `bufconn`.

## Ловушки

### `GracefulStop` ждёт вечно

```go
// ЛОВУШКА: открытый стрим или зависший обработчик не завершатся
// сами, и остановка ждёт до SIGKILL.
<-ctx.Done()
srv.GracefulStop()
```

`GracefulStop` не ограничен по времени: он ждёт все вызовы, включая бесконечные стримы и обработчики, застрявшие на внешнем вызове без дедлайна. Ограничьте ожидание сами:

```go
done := make(chan struct{})
go func() { srv.GracefulStop(); close(done) }()
select {
case <-done:
case <-time.After(10 * time.Second):
	srv.Stop() // оборвать оставшиеся вызовы
	<-done
}
```

### Канал на каждый вызов

```go
func getOrder(ctx context.Context, id string) (*Order, error) {
	conn, err := grpc.NewClient(addr, creds) // новое HTTP/2-соединение...
	if err != nil {
		return nil, err
	}
	defer conn.Close() // ...и его закрытие после одного вызова
	resp, err := NewOrderServiceClient(conn).GetOrder(ctx, &GetOrderRequest{Id: id})
	return resp.GetOrder(), err
}
```

Привычка из мира `HttpClient`-в-`using` здесь стоит вдвойне: каждый вызов платит за TCP, TLS и настройку HTTP/2, балансировка и переподключение не успевают сработать, а у сервера копятся соединения. Создайте канал один раз в `main`, передайте клиента зависимостям и закройте канал при остановке.

### bufconn и резолвер `dns`

```go
conn, _ := grpc.NewClient("bufnet", grpc.WithContextDialer(dialer), creds)
_, err := client.GetOrder(ctx, req)
// Unavailable: name resolver error: produced zero addresses
```

У `grpc.NewClient` резолвер по умолчанию — `dns`: он ищет хост `bufnet` и обычно не находит. Хуже, если находит: в некоторых сетях DNS отвечает на любое имя, и тест проходит у вас, а в CI падает. Для bufconn нужна схема `passthrough:///bufnet` — адрес уйдёт в диалер как есть, а диалер всё равно его игнорирует и подключается к слушателю в памяти.

## Задание

Реализуйте сервис заказов в памяти по контракту `task/orders.proto` (код по нему сгенерирован, пакет `task`) и сборку gRPC-сервера. Шаблон уже объявляет:

```go
type OrderServer struct {
	UnimplementedOrderServiceServer
	// состояние — ваше
}

func NewOrderServer() *OrderServer
func NewGRPCServer(s *OrderServer) *grpc.Server
func (s *OrderServer) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error)
func (s *OrderServer) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error)
func (s *OrderServer) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error)
```

Тесты вызывают сервис только через gRPC-клиента по `bufconn`.

- `NewOrderServer()` возвращает готовый к работе сервис без заказов.
- `NewGRPCServer(s)` создаёт `*grpc.Server` с лимитом входящего сообщения **64 КиБ** (`64 << 10` байт) и регистрирует на нём `s`. Запрос больше лимита получает `codes.ResourceExhausted`.
- `CreateOrder`: пустой `customer` или пустой список `items` → `codes.InvalidArgument`. Позиция с пустым `sku`, `quantity <= 0` или `price_cents < 0` → `codes.InvalidArgument` (цена 0 допустима). Иначе создаётся заказ: `id` — `ord-1`, `ord-2`, … по порядку успешных созданий (неудачные запросы номеров не расходуют), `customer` и `items` — из запроса, `total_cents` — сумма `quantity × price_cents` по позициям (переполнение `int64` тесты не проверяют: входные данные заведомо небольшие). Ответ содержит созданный заказ.
- `GetOrder`: пустой `id` → `codes.InvalidArgument`, неизвестный → `codes.NotFound`, иначе заказ.
- `ListOrders`: заказы в порядке создания; если `customer` в запросе не пуст — только заказы этого покупателя (точное совпадение). Если подходящих нет — пустой список без ошибки.
- `CancelOrder` не реализуйте: клиент должен получить `codes.Unimplemented`.
- Методы вызываются конкурентно: при одновременных `CreateOrder` номера уникальны, без пропусков, и `ListOrders` возвращает заказы по возрастанию номера.
