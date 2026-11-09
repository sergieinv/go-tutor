---
title: Распределённая трассировка
summary: "OpenTelemetry в Go: TracerProvider и ресурс, спаны, атрибуты, события и статус ошибки, проброс контекста через gRPC (otelgrpc) и через заголовки Kafka (TraceContext и свой TextMapCarrier), экспорт в памяти и в stdout, OTLP → Jaeger, сэмплирование."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 12-microservices/02-structured-logging
  - 11-grpc/07-metadata-deadlines
  - 05-concurrency/18-context
docs:
  - title: "OpenTelemetry Go: Getting started"
    url: https://opentelemetry.io/docs/languages/go/getting-started/
  - title: "W3C Trace Context"
    url: https://www.w3.org/TR/trace-context/
  - title: "otelgrpc"
    url: https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc
---

Лог отвечает на вопрос «что случилось», метрика — «как часто и сколько», трасса — «где запрос провёл время». Запрос `POST /orders` прошёл через «Заказы», вызвал «Склад», тот сходил в MySQL, а потом «Уведомления» прочитали событие из Kafka. Трасса собирает всё это в одно дерево: каждый участок — **спан** с именем, видом (`server`, `client`, `producer`, `consumer`, `internal`), временем начала и конца, атрибутами, событиями и статусом. У всех спанов одной трассы общий `trace_id`, у каждого — свой `span_id` и ссылка на родителя.

**Компоненты OpenTelemetry в Go.** API (`go.opentelemetry.io/otel/trace`) — то, чем пользуется код: `tracer.Start(ctx, name, opts...)` возвращает новый `ctx` со спаном и сам спан, который нужно закрыть `span.End()`. SDK (`go.opentelemetry.io/otel/sdk/trace`) — реализация: `TracerProvider` с ресурсом (`service.name` — по нему спаны группируются в UI), сэмплером и экспортёром. Без SDK API работает как no-op, поэтому библиотеки инструментируют себя через API, а приложение в `main` решает, куда и сколько отправлять. Провайдер создаётся один раз и закрывается `tp.Shutdown(ctx)` при остановке — иначе последняя пачка спанов не уйдёт.

**Внутри процесса** родство передаётся через `context.Context`: спан, созданный от `ctx`, в котором уже есть спан, становится его ребёнком. Поэтому правило из модуля 5 — `ctx` первым параметром и дальше по цепочке — здесь обязательно: потерянный `ctx` рвёт дерево.

**Между процессами** контекст едет в заголовке `traceparent` (W3C Trace Context): `00-<trace_id>-<span_id родителя>-<флаги>`. Пропагатор (`propagation.TraceContext{}`) пишет его методом `Inject` и читает методом `Extract` через `TextMapCarrier` — интерфейс из трёх методов `Get`, `Set`, `Keys` над любыми заголовками. Для gRPC это делает `otelgrpc`: stats handler `otelgrpc.NewClientHandler()` на клиенте создаёт спан вызова и кладёт `traceparent` в metadata, `otelgrpc.NewServerHandler()` на сервере достаёт его и создаёт серверный спан (interceptors в `otelgrpc` удалены, только stats handlers). Для HTTP есть `otelhttp`, для Kafka готового пакета в песочнице нет — заголовки записи `kgo.Record` адаптируют к `TextMapCarrier` сами, это задание.

**Атрибуты, события, статус.** Атрибуты — то, по чему ищут: `order.id`, `db.system.name`, `messaging.destination.name`. Имена берут из семантических соглашений OpenTelemetry (пакет `semconv`), тогда Jaeger и Grafana понимают их без настройки. В отличие от меток метрик, в атрибутах спана можно хранить id заказа: каждый спан хранится отдельно, кардинальность не взрывает хранилище. События (`span.AddEvent`) — точки во времени внутри спана; `span.RecordError(err)` пишет событие `exception`. Статус `Error` (`span.SetStatus(codes.Error, msg)`) — сигнал «этот участок сломался». Ошибка бизнес-логики — не обязательно сбой: `otelgrpc` на клиенте помечает Error любой код, кроме `OK`, а на сервере — только `Unknown`, `DeadlineExceeded`, `Unimplemented`, `Internal`, `Unavailable` и `DataLoss`. Это видно в выводе примера: `FailedPrecondition` — ошибка для «Заказов», но не сбой «Склада».

**Куда отправлять.** В песочнице — `tracetest.NewInMemoryExporter()` (пример печатает дерево) или `stdouttrace.New(stdouttrace.WithPrettyPrint())` — JSON каждого спана в stdout. В настоящей системе — OTLP: экспортёр `otlptracegrpc` (в песочнице его нет) отправляет спаны в коллектор OpenTelemetry или прямо в Jaeger, который принимает OTLP на портах 4317 (gRPC) и 4318 (HTTP); адрес задаёт переменная `OTEL_EXPORTER_OTLP_ENDPOINT`. UI Jaeger открывается на порту 16686: выбираете сервис, находите трассу по `trace_id` из лога (урок `02-structured-logging`) и видите водопад спанов. В системе из `labs/` провайдер с OTLP-экспортёром собирает общий пакет `labs/platform/telemetry`, подробности запуска — в README сервисов. В проде экспортёр подключают через `sdktrace.WithBatcher` — спаны копятся и уходят пачками в фоне, а не синхронно на каждый `End`, как `WithSyncer` в примере.

**Сэмплирование.** Хранить каждую трассу дорого. `sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))` сохраняет 10 % новых трасс, а для продолженных следует решению родителя (флаг `01` в `traceparent`), чтобы трасса не оказалась «дырявой». Решение принимается в начале трассы — поэтому ошибки, случившиеся позже, могут в выборку не попасть; это решают «хвостовым» сэмплированием в коллекторе.

## Как это в C#

- **Activity вместо Span.** В .NET трассировка встроена в `System.Diagnostics`: `ActivitySource.StartActivity` ≈ `tracer.Start`, `Activity` ≈ спан, `SetTag` ≈ атрибут. OpenTelemetry .NET лишь подписывается на них. В Go API и SDK — сторонние модули `go.opentelemetry.io/otel`, а стандартная библиотека о трассах не знает.
- **Контекст.** Текущая `Activity` живёт в `AsyncLocal` (`Activity.Current`) и сама течёт через `await`; дочерний спан находит родителя неявно. В Go родитель — только тот спан, что лежит в переданном `ctx`.
- **Автоинструментация.** `AddAspNetCoreInstrumentation()`, `AddGrpcClientInstrumentation()`, `AddHttpClientInstrumentation()` одной строкой покрывают входящие и исходящие запросы. В Go каждую точку подключают явно: stats handler у gRPC-сервера и клиента, `otelhttp.NewHandler` у HTTP-сервера, обёртка транспорта у клиента, carrier у Kafka.
- **Пропагатор.** `DistributedContextPropagator.Current` в .NET по умолчанию пишет `traceparent`; у Confluent.Kafka заголовки — список пар `string → byte[]`, и carrier для них пишут так же руками, как в задании.

## Ловушки

### Спан без `End`

```go
ctx, span := tracer.Start(ctx, "reserve")
if err != nil {
	return err // span не закрыт: не экспортируется и копится в памяти
}
span.End()
```

Незавершённый спан не попадёт в экспорт, а трасса в UI будет без участка — как раз того, где была ошибка. Пишите `defer span.End()` сразу после `Start`, как `defer rows.Close()`.

### Новый `context.Background()` посередине

```go
func (s *Service) Reserve(ctx context.Context, id string) error {
	_, span := tracer.Start(context.Background(), "UPDATE stock") // корень новой трассы
	defer span.End()
```

Спан получился корнем отдельной трассы: в UI запрос «обрывается» на сервисе, а `UPDATE stock` лежит отдельно, без связи. То же происходит, если обработчик берёт не тот `ctx` — исходный вместо возвращённого `tracer.Start`, — тогда спаны становятся «братьями», а не детьми.

### Контекст не передан в сообщение

```go
rec := &kgo.Record{Topic: "orders.events", Value: payload}
cl.Produce(ctx, rec, nil) // traceparent в заголовках нет
```

Kafka не знает про трассы, `kgo` сам заголовок не добавит. Без `Inject` в заголовки и `Extract` на стороне потребителя трасса заканчивается на продюсере, и обработку события в «Уведомлениях» не связать с исходным запросом. Готовые обёртки — пакет `kotel` из franz-go (в песочнице его нет); внутри он делает то же, что задание.

## Задание

Напишите проброс контекста трассы через заголовки записей Kafka и обработку записи в спане потребителя.

```go
type HeaderCarrier struct{ r *kgo.Record } // дан в шаблоне
func NewHeaderCarrier(r *kgo.Record) HeaderCarrier
func (c HeaderCarrier) Get(key string) string
func (c HeaderCarrier) Set(key, value string)
func (c HeaderCarrier) Keys() []string

func Inject(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record)
func Extract(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record) context.Context
func Consume(ctx context.Context, tracer trace.Tracer, p propagation.TextMapPropagator, r *kgo.Record,
	handle func(ctx context.Context, r *kgo.Record) error) error
```

`HeaderCarrier` реализует `propagation.TextMapCarrier` над `r.Headers`. Ключи сравниваются как есть, с учётом регистра (это байты Kafka, а не заголовки HTTP):

- `Get` возвращает значение первого заголовка с этим ключом или `""`, если такого нет;
- `Set` оставляет ровно один заголовок с этим ключом: если такие уже есть, первый получает новое значение и остаётся на своём месте, остальные с этим ключом удаляются; если нет — заголовок добавляется в конец. Заголовки с другими ключами сохраняются в прежнем порядке. Работает и на записи без заголовков;
- `Keys` возвращает ключи заголовков в порядке появления, без повторов (для записи без заголовков — пустой срез или `nil`).

Функции:

- `Inject` записывает в заголовки `r` контекст из `ctx` пропагатором `p`; повторный `Inject` в ту же запись заменяет прежние значения, а не дублирует их;
- `Extract` возвращает контекст-наследник переданного `ctx` (его значения сохраняются) с контекстом трассы и baggage из заголовков `r`;
- `Consume` извлекает контекст из заголовков `r` поверх `ctx` и начинает спан с именем `"process " + r.Topic`, видом `trace.SpanKindConsumer` и ровно четырьмя атрибутами: `messaging.system` = `"kafka"`, `messaging.operation.type` = `"process"`, `messaging.destination.name` = `r.Topic`, `messaging.kafka.offset` = `r.Offset` (int64). Если в заголовках был контекст продюсера, спан — его ребёнок в той же трассе (даже если в переданном `ctx` уже есть спан). Если не было — родителем становится спан из переданного `ctx`, а без него спан — корень новой трассы (тесты в этом случае передают `ctx` без спана);
- `Consume` вызывает `handle` с контекстом, в котором лежит этот спан, и с той же записью `r`. Если `handle` вернул ошибку — спан получает событие `exception` (`span.RecordError(err)`) и статус `codes.Error` с описанием `err.Error()`, а `Consume` возвращает эту ошибку; при успехе статус спана не меняется, `Consume` возвращает `nil`. В обоих случаях спан завершается до возврата из `Consume`.
