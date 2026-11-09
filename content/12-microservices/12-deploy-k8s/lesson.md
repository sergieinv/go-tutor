---
title: Docker и Kubernetes
summary: "Маленький образ: multi-stage, кеш модулей, CGO_ENABLED=0, distroless, не root, -trimpath -ldflags; версия сборки; пробы startup, liveness и readiness (HTTP и gRPC health); graceful shutdown при SIGTERM, preStop и terminationGracePeriodSeconds; GOMAXPROCS и GOMEMLIMIT в контейнере; конфигурация и секреты."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 08-web/08-graceful-shutdown
  - 11-grpc/10-health-reflection
  - 10-services-db/02-project-layout
  - 06-stdlib/22-signals-and-exit
docs:
  - title: "Kubernetes: Configure Liveness, Readiness and Startup Probes"
    url: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
  - title: "Kubernetes: Pod termination"
    url: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination
  - title: "Go 1.25 release notes: container-aware GOMAXPROCS"
    url: https://go.dev/doc/go1.25#container-aware-gomaxprocs
---

Go-сервис собирается в один статический бинарник без рантайма: ни JIT, ни сборок, ни `dotnet` в образе. Поэтому образ бывает в десятки мегабайт, стартует за миллисекунды, а атаковать в нём почти нечего — в distroless нет даже shell. Но чтобы Kubernetes обращался с подом правильно, сервис должен честно отвечать на пробы и правильно останавливаться. Об этом урок.

**Образ: multi-stage.**

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download   # слой зависимостей кешируется
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/orders ./cmd/orders

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/orders /orders
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/orders"]
```

- `go.mod`/`go.sum` копируются отдельно до исходников: пока зависимости не менялись, слой `go mod download` берётся из кеша. Cache mounts BuildKit сохраняют кеш модулей и сборки между сборками.
- `CGO_ENABLED=0` — статический бинарник без libc: он запустится в `scratch` или `distroless/static`. Нужен cgo (старый SQLite-драйвер, librdkafka) — берите `distroless/base` с glibc. Драйверы курса (`pgx`, `go-sql-driver/mysql`, `modernc.org/sqlite`, `franz-go`) — на чистом Go.
- `-trimpath` убирает из бинарника пути сборочной машины, `-s -w` — таблицу символов и DWARF (минус 20–30% размера; стеки паник остаются читаемыми).
- `distroless/static:nonroot` — сертификаты CA, `/etc/passwd`, часовые пояса и пользователь `nonroot` (uid 65532), без shell и пакетного менеджера. `scratch` ещё меньше, но сертификаты и `tzdata` придётся положить самим (или встроить `import _ "time/tzdata"`).
- Не root: `USER` в образе и `runAsNonRoot` в манифесте; файловая система — только для чтения (`readOnlyRootFilesystem`), временные файлы — в `emptyDir`.

**Версия сборки.** `-ldflags "-X main.version=1.4.2"` подменяет строковую переменную пакета при линковке — так в бинарник попадает версия из CI. С Go 1.24 `go build` в git-репозитории и сам записывает версию главного модуля из тега (`v1.4.2`, псевдоверсию или `+dirty`), а `debug.ReadBuildInfo()` отдаёт её вместе с ревизией (`vcs.revision`), временем коммита и флагами сборки — удобно для `/version` и первой строки лога. В песочнице git нет, поэтому пример печатает только `-X`-переменную.

**Три пробы.** kubelet спрашивает их сам, а результат решает разное:

| Проба | Провал значит | Что проверять |
| --- | --- | --- |
| startup | «ещё стартует»: остальные пробы не выполняются | старт закончен: конфигурация прочитана, миграции применены, кеш прогрет |
| liveness | «завис» — **перезапустить контейнер** | только сам процесс: обработчик отвечает. Никаких зависимостей |
| readiness | «не слать трафик» — убрать под из endpoints Service | стадия (не стартует и не останавливается) и то, без чего сервис не обслужит ни одного запроса |

Главная ошибка — проверять базу в liveness: база упала, liveness всех подов падает, kubelet перезапускает их по кругу, а база от этого не поднимется. В readiness — только критичные зависимости: если без кеша сервис работает (медленнее), кеш в readiness не нужен, иначе сбой Redis выведет из ротации все поды. Проверки readiness — с коротким таймаутом и без деталей ошибок в ответе. Для gRPC-сервиса проба может спрашивать `grpc.health.v1` напрямую (`grpc:` в манифесте, Kubernetes 1.27+): статусы `SERVING`/`NOT_SERVING` из урока о health и reflection — это та же readiness.

**Остановка.** Удаление пода (деплой, масштабирование, вытеснение) идёт так: под помечается удаляемым и убирается из endpoints, kubelet выполняет `preStop`-хук, шлёт процессу `SIGTERM`, ждёт и через `terminationGracePeriodSeconds` (по умолчанию 30 с, вместе с `preStop`) добивает `SIGKILL`. Удаление из endpoints и `SIGTERM` происходят **параллельно**: kube-proxy, ingress и балансировщики узнают о нём через секунды, и всё это время новые запросы ещё приходят. Поэтому порядок в сервисе:

1. `SIGTERM` (`signal.NotifyContext`) → стадия draining: readiness отвечает 503.
2. Пауза в несколько секунд — новые запросы ещё обслуживаются. Её делают в коде (`time.Sleep`) или `preStop`-хуком; в distroless нет `sleep`, поэтому `exec: [sleep, 5]` не сработает — в Kubernetes 1.30+ есть `preStop: {sleep: {seconds: 5}}`.
3. `srv.Shutdown(ctx)` / `grpcServer.GracefulStop()` — закрыть listener и дождаться активных запросов, с таймаутом меньше оставшегося grace period.
4. Остановить фоновые воркеры и консьюмеры Kafka (закоммитить офсеты), закрыть пул БД, сбросить трассы и метрики. Выйти с кодом 0.

Если процесс в контейнере не PID 1 (запущен через `sh -c`), `SIGTERM` достаётся shell, и сервис его не увидит — `ENTRYPOINT` в exec-форме (`["/orders"]`) решает это.

**CPU и память.** С Go 1.25 рантайм на Linux сам учитывает лимит CPU cgroup (при `go 1.25`+ в go.mod главного модуля): `GOMAXPROCS` по умолчанию — меньшее из числа доступных процессу CPU (`sched_getaffinity`) и `limits.cpu`, округлённого вверх (но не меньше 2), и пересчитывается, если лимит изменили. Учитывается именно limit, а не request; без limit — все CPU узла. Отключается `GODEBUG=containermaxprocs=0` или явным `GOMAXPROCS`, пересчёт на лету — `GODEBUG=updatemaxprocs=0`; раньше для этого подключали `go.uber.org/automaxprocs`. Память рантайм из cgroup не берёт: без `GOMEMLIMIT` куча растёт до `GOGC`-порога, и под с лимитом 512 МиБ получает OOMKill, хотя мог бы чаще собирать мусор. Ставьте `GOMEMLIMIT` примерно в 80–90% от `limits.memory` — это мягкий предел для сборщика, а не жёсткий потолок.

**Конфигурация и секреты.** Конфигурация — переменные окружения и флаги в одну структуру с валидацией при старте (урок о раскладке проекта). Секреты — не в образе и не в `ENV` Dockerfile (их видно в `docker history`): из Secret через переменную окружения или, лучше, файлом из тома (`/etc/orders/secrets/db-password`) — файл можно перечитать при ротации, и он не попадает в `/proc/<pid>/environ` и дампы окружения. В логи — никогда (урок о секретах в логах).

Пример урока — HTTP-сервис с тремя пробами: readiness зависит от стадии и «базы», liveness — нет, а остановка по `SIGTERM` идёт в правильном порядке и дожидается запроса, который застал её посреди работы. Рядом лежит `deployment.yaml` с пробами, ресурсами, `GOMEMLIMIT` и секретом из тома.

## Как это в C#

- **Образ.** В .NET — `dotnet publish -c Release` и образ `mcr.microsoft.com/dotnet/aspnet` или chiseled `runtime-deps:*-chiseled` (без shell и пакетного менеджера, nonroot — аналог distroless), с Native AOT — self-contained бинарник. В Go статический бинарник — поведение по умолчанию, а не отдельный режим публикации с ограничениями на рефлексию.
- **Пробы.** ASP.NET Core: `AddHealthChecks().AddNpgSql(...)` и `MapHealthChecks("/healthz/ready", new() { Predicate = c => c.Tags.Contains("ready") })`, для gRPC — `Grpc.AspNetCore.HealthChecks`. В Go пакета health checks в стандартной библиотеке нет: три маленьких обработчика и стадия — это и есть задание урока.
- **Остановка.** `IHostApplicationLifetime.ApplicationStopping` и `HostOptions.ShutdownTimeout` (по умолчанию 30 с, с .NET 6) делают за вас шаг 3; переключение readiness и паузу для балансировщиков добавляют сами. В Go весь порядок — явный код в `main` после `<-ctx.Done()`.
- **Память.** .NET GC сам видит лимит памяти контейнера (heap hard limit по умолчанию — 75% лимита). Go — нет: `GOMEMLIMIT` задают вручную. `GOMAXPROCS` с Go 1.25 учитывает лимит CPU, как .NET давно учитывает его для `Environment.ProcessorCount`.

## Ловушки

### База в liveness

```go
mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
	if err := db.PingContext(r.Context()); err != nil { // упала база — «умер» под
		http.Error(w, "db down", http.StatusServiceUnavailable)
		return
	}
})
```

На сбой базы kubelet ответит перезапуском всех подов — каскадный отказ: холодный старт, переподключения, миграции на старте, и ни один под не обслуживает даже те запросы, которым база не нужна. Liveness проверяет только сам процесс, зависимости — в readiness.

### Shutdown сразу после SIGTERM

```go
<-ctx.Done()
srv.Shutdown(context.Background()) // listener закрыт, а трафик ещё идёт
```

Балансировщики узнают об удалении пода позже, чем под получает `SIGTERM`. Закрытый listener отвечает на новые соединения отказом — во время каждого деплоя клиенты видят всплеск 502/connection refused. Сначала readiness 503 и пауза, потом `Shutdown` — и обязательно с таймаутом, иначе зависший запрос дотянет до `SIGKILL`.

### Секрет в образе

```dockerfile
# УЯЗВИМО: видно в docker history и docker inspect
ENV DB_PASSWORD=s3cret
# УЯЗВИМО: файл лежит в слое образа навсегда
COPY .env /app/.env
```

Слои образа неизменяемы: удалённый в следующем слое файл всё равно лежит в предыдущем, и любой, кто может скачать образ, его прочитает. Секреты приходят при запуске — из Secret Kubernetes (файлом из тома или переменной окружения), Vault, облачного менеджера секретов — и никогда не попадают в `Dockerfile`, `ARG` и контекст сборки (`.dockerignore`).

## Задание

Напишите менеджер проб со стадиями starting → ready → draining.

```go
type Stage int

const (
	Starting Stage = iota
	Ready
	Draining
)

func (s Stage) String() string

func New(timeout time.Duration) *Health
func (h *Health) AddCheck(name string, fn func(ctx context.Context) error)
func (h *Health) MarkReady()
func (h *Health) Drain()
func (h *Health) Stage() Stage
func (h *Health) Live() http.Handler
func (h *Health) Startup() http.Handler
func (h *Health) Ready() http.Handler
```

- `String` — `"starting"`, `"ready"`, `"draining"`; для других значений — любая другая строка.
- `New` создаёт менеджер в стадии `Starting`; `timeout` — общий таймаут проверок readiness, `timeout <= 0` — 1 секунда.
- Переходы только вперёд: `MarkReady` переводит `Starting` → `Ready` (в остальных стадиях ничего не делает), `Drain` переводит в `Draining` из любой стадии, и обратно пути нет. Повторные вызовы безопасны.
- `AddCheck` регистрирует проверку зависимости; проверка упала, если вернула ошибку. Повторный `AddCheck` с тем же именем заменяет прежнюю проверку.
- Все обработчики отвечают кодом и телом в одну строку с `\n` в конце, `Content-Type: text/plain; charset=utf-8`, на любой метод и путь:
  - `Live` — всегда `200 ok`;
  - `Startup` — `200 ok`, если `MarkReady` хоть раз перевёл менеджер в `Ready` (и после `Drain` тоже), иначе `503 starting` — в том числе после `Drain` прямо из `Starting`, без `MarkReady`;
  - `Ready` — в `Starting` и `Draining` отвечает `503` с именем стадии (`starting`, `draining`), не вызывая проверок. В `Ready` запускает все проверки одновременно с контекстом-потомком контекста запроса и таймаутом `timeout`; все прошли (или проверок нет) — `200 ok`, иначе `503 not ready: <имена упавших>` — по алфавиту, через запятую без пробелов, например `not ready: db,kafka`. Проверка, которая не вернулась до таймаута, считается упавшей, и `Ready` её не дожидается. Текст ошибок проверок в ответ не попадает.
- Все методы безопасны для одновременного вызова из многих горутин.
