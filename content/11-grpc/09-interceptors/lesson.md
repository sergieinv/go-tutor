---
title: Interceptors
summary: "Middleware gRPC: unary- и stream-interceptors сервера и клиента, ChainUnaryInterceptor и порядок звеньев, logging, recovery, auth, метрики и валидация; обёртка grpc.ServerStream; путь вызова по кадрам HTTP/2."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 11-grpc/06-grpc-streaming
  - 11-grpc/07-metadata-deadlines
  - 11-grpc/08-status-errors
  - 08-web/04-middleware
docs:
  - title: "grpc: UnaryServerInterceptor и ChainUnaryInterceptor"
    url: https://pkg.go.dev/google.golang.org/grpc#UnaryServerInterceptor
  - title: "gRPC over HTTP/2 — описание протокола"
    url: https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md
---

Interceptor — это middleware gRPC. Логирование, аутентификацию, перехват паник, метрики, валидацию и трассировку не пишут в каждом методе: их ставят в цепочку, через которую проходит любой вызов. Идея та же, что у HTTP-middleware из урока `08-web/04-middleware`. Разница в том, что звено получает уже десериализованное сообщение, имя метода и `handler` — остаток цепочки:

```go
type UnaryServerInterceptor func(ctx context.Context, req any,
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error)
```

Код до `handler(ctx, req)` работает на входе, код после — на выходе. Не вызвали `handler` — дальше вызов не пойдёт: так auth отказывает с `codes.Unauthenticated`. Чтобы передать что-то дальше по цепочке, звено зовёт `handler` с новым `ctx` (`context.WithValue`).

**Четыре вида.** На сервере — `UnaryServerInterceptor` и `StreamServerInterceptor`, на клиенте — `UnaryClientInterceptor` (оборачивает `invoker`, то есть сам вызов) и `StreamClientInterceptor` (оборачивает создание стрима). Unary- и stream-цепочки независимы. Стрим не проходит через unary-звенья, поэтому сквозную логику пишут в двух вариантах. Иначе стримовый метод окажется без auth.

**Цепочка.** `grpc.ChainUnaryInterceptor(a, b, c)` и `grpc.ChainStreamInterceptor(...)` на сервере, `grpc.WithChainUnaryInterceptor(...)` и `WithChainStreamInterceptor(...)` на клиенте. Первое звено — внешнее: на входе оно выполняется первым, на выходе — последним. Опция без `Chain` (`grpc.UnaryInterceptor`) принимает одно звено и всегда встаёт перед цепочкой, а повторный вызов вызывает панику при старте сервера. Типичный порядок на сервере:

1. **трассировка и request id** — всё, что ниже, пишет с ними;
2. **logging и метрики** — снаружи остальных, чтобы видеть итоговый код, включая отказ auth и `Internal` после паники. Метрики — RED по `info.FullMethod` и коду, подробно в модуле 12;
3. **recovery** — паника в обработчике без него роняет **весь процесс**: в grpc-go нет встроенного `recover`. Recovery отвечает `codes.Internal` с общим текстом, а значение паники и стек пишет только в лог;
4. **auth** — проверка токена из metadata (`authorization`) и права на метод. Отказ — `Unauthenticated` или `PermissionDenied`; пользователя кладут в `ctx`;
5. **валидация** (`04-protovalidate`) — после auth, чтобы анонимный клиент не узнавал правила валидации; ошибка — `InvalidArgument` с деталями.

Кроме того, сервер ограничивает размер входящего сообщения опцией `grpc.MaxRecvMsgSize` (по умолчанию 4 МБ). Это лимит транспорта: он срабатывает раньше interceptors и защищает от огромных сообщений.

Пример собирает серверную цепочку logging → recovery → auth, клиентский interceptor, который добавляет request id, и stream-interceptor с обёрткой стрима. Вывод показывает, в каком порядке срабатывают звенья в трёх случаях: успех, нет токена, паника.

**Стримы.** У stream-interceptor нет `ctx` и `req`: он получает `grpc.ServerStream`, а обработчик сам читает и пишет сообщения. Чтобы подменить контекст, посчитать или проверить сообщения, стрим оборачивают. Обёртка встраивает `grpc.ServerStream` и переопределяет `Context()`, `SendMsg` или `RecvMsg`, остальные методы переходят к ней через встраивание. Валидировать сообщения клиентского стрима можно в `RecvMsg` обёртки: каждое сообщение проверяется при получении.

Готовые звенья есть в `github.com/grpc-ecosystem/go-grpc-middleware/v2` (logging, recovery, auth, validator, retry), трассировка — `otelgrpc` (модуль 12). В песочнице go-grpc-middleware нет, но он устроен так же, как звенья в примере.

## Как это в C#

- **Базовый класс вместо функции.** В Grpc.AspNetCore interceptor — наследник `Interceptor` с виртуальными методами `UnaryServerHandler`, `ServerStreamingServerHandler`, `AsyncUnaryCall` и т.д. Регистрация — `AddGrpc(o => o.Interceptors.Add<T>())`, порядок добавления — порядок вызова, первый — внешний, как в `ChainUnaryInterceptor`. Экземпляр создаёт DI. В Go звено — замыкание, а зависимости передаются при сборке в `main`.
- **Ошибка — исключение.** В .NET звено ловит `RpcException` в `try/catch` вокруг `await continuation(...)`. В Go ошибка — второе возвращаемое значение `handler`, а код достают `status.Code(err)`.
- **Паника не равна исключению.** Необработанное исключение в обработчике ASP.NET Core превращается в `StatusCode.Unknown` («Exception was thrown by handler.»), и процесс живёт дальше. В Go паника без recovery-звена завершает весь процесс.
- **Auth.** В ASP.NET Core аутентификацию обычно делают middleware `AddAuthentication` и атрибутом `[Authorize]` на сервисе или методе. В Go конвейера нет: проверка токена и прав по `info.FullMethod` — это interceptor (подробно — в `12-grpc-security`).

## Под капотом

Сценарий проходит один unary-вызов насквозь. Клиентский interceptor дописывает metadata. Транспорт превращает дедлайн и metadata в заголовки HTTP/2, а сообщение — в кадр DATA с 5-байтовым префиксом. Сервер собирает из заголовков `ctx` и прогоняет вызов через цепочку. Статус возвращается в трейлерах. Во втором вызове обработчик паникует, recovery превращает панику в `Internal`, и ответ уходит одним кадром trailers-only.

Живой визуализатор запускает настоящие вызовы health-сервиса через bufconn. Каждый вызов клиента размечен задачей `trace.NewTask`, сервер открывает свою задачу, а каждое звено — регион. На полосах видна вложенность звеньев. При отказе auth полосы `handler` нет. При панике закрываются все регионы, и logging пишет `Internal`. Поменяйте порядок в `ChainUnaryInterceptor` и запустите снова.

## Ловушки

### Auth только в unary-цепочке

```go
srv := grpc.NewServer(
	grpc.ChainUnaryInterceptor(logging, recovery, auth),
	// стримы идут мимо auth и recovery
)
```

Стримовые методы (`ListOrders`, `Watch`) проходят только через `ChainStreamInterceptor`. Если там пусто, стрим доступен без токена, а паника в нём роняет сервер. Каждое сквозное правило ставят в обе цепочки, а проверку `info.FullMethod` для исключений (health, reflection) делают одной функцией для обоих вариантов.

### Recovery снаружи logging — паника не попадает в лог

```go
grpc.ChainUnaryInterceptor(recovery, logging, auth) // logging не увидит Internal
```

Паника проходит сквозь `logging`, как исключение сквозь блок без `finally`. Код после `handler(ctx, req)` не выполняется, строка лога и метрика не пишутся. Recovery превратит панику в ответ уже снаружи. Если recovery должен стоять первым (например, чтобы ловить панику и в logging), пишите лог в `defer` или логируйте прямо в recovery.

### Значение паники в ответе клиенту

```go
// УЯЗВИМО: текст паники уходит клиенту — пути, SQL, иногда секреты из ошибок драйвера
err = status.Errorf(codes.Internal, "panic: %v\n%s", p, debug.Stack())
```

`grpc-message` видит любой клиент, а grpc-gateway отдаёт его и в HTTP-ответе. Наружу — общий текст (`internal error`) и request id, а значение и стек — в лог (подробно — в `09-security/11-secrets-logs-errors`). Тем же правилом руководствуются все звенья: `Unauthenticated` не уточняет, что именно не так с токеном.

## Задание

Напишите серверную цепочку из четырёх unary-звеньев. `Chain(cfg)` возвращает их от внешнего к внутреннему: **request id → logging → recovery → auth**. Тесты подключают цепочку через `grpc.ChainUnaryInterceptor(Chain(cfg)...)` и вызывают настоящий сервер по bufconn.

```go
type Logger func(line string)

type Config struct {
	Log    Logger
	NewID  func() string     // request id, если клиент его не прислал
	Tokens map[string]string // пользователь → токен
}

func Chain(cfg Config) []grpc.UnaryServerInterceptor
func RequestIDFrom(ctx context.Context) string // "" — нет
func UserFrom(ctx context.Context) string      // "" — нет
```

- **request id.** Первое значение ключа `x-request-id` из входящей metadata. Если ключа нет или значение пустое, звено вызывает `cfg.NewID()` ровно один раз за вызов (если клиент прислал id, `NewID` не вызывается). Id кладётся в контекст (`RequestIDFrom` в любом следующем звене и обработчике) и отправляется клиенту в заголовке ответа `x-request-id` через `grpc.SetHeader` — до вызова следующего звена, так что заголовок уходит и при отказе auth, и при панике.
- **logging.** После завершения вызова пишет через `cfg.Log` ровно одну строку `<метод> <код> rid=<id>`, где метод — `info.FullMethod`, а код — `status.Code(err)` (`OK`, `NotFound`, `Unknown` для ошибок без статуса…). Пример: `/test.Echo/Say Unauthenticated rid=r-2`.
- **recovery.** Паника в обработчике или в auth превращается в `status.Error(codes.Internal, "internal error")` без деталей. Перед строкой logging пишется строка `panic <метод> rid=<id>: <значение>` (значение — через `%v`). После паники сервер обслуживает следующие вызовы.
- **auth.** Методы, чьё полное имя начинается с `/grpc.health.v1.Health/`, проходят без проверки. Для остальных во входящей metadata должно быть ровно одно значение `authorization` вида `Bearer <токен>`: префикс `Bearer ` с одним пробелом и с учётом регистра, токен непустой и в точности равен токену одного из пользователей в `cfg.Tokens`. Имена пользователей непустые, токены разных пользователей различны. Пользователь с пустым токеном в `cfg.Tokens` не проходит никогда. Сравнение — `crypto/subtle.ConstantTimeCompare`. Иначе — `status.Error(codes.Unauthenticated, "unauthenticated")`, и обработчик не вызывается. При успехе имя пользователя кладётся в контекст (`UserFrom`).
- `cfg.Log` и `cfg.NewID` всегда не `nil`. Ошибки обработчика проходят к клиенту без изменений. Для контекста без значений `RequestIDFrom` и `UserFrom` возвращают `""`.
