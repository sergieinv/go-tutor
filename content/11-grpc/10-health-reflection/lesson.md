---
title: Health и reflection
summary: "Стандартный health-сервис grpc.health.v1: health.NewServer, статусы по сервисам, Check, Watch и List, Shutdown при остановке; связь с gRPC-пробами Kubernetes; reflection, grpcurl и grpcui и почему reflection выключают в проде."
difficulty: 3
minutes: 30
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 11-grpc/06-grpc-streaming
  - 11-grpc/09-interceptors
  - 05-concurrency/18-context
docs:
  - title: "grpc/health: Server"
    url: https://pkg.go.dev/google.golang.org/grpc/health#Server
  - title: "GRPC Health Checking Protocol"
    url: https://github.com/grpc/grpc/blob/master/doc/health-checking.md
  - title: "Kubernetes: liveness, readiness и startup-пробы"
    url: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
---

Балансировщику, Kubernetes и соседним сервисам нужно знать, можно ли слать запросы в этот экземпляр сервера. Для HTTP-сервиса пишут `/healthz`. У gRPC есть стандартный сервис `grpc.health.v1.Health`, одинаковый во всех языках, поэтому его понимают k8s, Envoy, grpcurl и клиент gRPC. В grpc-go готовая реализация — `google.golang.org/grpc/health`, сгенерированный код протокола — `health/grpc_health_v1`.

**Что внутри.** `health.NewServer()` — потокобезопасная карта «имя сервиса → статус» с тремя методами:

- `Check(service)` — разовый вопрос. Ответ — `SERVING` или `NOT_SERVING`. Если сервис неизвестен — ошибка `codes.NotFound`.
- `Watch(service)` — серверный стрим. Первое сообщение — текущий статус, дальше — только изменения. Для неизвестного сервиса приходит `SERVICE_UNKNOWN`, стрим не закрывается. Так узнают о проблеме сразу, без опроса.
- `List()` — все статусы разом. Метод недавно добавлен в протокол, и старые клиенты и серверы его не знают.

Имя сервиса — полное имя из контракта (`shop.v1.OrderService`). Пустое имя `""` означает «сервер целиком», и `NewServer` сразу ставит ему `SERVING`. Остальные статусы выставляет ваш код: `hs.SetServingStatus(name, status)`. Сам health-сервер ничего не проверяет. Он только хранит то, что ему сказали, и рассылает изменения подписчикам `Watch`. Кто и когда опрашивает базу и брокер, решаете вы. Этим и займётся задание.

**Остановка.** `hs.Shutdown()` переводит все сервисы в `NOT_SERVING` и запрещает дальнейшие изменения: фоновая проверка уже не «оживит» сервер, который останавливается. Вернуть статусы может только `Resume()`. Порядок graceful shutdown: `hs.Shutdown()` → пауза, за которую балансировщик и readiness-проба заметят NOT_SERVING → `srv.GracefulStop()` дожидается текущих вызовов. `GracefulStop` ждёт и открытые стримы, в том числе чужие `Watch`, поэтому без таймаута с запасным `srv.Stop()` он может не вернуться никогда. Подробно — в `12-microservices/12-deploy-k8s`.

**Reflection.** `reflection.Register(srv)` добавляет сервис `grpc.reflection.v1.ServerReflection` (и старый `v1alpha`). Он отдаёт имена сервисов и дескрипторы файлов, то есть скомпилированные `.proto` всех зарегистрированных сервисов. Через него работают инструменты, которые не знают контракт заранее:

```sh
grpcurl -plaintext localhost:8080 list                       # сервисы
grpcurl -plaintext localhost:8080 describe shop.v1.OrderService
grpcurl -plaintext -d '{"id": 1}' localhost:8080 shop.v1.OrderService/GetOrder
grpcui -plaintext localhost:8080                             # веб-форма для вызовов
```

Пример поднимает health и reflection рядом с `OrderService`. Он спрашивает статусы, подписывается на изменения и проходит путь «база упала — база вернулась — остановка». Потом делает то же, что `grpcurl list` и `describe`: обычным клиентом reflection получает список сервисов и методы из дескриптора.

**Пробы Kubernetes.** С версии 1.27 (GA) kubelet умеет gRPC-пробы без отдельной утилиты: он сам вызывает `Health/Check` и считает успехом только `SERVING`.

```yaml
readinessProbe:
  grpc: { port: 8080, service: readiness }  # service — имя для Check, по умолчанию ""
livenessProbe:
  grpc: { port: 8080 }                      # "" — процесс жив и отвечает
```

Поле `service` позволяет завести разные «имена» под разные пробы и выставлять им разные статусы. TLS и заголовки авторизации gRPC-проба не поддерживает, поэтому health обычно освобождают от auth-interceptor (как в задании `09-interceptors`) или слушают на отдельном порту. Раньше для этого ставили в образ утилиту `grpc_health_probe`. Клиент gRPC тоже умеет следить за health соединений через `Watch` и не слать вызовы в NOT_SERVING-бэкенд. Это включается service config'ом (`healthCheckConfig`) при балансировке `round_robin`, подробнее — в `13-grpc-client`.

## Как это в C#

- **Health checks.** В ASP.NET Core проверки — это `IHealthCheck` и `AddHealthChecks().AddCheck(...)`, а пакет Grpc.AspNetCore.HealthChecks транслирует их результаты в `grpc.health.v1.Health`: `AddGrpcHealthChecks()` и `MapGrpcHealthChecksService()`. Связь сервиса с проверками задают `o.Services.Map("shop.v1.OrderService", r => r.Tags.Contains("db"))`. В Go `health.Server` — только хранилище статусов, проверки и их расписание пишутся руками.
- **Когда выполняются проверки.** В .NET `Check` по умолчанию заново выполняет проверки на каждый вызов, а `Watch` получает результаты фонового `HealthCheckPublisher` (раз в 30 с). В Go `Check` отдаёт последний выставленный статус мгновенно, а частоту опроса зависимостей задаёт ваш цикл.
- **Остановка.** В .NET на `ApplicationStopping` все `Watch` завершаются статусом NotServing. В Go то же делает явный вызов `hs.Shutdown()` в вашем коде остановки.
- **Reflection.** Пакет Grpc.AspNetCore.Server.Reflection: `AddGrpcReflection()` и `MapGrpcReflectionService()`, обычно под `if (app.Environment.IsDevelopment())`. В Go окружений нет: решение «включать или нет» — флаг конфигурации в `main`.

## Ловушки

### Liveness, который проверяет базу

```go
// liveness и readiness смотрят на одно и то же имя ""
hs.SetServingStatus("", statusOf(dbErr)) // база упала → NOT_SERVING для обеих проб
```

Liveness отвечает на вопрос «процесс завис — перезапустить?». Если он зависит от базы, минутный сбой PostgreSQL заставит k8s перезапустить **все** поды сервиса. База от этого не починится, а одновременный старт под нагрузкой ещё и ударит по ней. Зависимости проверяет только readiness (отдельное имя сервиса). Liveness — `""` в статусе SERVING, пока процесс отвечает.

### Статус своего сервиса не выставлен

```go
hs := health.NewServer()
healthpb.RegisterHealthServer(srv, hs)
// забыли hs.SetServingStatus("shop.v1.OrderService", SERVING)
```

Проба или балансировщик с `service: shop.v1.OrderService` получит `NotFound`, а не SERVING. Под никогда не станет ready, хотя `Check("")` отвечает SERVING. Регистрация сервиса в `grpc.Server` health-статус не создаёт: имена в health — это просто ключи карты.

### Reflection открыт наружу

```go
reflection.Register(srv) // УЯЗВИМО в проде: любой клиент получит полный контракт
```

Reflection отдаёт все сервисы и методы, включая внутренние и админские, и описания всех полей. Это готовая карта API для атакующего. В проде reflection выключают флагом конфигурации или закрывают тем же auth-interceptor, что и остальные методы (`/grpc.reflection.` не попадает в список исключений). Для отладки хватит `grpcurl -proto orders.proto ...` с локальным контрактом.

## Задание

Напишите монитор, который выставляет статусы health-сервера по состоянию зависимостей: база недоступна — NOT_SERVING.

```go
type Dependency struct {
	Name  string
	Check func(ctx context.Context) error
}

func NewMonitor(hs *health.Server, deps []Dependency, services map[string][]string, timeout time.Duration) *Monitor
func (m *Monitor) Refresh(ctx context.Context) []string
func (m *Monitor) Run(ctx context.Context, tick <-chan time.Time)
```

`services` сопоставляет имя gRPC-сервиса с именами нужных ему зависимостей.

- `Refresh` проверяет **каждую** зависимость из `deps` ровно один раз, даже если она нужна нескольким сервисам или не нужна ни одному. Проверки выполняются **параллельно**, каждая — с контекстом `context.WithTimeout(ctx, timeout)`. Зависимость упала, если `Check` вернул ошибку (в том числе из-за таймаута) или запаниковал. Паника не должна ронять процесс. `Refresh` возвращается, когда завершились все проверки. Каждый `Check` в тестах возвращается вскоре после отмены своего контекста; проверок, которые игнорируют `ctx`, в тестах нет. Имена в `deps` уникальны.
- Сервис получает `SERVING`, если живы все его зависимости (пустой список — `SERVING`), иначе `NOT_SERVING`. Имя, которого нет в `deps`, считается упавшей зависимостью. Статус выставляется через `hs.SetServingStatus` для каждого сервиса из `services`.
- Пустое имя `""` получает `SERVING`, если `SERVING` все сервисы из `services` (если сервисов нет — тоже `SERVING`), иначе `NOT_SERVING`.
- `Refresh` возвращает имена упавших зависимостей, включая неизвестные имена из `services`: без повторов, по возрастанию. Если все живы — пустой срез (`nil` допустим).
- `Run` сразу вызывает `Refresh` (даже если `ctx` уже отменён — тогда проверки получат отменённый контекст и упадут), затем вызывает его на каждое значение из `tick`. `Run` возвращается, когда `ctx` отменён или канал `tick` закрыт. В проде `tick` — это `time.NewTicker(...).C`, в тестах — канал, которым управляет тест.

Тесты проверяют статусы настоящими вызовами `Check` и `Watch` по bufconn.
