# Платформа сервисов

Общий каркас трёх сервисов ([`labs/README.md`](../README.md)) — то, что в
.NET дают Microsoft.Extensions.* (Logging, Hosting, HealthChecks),
OpenTelemetry .NET, prometheus-net и Polly. Здесь это несколько небольших
пакетов поверх стандартной библиотеки и официальных клиентов. Каждый
сервис собирает их в своём `main` явно.

| Пакет | Что | .NET-аналог |
|---|---|---|
| `logging` | `slog` JSON + `request_id`/`trace_id`/`span_id` из контекста; `Secret` — строка, скрытая в журнале и fmt | ILogger + scopes, Serilog enrichers |
| `telemetry` | TracerProvider, экспорт OTLP/gRPC (Jaeger), пропагатор W3C Trace Context | `AddOpenTelemetry().WithTracing().AddOtlpExporter()` |
| `grpcx` | gRPC-сервер и клиент: лимиты, keepalive, otelgrpc, interceptors (id запроса → журнал → метрики RED → recovery → дедлайн по умолчанию → protovalidate) | Grpc.AspNetCore + interceptors |
| `httpx` | HTTP-сервер с таймаутами, middleware (id запроса, recover, дедлайн, журнал и RED по шаблону маршрута), grpc-gateway с единым форматом ошибок, служебный обработчик `/healthz` `/readyz` `/metrics` | Kestrel limits, middleware, JsonTranscoding, ProblemDetails |
| `health` | готовность с проверками и режимом draining; синхронизация с `grpc.health.v1` | `AddHealthChecks()` с тегами live/ready |
| `resilience` | `Retry` (экспоненциальная пауза, full jitter, уважение дедлайна) и `Breaker` (closed → open → half-open) с внедряемыми часами | Polly / Microsoft.Extensions.Http.Resilience |
| `kafkax` | заголовки записи как `propagation.TextMapCarrier`, создание топика | — |

Почему общий модуль, а не копия кода в каждом сервисе: правила (формат
журнала, имена метрик, лимиты, порядок interceptor'ов) должны совпадать во
всей системе, иначе не работают дашборды и поиск по журналам. Цена —
связность: изменение платформы затрагивает все сервисы. Поэтому сюда
попадает только инфраструктура и никакой бизнес-логики. В компании такой
модуль версионируют и обновляют в сервисах по одному.

```sh
cd labs/platform
go test -race ./...
```

Тесты платформы используют контракт «Склада» из `labs/api` как пример
сервиса: interceptors проверяются на `bufconn`, gateway — через `httptest`,
повторы и выключатель — на фиктивных часах без пауз.
