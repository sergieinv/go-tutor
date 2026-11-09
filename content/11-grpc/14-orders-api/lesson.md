---
title: "Итог модуля: API заказов"
summary: "Контракт orders.v1 с protovalidate и google.api.http, gRPC-сервер с interceptors валидации и журнала, ошибки со статусами и деталями, REST через grpc-gateway поверх настоящего gRPC-соединения, стрим и пагинация, идемпотентность CreateOrder по ключу из metadata."
difficulty: 5
minutes: 60
runner:
  race: true
prerequisites:
  - 11-grpc/04-protovalidate
  - 11-grpc/08-status-errors
  - 11-grpc/09-interceptors
  - 11-grpc/11-grpc-gateway
  - 11-grpc/12-grpc-security
docs:
  - title: "grpc-gateway: runtime"
    url: https://pkg.go.dev/github.com/grpc-ecosystem/grpc-gateway/v2/runtime
  - title: "protovalidate-go"
    url: https://pkg.go.dev/buf.build/go/protovalidate
  - title: "AIP-158: Pagination"
    url: https://google.aip.dev/158
---

Модуль собирается в один сервис. Контракт `orders.proto` — единственный источник истины: из него `buf generate` делает типы сообщений, gRPC-сервер и клиент, REST-маршруты gateway, а правила `buf.validate` проверяют запросы на сервере. Всё остальное — обычный Go в `main`, без фреймворка:

- **сервер** — `grpc.NewServer` с явными опциями: лимит сообщения, цепочки `ChainUnaryInterceptor` и `ChainStreamInterceptor`;
- **interceptors** — сквозные задачи один раз для всех методов: журнал (снаружи, чтобы видеть и отказы), валидация по контракту (`InvalidArgument` + `BadRequest` с путями полей), в реальном сервисе ещё recovery, аутентификация из прошлого урока, метрики и трассировка (модуль 12);
- **обработчики** получают уже проверенный запрос и говорят доменными ошибками (`errNotFound`, `errCancelled`), а перевод в коды и детали (`NotFound`, `FailedPrecondition` + `PreconditionFailure`) живёт в одной функции `toStatus`, всё неизвестное — `Internal` без подробностей;
- **данные** под мьютексом, наружу уходят копии (`proto.CloneOf`): ссылка на сообщение из хранилища в ответе — гонка с конкурентной отменой;
- **REST** — `runtime.NewServeMux` и `RegisterOrderServiceHandler(ctx, mux, conn)` поверх *gRPC-соединения* с этим же сервером. Так REST проходит через те же interceptors. HTTP-сервер — с таймаутами и `http.MaxBytesHandler`.

Пример делает несколько вызовов по gRPC и по REST и печатает журнал сервера. Видно, что REST-запросы тоже проходят через interceptors (строки `[grpc]`), как gateway переводит коды в HTTP-статусы (`NotFound` → 404, `FailedPrecondition` → 400) и отдаёт тело ошибки `{"code":…,"message":…,"details":[…]}`, а серверный стрим `ListOrders` превращает в JSON по строке на сообщение (`{"result":…}`). JSON примера пропущен через `json.Compact`: protojson нарочно вставляет случайные пробелы, чтобы никто не сравнивал его вывод побайтно.

**Стрим или пагинация.** В примере `ListOrders` — серверный стрим: удобно для gRPC-клиентов и больших выборок, но REST-клиенту приходится читать построчный JSON, а повторить оборвавшийся стрим с середины нельзя. В задании — пагинация по AIP-158: `page_size` (0 — размер по умолчанию, сверху лимит), непрозрачный `page_token`, пустой `next_page_token` на последней странице. Для публичного REST API обычно выбирают её.

**Идемпотентность.** Сеть рвётся после того, как сервер создал заказ, но до того, как клиент получил ответ. Клиент повторяет — и без защиты получается два заказа. Решение — **ключ идемпотентности**: клиент генерирует его один раз на операцию и шлёт с каждым повтором (в gRPC — metadata `idempotency-key`, в REST — заголовок `Idempotency-Key`). Сервер помнит «ключ → запрос → результат». Тот же ключ и тот же запрос — вернуть прежний результат. Тот же ключ и другой запрос — ошибка: клиент переиспользовал ключ. Проверка и запись ключа должны быть атомарны с созданием, иначе два одновременных повтора создадут два заказа. В настоящем сервисе ключи хранят в базе с уникальным индексом и сроком жизни (сутки), в той же транзакции, что и заказ (урок `10-services-db/10-outbox` про ту же атомарность). Встроенные повторы gRPC из урока `13-grpc-client` безопасны для `CreateOrder` только с таким ключом.

**Заголовки через gateway.** По умолчанию `runtime.NewServeMux` пропускает в metadata только заголовки с префиксом `Grpc-Metadata-` и стандартные HTTP-заголовки с префиксом `grpcgateway-` (`Authorization` вдобавок приходит как `authorization`). Свои заголовки вроде `Idempotency-Key` или `X-Request-Id` подключают через `runtime.WithIncomingHeaderMatcher`. Пропускать всё подряд не стоит: клиент тогда сможет подложить в metadata служебные ключи, которым доверяют ваши interceptors.

Полноценная версия сервиса — `labs/orders/` (gRPC и REST поверх PostgreSQL, контракты в `labs/api/`); в модуле 12 к нему добавятся склад, Kafka и наблюдаемость.

## Как это в C#

- **Генерация.** `<Protobuf Include="orders.proto" GrpcServices="Server" />` в `.csproj`, и Grpc.Tools генерирует код при каждой сборке. В Go сгенерированный код — обычные файлы в репозитории, их обновляет `buf generate` (и проверяет CI).
- **REST.** `AddJsonTranscoding()` — аналог grpc-gateway по тем же `google.api.http`, но внутри конвейера ASP.NET Core: interceptors и авторизация работают сами, а HTTP-заголовки доходят до `ServerCallContext.RequestHeaders` без настройки. В Go gateway — отдельный `http.Handler`, который ходит в сервер по gRPC, и путь заголовков вы задаёте сами.
- **Валидация.** Правила `buf.validate` C#-генератор не применяет: их проверяют пакетом protovalidate-net или дублируют в FluentValidation. В Go `protovalidate.New()` читает правила прямо из дескрипторов сгенерированных типов — второй копии правил нет.
- **Ошибки.** `RpcException` + `Grpc.StatusProto` (`Google.Rpc.Status` с `BadRequest`, `.ToRpcException()`) — то же, что `status.New(...).WithDetails(...)`.

## Ловушки

### `Register…HandlerServer` обходит interceptors

```go
// УЯЗВИМО: REST вызывает методы svc напрямую — без auth и валидации,
// которые висят на grpc.Server.
gs := grpc.NewServer(grpc.ChainUnaryInterceptor(auth, validate))
RegisterOrderServiceServer(gs, svc)
mux := runtime.NewServeMux()
RegisterOrderServiceHandlerServer(ctx, mux, svc)
```

Режим «в процессе» удобен (не нужно соединение), но для gateway сервер — просто интерфейс `OrderServiceServer`: interceptors, лимиты сообщений и стримы gRPC не участвуют, а стриминговые методы вовсе возвращают ошибку. Безопаснее `RegisterOrderServiceHandler(ctx, mux, conn)` через gRPC-соединение (loopback или `bufconn`) — путь запроса один для обоих протоколов.

### Ответ ссылается на данные хранилища

```go
func (s *server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &GetOrderResponse{Order: s.byID[req.GetId()]}, nil // ссылка, не копия
}
```

Мьютекс отпускается при выходе из обработчика, а сериализация ответа происходит позже — уже без блокировки. Конкурентный `CancelOrder` в это время меняет тот же `*Order`, и `-race` это поймает. Отдавайте копии (`proto.CloneOf(o)`), а запросы, которые сохраняете, тоже клонируйте: сообщение запроса принадлежит вызову.

### Ключ идемпотентности без сравнения запроса

```go
if id, ok := s.keys[key]; ok {
	return s.byID[id], nil // а если тело другое?
}
```

Клиент с ошибкой переиспользует ключ для нового заказа и получает старый — изменения молча теряются. Храните вместе с ключом сам запрос (или его хеш от детерминированной сериализации) и сравнивайте `proto.Equal`; несовпадение — `AlreadyExists` (в REST 409).

## Задание

Реализуйте сервер по контракту `task/orders.proto` (он показан над тестами) и REST поверх него. Сгенерированный код (`orders.pb.go`, `orders_grpc.pb.go`, `orders.pb.gw.go`) лежит рядом, пакет — `task`.

```go
func NewService(now func() time.Time) *Service // Service реализует OrderServiceServer
func NewGRPCServer(svc *Service) *grpc.Server
func NewGateway(ctx context.Context, conn *grpc.ClientConn) (http.Handler, error)
```

Тесты собирают сервер так: `NewGRPCServer(NewService(now))` на `bufconn`, gRPC-соединение к нему и `NewGateway(ctx, conn)` на `httptest`-сервере. Вызовы идут по gRPC и по REST; методы `Service` напрямую не вызываются. Сервис используют конкурентно.

**Валидация.** Каждый запрос проверяется по правилам `buf.validate` из контракта до выполнения метода. Нарушение — `codes.InvalidArgument` с деталью `errdetails.BadRequest`, где на каждое нарушение есть `FieldViolation` с непустым `description` и `field` — путём поля как у `protovalidate.FieldPathString` (`customer_id`, `items`, `items[0].quantity`, `page_size`, `id`). Невалидный запрос ничего не меняет.

**CreateOrder.** Создаёт заказ: `id` — `ord-1`, `ord-2`, … по порядку успешных созданий (отказы номера не расходуют), `customer_id` и `items` из запроса, `total_cents` = Σ `quantity × price_cents`, `status` = `ORDER_STATUS_CREATED`, `create_time` = `now()`.

- Ключ идемпотентности — metadata `idempotency-key`. Нет ключа — каждый вызов создаёт новый заказ. Ключ есть — должен быть ровно одно значение длиной от 1 до 64 байт, иначе `codes.InvalidArgument`. Через REST ключ, переданный и в `Idempotency-Key`, и в `Grpc-Metadata-Idempotency-Key`, даёт два значения — тоже `InvalidArgument`. Если неверны и ключ, и тело запроса, порядок проверок не важен: код в обоих случаях `InvalidArgument`.
- Первый вызов с ключом создаёт заказ. Повтор с тем же ключом и равным запросом (`proto.Equal`) возвращает заказ с тем же `id` в его текущем состоянии (если его уже отменили — со статусом `ORDER_STATUS_CANCELLED`), новый не создаётся — в том числе когда повторы идут одновременно. Тот же ключ с другим запросом — `codes.AlreadyExists`. Ключи общие для всех клиентов. Невалидный запрос с уже использованным ключом — `InvalidArgument`.

**GetOrder** возвращает заказ в текущем состоянии; нет такого — `codes.NotFound`.

**ListOrders** возвращает заказы клиента `customer_id` в порядке создания:

- `page_size` 0 — 10 заказов на странице, иначе `page_size`;
- `next_page_token` непустой, только если после этой страницы у клиента есть ещё заказы; пустой `page_token` — первая страница, `next_page_token` из прошлого ответа (для того же клиента) — следующая; заказы, созданные между запросами страниц, попадают в последующие страницы;
- строка, которую сервер не выдавал как токен (например, `not-a-token`), — `codes.InvalidArgument`; клиент без заказов — пустой список и пустой токен. Токен, выданный для другого клиента, тесты не передают — поведение на нём на ваше усмотрение. Как устроен токен, тоже решаете вы: закодированное смещение, курсор по `id` или запись на сервере — подходит любой вариант.

**CancelOrder** ставит `ORDER_STATUS_CANCELLED` и возвращает заказ; нет заказа — `codes.NotFound`, уже отменён — `codes.FailedPrecondition`.

**REST.** `NewGateway` регистрирует маршруты контракта (`POST /v1/orders`, `GET /v1/orders/{id}`, `GET /v1/customers/{customer_id}/orders?page_size=…&page_token=…`, `POST /v1/orders/{id}:cancel`) поверх `conn` и передаёт HTTP-заголовок `Idempotency-Key` в metadata `idempotency-key`. Ошибки — стандартным обработчиком gateway: HTTP-статус по коду gRPC (400, 404, 409) и JSON-тело статуса с `code` и деталями.
