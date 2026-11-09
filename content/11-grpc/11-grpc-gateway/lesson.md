---
title: grpc-gateway
summary: "REST из того же контракта: google.api.http (путь, query, body: \"*\", additional_bindings), runtime.ServeMux и его опции, HandlerFromEndpoint против HandlerServer, коды gRPC → HTTP, свой обработчик ошибок, таймауты HTTP-сервера, OpenAPI; Connect — обзорно."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 11-grpc/08-status-errors
  - 11-grpc/09-interceptors
  - 08-web/05-json-api
docs:
  - title: "grpc-gateway: runtime"
    url: https://pkg.go.dev/github.com/grpc-ecosystem/grpc-gateway/v2/runtime
  - title: "google/api/http.proto — правила аннотаций"
    url: https://github.com/googleapis/googleapis/blob/master/google/api/http.proto
  - title: "Документация grpc-gateway"
    url: https://grpc-ecosystem.github.io/grpc-gateway/
---

Внутри системы сервисы говорят по gRPC, а браузеру, мобильному клиенту и партнёрам нужен REST с JSON. Писать второй слой HTTP-обработчиков руками — значит держать два контракта, которые неизбежно разойдутся. grpc-gateway генерирует REST-слой из того же `.proto`. Аннотация `google.api.http` у метода задаёт HTTP-метод, шаблон пути и тело. `protoc-gen-grpc-gateway` по ней создаёт `orders.pb.gw.go`, где обработчики разбирают запрос в protobuf-сообщение, вызывают gRPC-метод и печатают ответ через `protojson`. В песочнице этот файл генерируется вместе с остальными, как только в контракте появляется `google.api.http`.

**Правила маршрутов** (`google/api/http.proto`):

- `get: "/v1/orders/{id}"` — переменная пути заполняет поле `id`. Можно и вложенное поле: `{order.id}`.
- Поля, которых нет в пути и в теле, берутся из query: `?status=ORDER_STATUS_PAID&customer_id=c1`. Enum передаётся по имени или числом, повторяющиеся поля — повтором параметра.
- `post: "/v1/orders"` с `body: "*"` — JSON-тело заполняет всё сообщение. `body: "order"` — только поле `order`, остальное приходит из пути и query. У GET тела нет: генератор откажется собирать такой маршрут.
- `additional_bindings` вешает на метод ещё маршруты: `/v1/orders?customer_id=c1` и `/v1/customers/c1/orders` приходят в один и тот же `ListOrders`.
- `post: "/v1/orders/{id}:cancel"` — «пользовательский метод» после двоеточия, стиль Google API для действий, которые не ложатся на CRUD.

**Сборка в `main`.** `runtime.NewServeMux(опции...)` — роутер gateway, обычный `http.Handler`. Обработчики регистрируют одним из двух способов:

- `RegisterOrderServiceHandlerFromEndpoint(ctx, mux, addr, dialOpts)` (или `…Handler(ctx, mux, conn)`) — gateway становится gRPC-**клиентом**. Вызов идёт через транспорт, и работают interceptors сервера: auth, recovery, логи, метрики. Gateway можно вынести в отдельный процесс.
- `RegisterOrderServiceHandlerServer(ctx, mux, srv)` — прямой вызов методов в том же процессе: быстрее, но **без interceptors** и без стримовых методов.

Основные опции mux:

| Опция | Зачем |
| --- | --- |
| `WithIncomingHeaderMatcher` | Какие HTTP-заголовки станут metadata. По умолчанию проходят `Authorization`, `Grpc-Metadata-*` и стандартные заголовки (с префиксом `grpcgateway-`), а `X-Request-Id` теряется |
| `WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{...})` | Вид JSON: `UseProtoNames` (`customer_id` вместо `customerId`), `EmitUnpopulated` (нулевые значения не пропадают), `DiscardUnknown` на входе |
| `WithErrorHandler` | Формат ошибок: по умолчанию тело — `google.rpc.Status` (`{"code":5,"message":…,"details":[]}`) |
| `WithRoutingErrorHandler` | «Нет такого пути» и «не тот метод» — до вызова gRPC |
| `WithForwardResponseOption` | Дописать заголовки ответа или поменять HTTP-статус (например, 201 для создания) |

HTTP-статус ответа gateway берёт из кода gRPC (`runtime.HTTPStatusFromCode`): `InvalidArgument`, `FailedPrecondition` и `OutOfRange` → 400, `Unauthenticated` → 401, `PermissionDenied` → 403, `NotFound` → 404, `AlreadyExists` и `Aborted` → 409, `ResourceExhausted` → 429, `Canceled` → 499, `Unimplemented` → 501, `Unavailable` → 503, `DeadlineExceeded` → 504, `Internal`, `Unknown` и `DataLoss` → 500. Успех — всегда 200.

Перед mux стоит обычный `http.Server` из модуля 8 со всеми его обязанностями: `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `http.MaxBytesHandler` для тела. Gateway их не добавляет. Помните, что `MaxBytesReader` срабатывает, только когда кто-то читает тело дальше лимита, а декодер gateway читает лишь первое JSON-значение: хвост за ним никто не читает, и лимит его не видит. Если нужен лимит на всё тело, читайте его целиком до mux. Учтите ещё, что клиент может прислать заголовок `Grpc-Timeout`, и gateway сделает из него дедлайн вызова.

Пример поднимает gRPC-сервер и gateway на loopback, регистрирует его через `…FromEndpoint` и ходит в REST обычным `http.Client`. В выводе видно, как `X-Request-Id` дошёл до interceptor сервера, как `int64` превратился в JSON-строку, как сработали оба маршрута `ListOrders` и как ошибки gRPC стали HTTP-статусами.

**OpenAPI.** Плагин `protoc-gen-openapiv2` из того же репозитория генерирует по тем же аннотациям `orders.swagger.json`, из которого делают документацию и клиентов. В песочнице его нет: buf генерирует только Go-код. В реальном проекте это ещё одна строка `plugins` в `buf.gen.yaml`.

**Connect** (`connectrpc.com/connect`) решает ту же задачу иначе. Это своя реализация gRPC поверх `net/http`. Один обработчик понимает три протокола: gRPC, gRPC-Web и собственный Connect — простой POST с JSON или protobuf на путь `/<пакет>.<Сервис>/<Метод>`, который можно вызвать хоть `curl`. Прокси и аннотации путей не нужны, зато и «красивых» REST-путей вроде `/v1/orders/{id}` нет: это RPC по HTTP, а не ресурсный REST. Если внешним клиентам нужен именно REST по контракту, курс использует grpc-gateway. Если нужно вызывать сервис из браузера и удобно отлаживать, стоит посмотреть на Connect.

## Как это в C#

- **JSON transcoding вместо прокси.** `Microsoft.AspNetCore.Grpc.JsonTranscoding` читает те же аннотации `google.api.http`: `AddGrpc().AddJsonTranscoding()`, и `MapGrpcService<T>()` отдаёт и gRPC, и REST на одном порту. Отдельного сгенерированного файла маршрутов нет, всё строится при старте по дескрипторам. В Go gateway — сгенерированный код и отдельный `http.Handler`, который надо собрать в `main`.
- **Конвейер.** Transcoding в ASP.NET Core проходит через обычный конвейер gRPC-сервиса, включая interceptors и `[Authorize]`. В Go так ведёт себя только `…HandlerFromEndpoint`. Режим `…HandlerServer` обходит interceptors.
- **Ошибки.** В .NET `RpcException` превращается в HTTP-статус по той же таблице, тело — JSON `google.rpc.Status`. Свой формат там задают через middleware или фильтры, в Go — `WithErrorHandler`.
- **OpenAPI.** `Microsoft.AspNetCore.Grpc.Swagger` подключает transcoding-маршруты к Swashbuckle. В Go это отдельный плагин генерации `protoc-gen-openapiv2`.

## Ловушки

### HandlerServer обходит interceptors

```go
grpcSrv := grpc.NewServer(grpc.ChainUnaryInterceptor(recovery, auth))
RegisterOrderServiceServer(grpcSrv, srv)
RegisterOrderServiceHandlerServer(ctx, mux, srv) // REST идёт мимо auth и recovery
```

`…HandlerServer` вызывает методы `srv` напрямую. Цепочку выполняет `grpc.Server`, а он в этом вызове не участвует. REST-клиент без токена дойдёт до обработчика, а паника уронит процесс. Либо регистрируйте через `…FromEndpoint` (gateway ходит в gRPC-сервер по loopback или bufconn), либо повторите проверки HTTP-middleware вокруг mux.

### Внутренние детали в теле ошибки

```go
return nil, status.Errorf(codes.Internal, "query failed: %v", err) // УЯЗВИМО: текст ошибки БД уйдёт в JSON
```

Стандартный обработчик gateway отдаёт `message` как есть. То, что раньше видел только другой сервис по gRPC, теперь читает любой браузер. Для `Internal` и `Unknown` свой `WithErrorHandler` должен подменять текст общим (`internal error`), а подробности писать в лог с request id. Это и есть задание урока.

### 405 превращается в 501

```go
// DELETE /v1/orders/1, а маршрута DELETE нет
// стандартный ответ: 501 {"code":12,"message":"Method Not Allowed"}
```

Роутер gateway понимает, что путь есть, а метода нет, и передаёт обработчику ошибок `codes.Unimplemented`. Тот по таблице кодов отвечает 501. Клиенты и балансировщики считают 5xx сбоем сервера и могут повторять запрос. Если важен честный 405, задайте `WithRoutingErrorHandler` и передайте статус как есть через `*runtime.HTTPStatusError`.

## Задание

`task.go` — сервис заказов в памяти (`GetOrder` и `CancelOrder` готовы) и заготовки REST-слоя по контракту `task/orders.proto`. Допишите три функции:

```go
func (s *Server) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error)
func ErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error)
func NewGateway(ctx context.Context, srv OrderServiceServer) (http.Handler, error)
```

- **`ListOrders`** обслуживает оба маршрута — `GET /v1/orders` и `GET /v1/customers/{customer_id}/orders`. Он возвращает заказы с `customer_id`, равным запрошенному (пустой — любой клиент), и статусом, равным запрошенному (`ORDER_STATUS_UNSPECIFIED` — любой), по возрастанию `id`. Если таких нет — пустой список.
- **`NewGateway`** регистрирует `srv` в `runtime.ServeMux` в том же процессе (`RegisterOrderServiceHandlerServer`) и возвращает обработчик:
  - JSON ответов — с именами полей как в `.proto` (`customer_id`) и без пропуска нулевых значений (`"total_cents": "0"`, `"orders": []`). Неизвестные поля во входящем JSON — ошибка 400;
  - заголовок `X-Request-Id` доходит до сервера как metadata `x-request-id`;
  - все ошибки, включая ошибки маршрутизации, отдаются через `ErrorHandler`. Нет такого пути — статус 404, код `NOT_FOUND`, `message` — `Not Found`. Путь есть, а метода нет — статус **405**, код `UNIMPLEMENTED`, `message` — `Method Not Allowed`. Неверный запрос (id не число, битый JSON) — 400 `INVALID_ARGUMENT`; если об этом сообщает сам роутер gateway, `message` — `Bad Request`;
  - тело запроса ограничено 65536 байтами, считая **все** байты тела, включая пробелы после JSON (декодер gateway читает только первое JSON-значение, поэтому одного `http.MaxBytesReader` мало). Тело больше — 400 `INVALID_ARGUMENT` (текст `message` — любой) ещё до маршрутизации, то есть на любом пути, и сервис не вызывается.
- **`ErrorHandler`** отвечает с `Content-Type: application/json` и телом

  ```json
  {"error": {"code": "NOT_FOUND", "message": "order 42 not found", "request_id": "r-1"}}
  ```

  - `code` — имя кода gRPC в верхнем регистре, как в `google.rpc.Code` (`NOT_FOUND`, `FAILED_PRECONDITION`, `UNKNOWN` для ошибок без статуса).
  - `message` — текст статуса. Для `Internal`, `Unknown` и `DataLoss` вместо него — `internal error`.
  - `request_id` — заголовок `X-Request-Id` запроса (`""`, если его нет).
  - HTTP-статус — `runtime.HTTPStatusFromCode(код)`. Если ошибка — `*runtime.HTTPStatusError` (в том числе обёрнутая), статус берётся из её `HTTPStatus`, а код и текст — из её `Err`.
  - Не проверяется и в тестах не встречается: коды вне `google.rpc.Code`, `HTTPStatusError` с `Err == nil`, перенос заголовков из metadata ответа (`Grpc-Metadata-*`) в ответ с ошибкой.
