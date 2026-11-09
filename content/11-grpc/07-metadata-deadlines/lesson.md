---
title: Metadata и дедлайны
summary: "Входящая и исходящая metadata, -bin ключи, заголовки и трейлеры ответа; дедлайн от context.WithTimeout через grpc-timeout до ctx.Deadline() сервера, проверка остатка, DeadlineExceeded против Canceled."
difficulty: 4
minutes: 35
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 05-concurrency/18-context
  - 08-web/07-request-context
docs:
  - title: "gRPC: Metadata"
    url: https://grpc.io/docs/guides/metadata/
  - title: "gRPC: Deadlines"
    url: https://grpc.io/docs/guides/deadlines/
  - title: "Пакет grpc/metadata"
    url: https://pkg.go.dev/google.golang.org/grpc/metadata
---

Metadata — это заголовки HTTP/2 вызова: пары «ключ → список строк» (`metadata.MD` — `map[string][]string`). В них ходит всё, что не входит в контракт сообщения: id запроса, токен (`authorization`), язык, данные трассировки. Ключи — в нижнем регистре (`metadata.Pairs` и `Get` приводят их сами), префикс `grpc-` зарезервирован. Ключ с суффиксом `-bin` несёт произвольные байты: на проводе они в base64, но в Go вы кладёте и получаете исходные байты.

В контексте metadata две, и они не смешиваются. **Исходящую** кладёт клиент: `metadata.AppendToOutgoingContext(ctx, k, v, ...)` дописывает пары к уже имеющимся, `metadata.NewOutgoingContext(ctx, md)` заменяет целиком. **Входящую** сервер читает из контекста обработчика: `metadata.FromIncomingContext(ctx)`. Поэтому сервер, вызывающий следующий сервис с тем же `ctx`, metadata не пересылает — и это правильно: токен клиента не должен утечь соседу. Нужные ключи (request id) копируют явно.

У ответа своя metadata: **заголовки** уходят перед первым сообщением (`grpc.SetHeader` копит, `grpc.SendHeader` отправляет сразу), **трейлеры** — после последнего, вместе со статусом (`grpc.SetTrailer`). Клиент получает их опциями вызова `grpc.Header(&md)` и `grpc.Trailer(&md)`, у стрима — методами `Header()` и `Trailer()`. В grpc-go 1.84 `MD.String()` (а значит, `%v` в логах) печатает только стандартные протокольные заголовки, а прикладные ключи заменяет на `<N redacted>` — защита от случайно залогированного токена; это экспериментальное поведение, полагаться на формат не стоит.

Дедлайн в gRPC — часть протокола. Клиент задаёт его контекстом (`context.WithTimeout`), grpc-go переводит остаток в заголовок `grpc-timeout` (число до 8 цифр и единица: `1999988u` — микросекунды, округление вверх), сервер ставит такой же дедлайн контексту обработчика: `ctx.Deadline()` вернёт его, а `ctx.Done()` закроется, когда время выйдет. Передайте этот `ctx` в следующий gRPC-вызов или в `QueryContext` — бюджет перейдёт дальше сам и будет уменьшаться на каждом шаге (подробно — в уроке 12-microservices/07). Без дедлайна вызов может ждать вечно, поэтому в gRPC он нужен у каждого вызова, а сервер может сам ограничить вызовы без дедлайна.

Два кода для конца ожидания: `codes.DeadlineExceeded` — время вышло, `codes.Canceled` — клиент отменил сам (закрыл вкладку, отменил запрос выше по цепочке). Получив их, обработчик бросает работу: `ctx.Err()` подскажет причину, а `status.FromContextError(ctx.Err())` превратит её в нужный статус. И раз время известно заранее, дорогую работу стоит начинать, только если остаток её вмещает: отказ за микросекунду дешевле, чем полработы, которую никто не дождётся.

## Как это в C#

- `Grpc.Core.Metadata` ↔ `metadata.MD`, `ServerCallContext.RequestHeaders` ↔ `metadata.FromIncomingContext`. Ответные `WriteResponseHeadersAsync` и `ResponseTrailers` ↔ `grpc.SetHeader` / `grpc.SetTrailer`, на клиенте `call.ResponseHeadersAsync` и `call.GetTrailers()` ↔ `grpc.Header` / `grpc.Trailer`.
- Дедлайн в .NET — абсолютное время `deadline:` у вызова и `ServerCallContext.Deadline` (без дедлайна — `DateTime.MaxValue`). В Go он часть `context.Context` наряду с отменой и значениями, поэтому один `ctx` несёт всё сразу.
- `EnableCallContextPropagation()` у клиента из `Grpc.Net.ClientFactory` переносит дедлайн и отмену входящего вызова в исходящие. В Go это не опция, а следствие: вы передаёте дальше тот же `ctx`. Metadata не переносит ни то, ни другое.
- `ServerCallContext.CancellationToken` срабатывает и по дедлайну, и по отмене ↔ `ctx.Done()`; различить причину в Go — `ctx.Err()`: `context.DeadlineExceeded` или `context.Canceled`.

## Ловушки

### `context.Background()` в обработчике

```go
func (s *server) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error) {
	// ЛОВУШКА: дедлайн и отмена клиента обрываются здесь.
	res, err := s.stock.Reserve(context.Background(), &ReserveRequest{Sku: req.GetSku()})
	...
}
```

Клиент давно получил `DeadlineExceeded`, а Stock всё ещё резервирует товар, держит соединение и блокировки. Каждый ушедший клиент оставляет хвост работы — при перегрузке это и есть лавина. Всегда стройте контексты от входящего `ctx`; для работы, которая должна пережить ответ, — `context.WithoutCancel(ctx)` со своим таймаутом.

### Проброс всей входящей metadata

```go
// УЯЗВИМО: токен клиента, его cookie и всё прочее уходят в чужой сервис.
md, _ := metadata.FromIncomingContext(ctx)
out := metadata.NewOutgoingContext(ctx, md)
```

Соседний сервис получит `authorization` клиента и сможет действовать от его имени, а заодно — служебные заголовки вроде `:authority` и `content-type`, которые не должны ходить дальше. Пробрасывайте по списку: `metadata.AppendToOutgoingContext(ctx, "x-request-id", id)`.

### Свой таймаут длиннее входящего

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second) // «дадим складу время»
defer cancel()
```

Дочерний контекст не может жить дольше родителя: если клиент дал 2 секунды, `WithTimeout` на 30 секунд ничего не продлит, а читатель кода решит, что у склада полминуты. Свой таймаут имеет смысл только как *ограничение сверху* — меньше входящего или когда входящего дедлайна нет.

## Задание

Реализуйте обработчик `Checkout` по контракту `task/checkout.proto` (код сгенерирован, пакет `task`). Он принимает заказ и резервирует товар вызовом `StockService.Reserve` — клиент Stock передаётся в конструктор (в тестах — фейк или настоящий сервер по bufconn).

```go
type Config struct {
	MinBudget      time.Duration    // меньше этого остатка — отказ без вызова Stock
	DefaultTimeout time.Duration    // если у входящего вызова нет дедлайна
	Now            func() time.Time // часы
	NewID          func() string    // id запроса, если клиент его не прислал
}

func NewCheckoutServer(stock StockServiceClient, cfg Config) *CheckoutServer
func (s *CheckoutServer) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error)
```

Все поля `Config` в тестах заданы. Шаги `Checkout` по порядку:

1. Id запроса — первое значение ключа `x-request-id` входящей metadata. Если ключа нет или значение пустое — `cfg.NewID()`.
2. Заголовок ответа `x-request-id` с этим id — `grpc.SetHeader` — ставится при любом исходе, включая ошибки. Ошибку `SetHeader` игнорируйте.
3. Пустой `order_id`, пустой `sku` или `quantity <= 0` → `codes.InvalidArgument`, Stock не вызывается.
4. Если у входящего контекста есть дедлайн и остаток `дедлайн − cfg.Now()` меньше `cfg.MinBudget` → `codes.DeadlineExceeded`, Stock не вызывается (остаток, равный `MinBudget`, допустим). Если дедлайна нет — вызов Stock получает контекст `context.WithTimeout(ctx, cfg.DefaultTimeout)`: таймер контекста идёт по настоящим часам, поэтому дедлайн считается от настоящего `time.Now()`, а не от `cfg.Now()`. `cfg.Now` нужен только для расчёта остатка в этом шаге.
5. `Reserve` вызывается с контекстом, производным от входящего (дедлайн и отмена клиента доходят до Stock), с запросом `order_id`, `sku`, `quantity` из входящего и с исходящей metadata, в которой ровно один ключ — `x-request-id` с id запроса. Другие ключи входящей metadata (`authorization` и т.п.) не пересылаются.
6. Ошибка `Reserve` возвращается как есть (её статус и сообщение доходят до клиента без изменений). Иначе ответ — `order_id` из запроса и `reservation_id` из ответа Stock.
