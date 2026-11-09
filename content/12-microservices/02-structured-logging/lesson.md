---
title: Структурные логи в сервисах
summary: "slog в микросервисе: JSON в stdout, уровни по коду ответа, request_id и trace_id из context через свою обёртку slog.Handler, проброс request_id в gRPC-metadata, LogValuer вместо логирования сообщений целиком, логирование в interceptors, сэмплирование."
difficulty: 4
minutes: 35
runner:
  race: true
prerequisites:
  - 06-stdlib/20-slog
  - 09-security/11-secrets-logs-errors
  - 11-grpc/07-metadata-deadlines
  - 11-grpc/09-interceptors
  - 12-microservices/01-architecture
docs:
  - title: "Руководство по написанию slog.Handler"
    url: https://github.com/golang/example/blob/master/slog-handler-guide/README.md
  - title: "Пакет log/slog"
    url: https://pkg.go.dev/log/slog
  - title: "trace.SpanContextFromContext"
    url: https://pkg.go.dev/go.opentelemetry.io/otel/trace#SpanContextFromContext
---

`slog` уже знаком по уроку `06-stdlib/20-slog`. В микросервисе к нему добавляются три требования. **Формат — JSON в stdout**: сервис не пишет файлов и не знает про Loki или Elasticsearch, логи забирает агент контейнерной платформы (Promtail, Fluent Bit, Vector) и разбирает JSON без регулярных выражений. **Каждая запись знает свой запрос**: `request_id` (а после урока `03-tracing` и `trace_id`) позволяет найти все записи одного запроса во всех сервисах одним фильтром. **Логи — не отладочный дамп**: в них нет секретов и персональных данных, и их объём контролируется уровнями и сэмплированием.

Атрибуты запроса живут в `context.Context`. Кто принял запрос — HTTP-middleware или gRPC-interceptor — кладёт `request_id` в контекст, а обёртка над `slog.Handler` достаёт его в `Handle` и дописывает к записи. Функциям посередине не нужно знать о `request_id`: достаточно передавать `ctx` дальше и логировать через `InfoContext`/`ErrorContext`. Между сервисами `request_id` едет в gRPC-metadata (`x-request-id`): клиентский interceptor кладёт его в исходящую metadata, серверный — достаёт и кладёт в контекст.

**Одна запись на вызов** — так называемый access log — пишет interceptor сервера: метод, код ответа, длительность, `request_id`. Уровень — по коду: `OK` — INFO, ошибка клиента (`InvalidArgument`, `NotFound`, `FailedPrecondition`) — WARN, сбой сервера (`Internal`, `Unavailable`, `Unknown`) — ERROR. Пробы health от Kubernetes приходят раз в несколько секунд с каждого узла и забивают логи — их пишут на DEBUG или не пишут вовсе. В примере длительности нет только ради детерминированного вывода, в настоящем interceptor она нужна (`slog.Duration("duration", time.Since(start))`).

**Что можно логировать.** Не логируйте сообщения запроса целиком (`"req", req`): сегодня в `ReserveRequest` только id и позиции, завтра кто-то добавит адрес покупателя — и он молча поедет в логи. Тип-обёртка с `LogValue` (как `reserveLog` в примере) выбирает поля явно. Metadata целиком тоже нельзя: в ней `authorization`. Правила редакции секретов и `ReplaceAttr` для опасных ключей — урок `09-security/11-secrets-logs-errors`.

**Сэмплирование.** При тысячах запросов в секунду INFO-запись на каждый вызов стоит денег на хранение. Обычный приём — обёртка-`Handler`, которая пропускает все WARN и ERROR, а INFO и DEBUG — каждый N-й или по хешу `trace_id` (тогда запрос виден целиком во всех сервисах или не виден вовсе). Ошибки не сэмплируют никогда. Готового сэмплера в `slog` нет — это десяток строк в `Enabled`/`Handle`.

## Как это в C#

- **Scope против контекста.** `ILogger.BeginScope(new { RequestId = id })` в middleware, и JSON-консоль с `IncludeScopes = true` дописывает scope к каждой записи; в Serilog — `LogContext.PushProperty` и `Enrich.FromLogContext()`. Scope живёт в `AsyncLocal`, поэтому «невидимо» течёт через `await`. В Go неявной области нет: атрибуты лежат в `ctx`, а `ctx` передаётся явно, первым параметром.
- **TraceId в логах.** `TraceId`, `SpanId` и `ParentId` текущей `Activity` попадают в scope каждой записи: в хосте это включено по умолчанию, без хоста — через `ActivityTrackingOptions`. В Go их достают из контекста: `trace.SpanContextFromContext(ctx)` — это делает обёртка из задания.
- **Провайдеры и конфигурация.** Уровни по категориям из `appsettings.json`, провайдеры консоли, Seq, Application Insights — всё регистрируется в `builder.Logging`. В Go один `slog.Handler` на процесс, уровень — `HandlerOptions.Level` (или `slog.LevelVar`, чтобы менять на лету, например по сигналу или через админ-эндпоинт), а доставка — забота агента, который читает stdout.
- **Шаблоны сообщений.** `LogInformation("Order {OrderId} created", id)` делает из шаблона и текст, и свойства; `[LoggerMessage]`-генератор убирает аллокации. В slog сообщение — константа, данные — атрибуты; для горячих путей есть `LogAttrs` с типизированными `slog.Attr`.

## Ловушки

### Обёртка потерялась после `With`

```go
type ctxHandler struct{ slog.Handler } // WithAttrs и WithGroup не переопределены

log := slog.New(ctxHandler{jh})
log.With("service", "orders").InfoContext(ctx, "c") // request_id нет
```

Встроенный `WithAttrs` возвращает внутренний обработчик, и `Logger.With` дальше работает уже с ним — обёртка выпала. Ошибка не видна, пока логгер не начнут дополнять через `With`, то есть почти сразу в реальном сервисе. Обёртка обязана переопределять `WithAttrs` и `WithGroup` и возвращать новую обёртку.

### `request_id` уезжает внутрь группы

```go
log.WithGroup("db").InfoContext(ctx, "query", "rows", 3)
// {"msg":"query","db":{"rows":3,"request_id":"r-1"}}
```

Обёртка из примера добавляет атрибуты из контекста как атрибуты вызова, а они попадают в текущую группу. Поиск по `request_id` эту запись не найдёт: поле называется `db.request_id`. Чтобы атрибуты запроса всегда были на верхнем уровне, обёртка должна сама помнить группы и собирать запись в `Handle` — это и есть задание.

### `Info` вместо `InfoContext`

```go
func (s *Service) Reserve(ctx context.Context, ...) error {
	s.log.Info("reserve") // контекст не передан — request_id и trace_id потеряны
```

`Info`, `Warn`, `Error` передают в `Handle` `context.Background()`. В коде, где есть `ctx`, используют `*Context`-методы; `sloglint` из golangci-lint умеет это проверять (`context: scope`).

## Задание

Напишите обёртку над `slog.Handler`, которая добавляет к каждой записи атрибуты запроса из контекста и идентификаторы трассы OpenTelemetry — всегда на верхнем уровне записи.

```go
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context
func AttrsFrom(ctx context.Context) []slog.Attr
func NewContextHandler(next slog.Handler) slog.Handler
```

Требования:

- `WithAttrs` возвращает дочерний контекст: атрибуты родителя, затем новые в порядке аргументов. Если атрибут с таким ключом уже есть — у родителя или раньше в том же вызове, — новое значение заменяет старое на его прежнем месте (выигрывает последнее). Родительский контекст и контексты, полученные из него другими вызовами `WithAttrs`, не меняются;
- `AttrsFrom` возвращает атрибуты контекста (пустой срез или `nil`, если их нет); изменение возвращённого среза не влияет на контекст;
- обработчик из `NewContextHandler` в `Handle` передаёт в `next` запись с теми же временем, уровнем, сообщением и `PC`, а атрибуты в ней идут в таком порядке: сначала атрибуты из `AttrsFrom(ctx)`; затем, если `trace.SpanContextFromContext(ctx)` валиден (`IsValid()`), — `trace_id` и `span_id` строками (`TraceID().String()`, `SpanID().String()`); затем атрибуты из `logger.With(...)` и группы из `logger.WithGroup(...)` в порядке вызовов, а атрибуты самого вызова — внутри самой вложенной группы. Например, `log.With("service", "orders").WithGroup("db").InfoContext(ctx, "q", "rows", 1)` с `request_id` в контексте даёт `{"level":"INFO","msg":"q","request_id":"r-1","service":"orders","db":{"rows":1}}` (время убрано);
- группа, в которую не попало ни одного атрибута, в выводе не появляется — как у самого `JSONHandler`;
- без атрибутов в контексте и без валидного `SpanContext` запись выглядит так же, как без обёртки: байт в байт как у `next` с той же цепочкой `With`/`WithGroup`;
- `WithGroup("")` возвращает обработчик, который ведёт себя как исходный;
- уровень решает `next`: `Enabled` отвечает так же, как `next.Enabled`;
- `With`/`WithGroup` у производных логгеров не влияют друг на друга и на исходный логгер; обработчик безопасен для одновременного использования из многих горутин.
