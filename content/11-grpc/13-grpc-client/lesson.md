---
title: gRPC-клиент в продакшне
summary: "Одно соединение на сервис, ленивый grpc.NewClient, схемы адресов и resolver, балансировка round_robin и политика повторов через service config, её ограничения, hedging, keepalive, WaitForReady и состояния канала; свой retry-interceptor по RetryInfo."
difficulty: 4
minutes: 45
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 11-grpc/08-status-errors
  - 11-grpc/09-interceptors
  - 11-grpc/07-metadata-deadlines
docs:
  - title: "grpc-go: NewClient"
    url: https://pkg.go.dev/google.golang.org/grpc#NewClient
  - title: "gRPC: Retry"
    url: https://grpc.io/docs/guides/retry/
  - title: "gRPC: Service config"
    url: https://grpc.io/docs/guides/service-config/
---

`*grpc.ClientConn` — не соединение, а **канал**: объект, который сам находит адреса сервиса, держит к ним HTTP/2-соединения, переподключается, балансирует вызовы и повторяет их по политике. Он безопасен для конкурентного использования, а вызовы мультиплексируются в соединениях — поэтому правило простое: **один канал на сервис-зависимость на весь процесс**. Его создают в `main`, передают в конструкторы клиентов (`NewOrderServiceClient(conn)` — дешёвая обёртка) и закрывают при остановке. Канал на каждый запрос — это рукопожатие TCP и TLS на каждый вызов и тысячи соединений у сервера.

**`grpc.NewClient` ленивый.** Он разбирает адрес и опции, но в сеть не ходит: канал в состоянии `IDLE` до первого вызова или `conn.Connect()`, ошибки подключения проявятся в вызовах как `Unavailable`. Устаревший `grpc.Dial` с `WithBlock` ждал подключения при старте — в новом коде так не делают: сервис должен стартовать и тогда, когда зависимость ещё поднимается. Состояния канала (`conn.GetState()`, пакет `connectivity`): `IDLE` → `CONNECTING` → `READY`, при обрыве — `TRANSIENT_FAILURE` с переподключением по экспоненциальному backoff, после `Close` — `SHUTDOWN`. Без вызовов дольше 30 минут канал сам уходит в `IDLE` и закрывает соединения (`grpc.WithIdleTimeout`).

**Адрес — это target с схемой resolver'а.** `dns:///orders.shop.svc:50051` — DNS-имя, resolver вернёт все его A/AAAA-записи (у `NewClient` схема по умолчанию — `dns`, у старого `Dial` была `passthrough`). `passthrough:///host:port` — адрес как есть, без разрешения (так подключаются к `bufconn`). `unix:///run/app.sock` — Unix-сокет. Свой resolver (Consul, etcd, xDS) регистрируют через `resolver.Register` или передают каналу `grpc.WithResolvers`. Пакет `resolver/manual` — resolver, которому адреса отдают руками: в тестах и в примере ниже.

**Балансировщик** решает, в какой адрес пойдёт очередной вызов. По умолчанию — `pick_first`: одно соединение к первому доступному адресу. `round_robin` держит соединения ко всем адресам и раздаёт вызовы по кругу (стартовая позиция случайна). Включают его через **service config** — JSON с настройками канала, который приходит от resolver'а (DNS TXT-запись, xDS) или задаётся клиентом по умолчанию: `grpc.WithDefaultServiceConfig(json)`. Там же задают таймауты и политику повторов по методам (`methodConfig`).

**Политика повторов** (`retryPolicy`): `maxAttempts` (всего попыток, grpc-go режет до 5 — `grpc.WithMaxCallAttempts`), `initialBackoff`, `maxBackoff`, `backoffMultiplier` и `retryableStatusCodes`. Пауза в grpc-go 1.84 — экспонента, умноженная на случайный множитель 0,8–1,2. Сервер может управлять паузой трейлером `grpc-retry-pushback-ms` (отрицательное значение — «не повторять»), повторная попытка несёт заголовок `grpc-previous-rpc-attempts`. Ограничения, о которых стоит знать:

- повторяется только вызов, на который сервер ещё ничего не ответил (ни заголовков, ни сообщений) — иначе он «зафиксирован»;
- отправленное хранится в буфере на 256 КиБ (`grpc.MaxRetryRPCBufferSize`), большой клиентский стрим повторы теряет;
- все попытки укладываются в **один** дедлайн вызова;
- `retryThrottling` в service config ограничивает долю повторов, чтобы клиенты не добили перегруженный сервер (подробнее о retry storm — модуль 12);
- grpc-go не знает, идемпотентен ли метод, — включать повторы для неидемпотентных методов нельзя;
- политика смотрит только на код: деталь `RetryInfo` из `errdetails` (урок `08-status-errors`) встроенные повторы не читают — это делают свои interceptors, как в задании.

**Hedging** (`hedgingPolicy`) — отправка копий запроса, не дожидаясь ответа, чтобы срезать хвост задержек. Он описан в спецификации gRPC и есть в grpc-java и Grpc.Net.Client, но в grpc-go не реализован.

**Keepalive.** Простаивающее соединение может молча умереть на NAT или балансировщике. `grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: ..., Timeout: ...})` шлёт HTTP/2 PING и рвёт соединение, если ответа нет. Сервер по умолчанию не разрешает пинговать чаще раза в 5 минут (`keepalive.EnforcementPolicy.MinTime`) и после нескольких (в grpc-go — трёх) слишком частых пингов рвёт соединение: `GOAWAY` с кодом `ENHANCE_YOUR_CALM` — настройки клиента и сервера согласуют.

**`WaitForReady`.** По умолчанию вызов в канале без готовых соединений (`TRANSIENT_FAILURE`) сразу падает с `Unavailable`. `grpc.WaitForReady(true)` заставляет его ждать подключения до дедлайна — полезно на старте, когда зависимость поднимается вместе с вами, но только с дедлайном: иначе вызов будет ждать бесконечно.

Пример поднимает три экземпляра сервиса на loopback, отдаёт их адреса `manual` resolver'у и показывает: ленивый `IDLE`, круговую раздачу `round_robin`, прозрачный повтор `Unavailable` по service config (повторная попытка уходит на другой экземпляр), отказ повторять `InvalidArgument` и разницу между обычным вызовом и `WaitForReady` к лежащему серверу.

## Как это в C#

- **Канал.** `GrpcChannel.ForAddress(...)` — аналог `grpc.NewClient`: долгоживущий, потокобезопасный, подключается лениво (`channel.State == Idle`). В ASP.NET Core каналами управляет `AddGrpcClient<T>()` из Grpc.Net.ClientFactory; в Go канал создают в `main` и передают в конструкторы.
- **Балансировка и resolver.** `GrpcChannelOptions.ServiceConfig` с `LoadBalancingConfigs = { new RoundRobinConfig() }` и адрес `dns:///…` или `static:///` со `StaticResolverFactory` — те же идеи, что `round_robin` и `manual` resolver. Конфигурация — объектами C#, в Go — JSON-строкой.
- **Повторы.** `MethodConfig.RetryPolicy` повторяет семантику grpc-go (тот же предел — 5 попыток), а `HedgingPolicy` в .NET есть, в Go нет. Повторы на уровне HTTP (Polly, `Microsoft.Extensions.Http.Resilience`) для gRPC не подходят: они не понимают коды gRPC в трейлерах.
- **Keepalive** — свойства `SocketsHttpHandler` (`KeepAlivePingDelay`, `KeepAlivePingTimeout`), **WaitForReady** — `CallOptions.WithWaitForReady()`.

## Ловушки

### `pick_first` и ClusterIP: вся нагрузка в один под

```go
// Service типа ClusterIP: один виртуальный IP, балансировщик по умолчанию.
conn, _ := grpc.NewClient("orders:50051", grpc.WithTransportCredentials(creds))
```

kube-proxy выбирает под при установке TCP-соединения, а gRPC гонит все вызовы через одно долгоживущее соединение — один под получает всё, остальные простаивают. Нужны headless Service (`clusterIP: None`, DNS вернёт IP всех подов), адрес `dns:///orders.shop.svc.cluster.local:50051` и `{"loadBalancingConfig": [{"round_robin": {}}]}`. Или L7-балансировщик между сервисами (Envoy, service mesh), который понимает HTTP/2.

### DNS разрешается не так часто, как кажется

DNS-resolver grpc-go перечитывает записи при обрыве соединения или явном `ResolveNow`, и не чаще раза в 30 секунд, а TTL не учитывает. Новые поды после масштабирования клиент не увидит, пока старые соединения живы. Лекарство — на стороне сервера: `grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionAge: 5 * time.Minute, MaxConnectionAgeGrace: 30 * time.Second})` периодически закрывает соединения, клиенты переподключаются и заново разрешают имя.

### Повторы на двух уровнях перемножаются

```go
conn, _ := grpc.NewClient(target,
	grpc.WithDefaultServiceConfig(`{... "retryPolicy": {"maxAttempts": 3, ...}}`),
	grpc.WithUnaryInterceptor(myRetry(3))) // ещё 3 попытки поверх
```

Каждая попытка своего interceptor'а — это вызов канала со своими тремя попытками: до 9 запросов на один вызов, а если сервис сам ходит дальше с повторами — 27. Повторять нужно на одном уровне: либо service config, либо свой interceptor, тогда `grpc.WithDisableRetry()`. И только там, где операция идемпотентна.

## Задание

Напишите клиентский interceptor с повторами, который уважает `RetryInfo` сервера и дедлайн вызова. Время внедряется — тесты не ждут по-настоящему.

```go
type Config struct {
	MaxAttempts    int           // всего попыток, включая первую
	InitialBackoff time.Duration // пауза перед первым повтором без RetryInfo
	MaxBackoff     time.Duration // потолок паузы без RetryInfo; 0 — без потолка
	Sleep          func(ctx context.Context, d time.Duration) error
	Now            func() time.Time
}

func UnaryRetry(cfg Config) grpc.UnaryClientInterceptor
```

Требования:

- Каждая попытка вызывает `invoker` с исходными `method`, `req`, `reply`, `cc`, `opts` и контекстом, в исходящей metadata которого всё, что передал вызывающий, плюс ровно одно значение `x-attempt` — номер попытки (`"1"`, `"2"`, …). Если вызывающий сам передал `x-attempt`, его значение заменяется номером попытки.
- Успех — вернуть `nil`. `MaxAttempts < 1` — одна попытка. После последней попытки — вернуть её ошибку без паузы.
- Повторяются только ошибки с кодами `Unavailable` и `Aborted`, а `ResourceExhausted` — только если в деталях статуса есть `errdetails.RetryInfo` с заполненным `retry_delay`. Здесь и дальше `RetryInfo` без `retry_delay` считается отсутствующим. Остальные коды возвращаются сразу. Возвращаемая ошибка — ошибка `invoker` как есть (код, сообщение и детали сохранены).
- Пауза перед повтором: если в ошибке есть `RetryInfo` с `retry_delay` — ровно эта длительность (отрицательная — 0), без ограничения `MaxBackoff`. Иначе перед n-м повтором (n = 1, 2, …) — `InitialBackoff · 2^(n-1)`, но не больше `MaxBackoff`, если он больше нуля.
- Если у `ctx` есть дедлайн и пауза не меньше оставшегося времени (`дедлайн − cfg.Now()`), повтора не будет: вернуть последнюю ошибку, не вызывая `Sleep`. Это же касается уже прошедшего дедлайна (остаток отрицательный).
- Пауза — только через `cfg.Sleep(ctx, d)`. Если он вернул ошибку, вернуть `status.FromContextError(err).Err()` (код `Canceled` или `DeadlineExceeded`) без новых попыток.
- Interceptor используют конкурентно из многих горутин.
- Входные данные корректны: `Sleep` и `Now` всегда заданы, `InitialBackoff` и `MaxBackoff` неотрицательны, а значения в тестах такие, что `InitialBackoff · 2^(n-1)` не переполняется (при `MaxBackoff > 0` удваивать можно, пока не достигнут потолок).

Тесты поднимают сервер на `bufconn` с выключенными встроенными повторами (`grpc.WithDisableRetry()`), ставят ваш interceptor клиенту и подменяют `Sleep` и `Now`.
