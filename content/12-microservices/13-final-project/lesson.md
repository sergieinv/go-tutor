---
title: "Итоговый проект: три сервиса"
summary: "Итог модулей 11–12: экскурсия по labs/ (какой урок в каком файле), запуск и наблюдение (трасса через три сервиса в Jaeger, метрики в Prometheus, падение склада → 503 с RetryInfo и выключатель, компенсация), модель всей цепочки в одном процессе, чек-лист продакшн-готовности, упражнения и сага с гарантированной компенсацией."
difficulty: 5
minutes: 90
runner:
  race: true
prerequisites:
  - 11-grpc/14-orders-api
  - 12-microservices/11-outbox-saga
  - 12-microservices/12-deploy-k8s
docs:
  - title: "Google SRE Book: Addressing Cascading Failures"
    url: https://sre.google/sre-book/addressing-cascading-failures/
  - title: "OpenTelemetry Go"
    url: https://opentelemetry.io/docs/languages/go/
  - title: "Prometheus: Instrumentation best practices"
    url: https://prometheus.io/docs/practices/instrumentation/
---

Модули 11 и 12 разбирали приёмы по одному: контракт, interceptor, трассу, повторы, выключатель, консьюмер, outbox, сагу. В каталоге `labs/` все они работают вместе — в системе из трёх сервисов, написанной так, как пишут в продакшне. «Заказы» принимают REST и gRPC и хранят заказы в PostgreSQL. «Склад» держит остатки в MySQL. «Уведомления» читают события из Kafka. Этот урок — экскурсия по системе. Сначала — где в коде живёт каждый урок. Потом — как запустить систему и увидеть трассу, метрики и сбой своими глазами. Дальше — пример, который собирает всю цепочку в одном процессе песочницы, чек-лист продакшн-готовности и список того, что стоит доделать самому. Задача урока закрывает одну из таких дыр: компенсацию, которая не теряется, даже если склад не ответил.

Общая схема и правила системы — в `labs/README.md`, подробности — в README каждого сервиса. Читайте их вместе с кодом: комментарии в `labs/` объясняют не «что», а «почему».

### Где какой урок

Пути — от корня репозитория.

| Урок | Где в `labs/` | На что смотреть |
| --- | --- | --- |
| 11/01–03 protobuf, эволюция, buf | `labs/api/buf.yaml`, `labs/api/proto/events/v1/events.proto`, `labs/api/gen/` | lint `STANDARD`, breaking `FILE`; конверт `OrderEvent` с `oneof` и `event_id`; сгенерированный код закоммичен, CI проверяет `buf generate && git diff --exit-code` |
| 11/04 protovalidate | `labs/api/proto/orders/v1/orders.proto`, `labs/platform/grpcx/interceptors.go` | правила в полях и CEL `unique()`; `ValidationUnary` — валидация один раз для всех методов, ответ `BadRequest` |
| 11/05, 11/14 сервер и API заказов | `labs/orders/internal/grpcapi/server.go` | тонкие обработчики: proto ↔ домен, курсор пагинации |
| 11/07 metadata и дедлайны | `labs/platform/grpcx/interceptors.go`, `labs/platform/grpcx/client.go` | `TimeoutUnary` даёт дедлайн вызову без дедлайна; `x-request-id` уходит соседу через `RequestIDClient` |
| 11/08 статусы и детали | `labs/orders/internal/grpcapi/errors.go`, `labs/warehouse/internal/grpcapi/errors.go`, `labs/orders/internal/warehouse/client.go` | ошибки → коды в одном месте; `ErrorInfo`, `PreconditionFailure`, `RetryInfo`; обратный перевод `toDomain` — по коду и `reason`, а не по тексту |
| 11/09 interceptors | `labs/platform/grpcx/server.go` | порядок цепочки: id запроса → журнал → метрики → recovery → дедлайн → валидация; otelgrpc — stats handler, а не interceptor |
| 11/10 health и reflection | `labs/platform/health/health.go` | `SyncGRPC` переносит готовность в `grpc.health.v1`; reflection включается переменной `*_GRPC_REFLECTION` |
| 11/11 grpc-gateway | `labs/platform/httpx/gateway.go` | единый формат ошибки с `request_id`, внутренние metadata не становятся HTTP-заголовками |
| 11/12 безопасность | `labs/platform/grpcx/server.go` (`ServerConfig.Creds`) | точка для mTLS есть, сертификатов на стенде нет — упражнение ниже |
| 11/13 клиент | `labs/platform/grpcx/client.go` | одно соединение на сервис, `dns:///` + `round_robin`, keepalive не чаще политики сервера |
| 12/01 архитектура | `labs/README.md`, `docker-compose.yml` | у каждого сервиса своя база; синхронно — только резерв |
| 12/02 журнал | `labs/platform/logging/logging.go` | обёртка `slog.Handler` с `request_id`/`trace_id`, тип `Secret`, проверка входящего `X-Request-ID` |
| 12/03 трассировка | `labs/platform/telemetry/telemetry.go`, `labs/platform/kafkax/kafkax.go`, `labs/orders/internal/outbox/relay.go`, `labs/notifications/internal/consumer/consumer.go` | `ParentBased` сэмплер, `WithBatcher`; `HeaderCarrier` над заголовками Kafka; relay продолжает трассу запроса, консьюмер — трассу relay |
| 12/04 метрики | `labs/platform/grpcx/metrics.go`, `labs/platform/httpx/httpx.go`, `labs/prometheus/prometheus.yml` | RED по методу и коду; HTTP — по шаблону маршрута, а не по пути (кардинальность) |
| 12/05–06 повторы и выключатель | `labs/platform/resilience/retry.go`, `labs/platform/resilience/breaker.go`, `labs/orders/internal/warehouse/client.go` | дедлайн → повторы только на `UNAVAILABLE` → выключатель → gRPC; `isFailure` не считает бизнес-отказы |
| 12/07 бюджет времени | `labs/orders/internal/warehouse/client.go`, `labs/orders/internal/orders/service.go` | `WithTimeout` не продлевает дедлайн родителя; компенсация — на `context.WithoutCancel` со своим таймаутом |
| 12/08 Kafka | `labs/orders/internal/outbox/relay.go`, `labs/platform/kafkax/kafkax.go` | ключ — номер заказа, заголовки `event-id`/`event-type`/`traceparent`, `EnsureTopic` |
| 12/09 консьюмеры | `labs/notifications/internal/consumer/consumer.go` | `DisableAutoCommit`, `BlockRebalanceOnPoll`, `CommitRecords` после обработки, мягкая остановка с `context.WithoutCancel` |
| 12/10 надёжность | `labs/notifications/internal/consumer/consumer.go`, `labs/notifications/internal/store/sqlite/store.go` | DLQ с заголовками причины, повторы с внедрённым сном; `processed_events … ON CONFLICT DO NOTHING` в одной транзакции с уведомлением |
| 12/11 outbox и сага | `labs/orders/internal/orders/service.go`, `labs/orders/internal/store/postgres/query.sql` | номер до резерва, компенсация при неизвестном исходе, неоднозначный COMMIT; relay под `pg_try_advisory_xact_lock` |
| 12/12 Docker и k8s | `labs/orders/Dockerfile`, `labs/orders/cmd/orders/main.go` | multi-stage, `scratch`, uid 65532; порядок остановки: готовность → REST → gRPC → relay → служебный сервер |
| 10/04, 10/08 тесты и миграции | `labs/orders/internal/storetest/storetest.go`, `labs/orders/migrations/`, `labs/warehouse/warehousetest/warehousetest.go` | один контрактный набор на SQLite и PostgreSQL; «Склад» в памяти для тестов клиентов — не мок, а настоящий домен |

Самое поучительное — два файла. Начните с `labs/orders/internal/orders/service.go`: сага на 150 строк, где каждая ветка ошибки объяснена. За ним — `labs/notifications/internal/consumer/consumer.go`: всё, что происходит с записью между «прочитали» и «закоммитили», лежит в одном файле.

### Запуск и наблюдение

```sh
docker compose --profile labs up --build      # или docker-compose, если плагина compose нет
```

Сервисы сами применяют миграции, создают топики и заводят демо-каталог: 1 — «Клавиатура» (10 шт.), 2 — «Мышь» (25), 3 — «Монитор» (3). Базы живут в tmpfs, так что после перезапуска стенд пуст. Порты открыты только на `127.0.0.1`. Полный curl-сценарий — в `labs/README.md`, ниже — что в нём смотреть.

**1. Заказ и трасса через три сервиса.**

```sh
curl -si -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 42, "items": [{"product_id": 3, "quantity": 2}, {"product_id": 1, "quantity": 1}]}'
curl -s localhost:8091/v1/stock/3                       # available 1, reserved 2
docker compose logs notifications | grep 'notification sent'   # там trace_id
```

В ответе — заказ в статусе `ORDER_STATUS_RESERVED`. `int64` в JSON приходит строкой (`"order_id": "1"`): так protojson защищает JavaScript-клиентов от потери точности. Возьмите `trace_id` из журнала «Уведомлений» и откройте трассу в Jaeger: http://127.0.0.1:16686 → Search, или скриптом из `labs/README.md` через `/api/traces/<trace_id>`. В трассе семь спанов, и по ним видна вся цепочка:

```
orders         POST /v1/orders                           (otelhttp, gateway)
orders         orders.v1.OrderService/CreateOrder        клиент: gateway → свой gRPC
orders         orders.v1.OrderService/CreateOrder        сервер
orders         warehouse.v1.WarehouseService/Reserve     клиент
warehouse      warehouse.v1.WarehouseService/Reserve     сервер
orders         orders.events publish                     relay, через ~0,5 с
notifications  orders.events process                     консьюмер
```

Спан публикации — ребёнок серверного `CreateOrder`, хотя relay работает в другой горутине и позже. Контекст трассы сохранён в строке outbox вместе с событием. Спан обработки — ребёнок публикации: `traceparent` доехал в заголовке записи Kafka.

**2. Метрики.** Prometheus — http://127.0.0.1:9091, вкладка Status → Targets: все три цели в состоянии `up`. Запросы для начала:

```promql
sum by (job, grpc_code) (rate(grpc_server_handled_total[1m]))   # RED: поток и ошибки
histogram_quantile(0.99, sum by (le, grpc_method) (rate(grpc_server_handling_seconds_bucket[5m])))
sum by (result) (warehouse_reservations_total)
orders_outbox_lag_seconds          # главный алерт relay: событие застряло
notifications_dlq_total            # повод разбудить человека
```

Напрямую — служебные порты `:8190`, `:8191`, `:8192`, путь `/metrics`.

**3. Склад упал.**

```sh
docker compose stop warehouse
curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 7, "items": [{"product_id": 1, "quantity": 1}]}'
curl -s localhost:8190/metrics | grep -E '^orders_warehouse_(breaker_state|retries_total|releases_total)'
curl -s localhost:8090/v1/orders/1         # чтение работает без склада
docker compose start warehouse
```

Ответ — `503` с кодом `UNAVAILABLE` и `RetryInfo` в `details`: клиенту сообщают, когда повторить. Внутри «Заказы» сделали три попытки `Reserve`, а потом вызвали компенсирующий `Release`: исход резерва неизвестен. Выключатель считает и неудачи `Release`, поэтому размыкается уже на этом первом запросе: 3 попытки `Reserve` плюс 2 попытки `Release` — это 5 неудач подряд. Следующие заказы отклоняются мгновенно, без обращения к сети. Это видно по `orders_warehouse_breaker_state 1` и по тому, что `retries_total` больше не растёт. Через 5 секунд после того, как склад поднялся, пробный вызов замыкает выключатель. Чтение и отмена заказов всё это время работают: склад нужен только для резерва.

**4. Компенсация сбоя записи** (резерв сделан, заказ не записался) на стенде не вызвать — для этого базу пришлось бы ронять ровно между двумя шагами. Её проверяют тесты: `labs/orders/internal/e2e/saga_test.go` (настоящий «Склад» из `warehousetest` и Kafka на `kfake`) и тесты `labs/orders/internal/orders`. Запустите их сами — `go test -race ./...` в каталоге сервиса, Docker для этого не нужен.

### Пример: вся цепочка в одном процессе

Песочница не видит модули `labs/`, поэтому пример — компактная модель системы. Два gRPC-сервера на `bufconn`, «Заказы» с SQLite и outbox, Kafka на `kfake`, консьюмер «Уведомлений» в группе с ручным коммитом. Трассы собирает экспортёр в памяти, метрики — свой реестр Prometheus. Склад «падает» по флагу в interceptor, а время выключателя — фейковые часы. Поэтому вывод детерминирован, а «прошло 5 секунд» занимает ноль.

Сценарий повторяет стенд. Шаг 3 — компенсация сбоя записи, которую на стенде не вызвать. На шаге 4 видно, как выключатель размыкается посреди компенсации. Шаг 7 — at-least-once целиком: relay «падает» между `ProduceSync` и отметкой, событие уходит дважды, а консьюмер отсекает дубль по `event-id`. Ядовитая запись уходит в DLQ. В трассе заказа 1 обе публикации и обе обработки — дети одного запроса, на второй висит событие `duplicate`.

Чего в модели нет по сравнению с `labs/`: gateway и валидации protovalidate, пауз с jitter между повторами, повторов обработки перед DLQ, `processed_events` в базе (здесь — map), неоднозначного COMMIT. Всё это есть в коде, ссылки — в таблице выше.

### Чек-лист продакшн-готовности

Пройдите по своему сервису, прежде чем выпускать его к пользователям. В скобках — как это сделано в `labs/` и где об этом урок.

**Контракт и API**

- Контракт в buf-модуле, `buf lint` и `buf breaking` в CI; несовместимое — только через новый пакет `v2` (`labs/api`; 11/02–03).
- Валидация по контракту на сервере, один interceptor на все методы (11/04).
- Ошибки — коды и детали, переведённые в одном месте; `INTERNAL` наружу без текста, подробности — в журнал с `request_id` (11/08).
- Повторяемые операции идемпотентны: `Reserve`/`Release` — по номеру заказа (12/11). Для `CreateOrder` клиента — **нет** (упражнение).

**Устойчивость**

- У каждого исходящего вызова есть дедлайн, и он не длиннее входящего (12/07).
- Повторяются только безопасные коды, с backoff и full jitter; дедлайн важнее числа попыток (12/05).
- Выключатель на каждого соседа; бизнес-отказы не размыкают его (12/06).
- Готовность (`/readyz`) проверяет только то, без чего сервис не может отвечать: базу — да, Kafka — нет, outbox подождёт (12/12).
- Работа, которая должна пережить запрос (компенсация, коммит офсетов при остановке), идёт на `context.WithoutCancel` со своим таймаутом (12/07, 12/09).

**Данные и события**

- Изменение и событие пишутся одной транзакцией (outbox); relay отмечает после подтверждения брокера (10/10, 12/11).
- Порядок по ключу сохраняется: ключ записи — сущность, relay один (advisory lock) (12/08, 12/11).
- Консьюмер коммитит после обработки; дедупликация — в той же транзакции, что и результат; ядовитое — в DLQ с причиной (12/09–10).
- Миграции встроены в бинарник; в проде — отдельным шагом деплоя под ролью-владельцем схемы (10/08).

**Наблюдаемость**

- JSON-журнал с `trace_id`/`request_id`, секреты как `[REDACTED]` (12/02).
- Трасса проходит через gRPC и Kafka; сэмплирование решает корень (12/03).
- RED по каждому методу, метки — с ограниченным набором значений; алерты на `orders_outbox_lag_seconds`, `notifications_dlq_total`, `orders_warehouse_releases_total{result="failed"}` (12/04).
- Лаг консьюмера снимается снаружи — kafka-exporter или Burrow (12/09).

**Безопасность и эксплуатация**

- Таймауты HTTP-сервера, лимит тела, `MaxRecvMsgSize`, `MaxConcurrentStreams`, keepalive enforcement (08/08, 09/10, 11/05).
- Образ `scratch`/distroless, не root, `read_only`; секреты — из окружения или файла, не в образе (12/12, 09/11).
- Мягкая остановка в правильном порядке, `terminationGracePeriodSeconds` больше её суммы (12/12).
- **Нет на стенде:** TLS/mTLS между сервисами, аутентификации пользователей, отдельных ролей БД у «Заказов». Это следующие пункты.

### Что доработать самому

Упражнения на `labs/` — в своей ветке, со своими тестами. Ни одно не требует фреймворков: всё нужное уже есть в модулях и в `labs/platform`.

1. **mTLS между сервисами.** Свой CA и сертификаты сервисов (для стенда — генерация при старте или `make certs`; в проде — cert-manager или service mesh). `grpcx.ServerConfig.Creds = credentials.NewTLS(&tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, ...})`, на клиенте — свой `RootCAs` и сертификат клиента. «Склад» проверяет, что `Reserve` вызывает именно «Заказы»: SAN из `credentials.TLSInfo` в `peer` (11/12). Тест — на `bufconn` с сертификатами в памяти.
2. **Фоновые повторы компенсации.** Сейчас неудавшийся `Release` виден только в журнале и метрике. Запишите его в таблицу `pending_releases` (или тем же outbox — командой) и повторяйте воркером до успеха. Задача урока — ядро этого механизма. Не забудьте метрику размера очереди и алерт на её возраст.
3. **TTL резерва на складе.** Защита с другой стороны: резерв, который «Заказы» не подтвердили за N минут, снимается сам. Для этого нужно подтверждение — новый метод `Confirm`. А значит, эволюция контракта `warehouse.v1` без поломки клиентов (11/02).
4. **Уборка outbox и надгробий.** Опубликованные строки outbox и надгробия «Склада» копятся вечно. Периодическое удаление пачками (`DELETE … WHERE id IN (SELECT … LIMIT 1000)`) или партиционирование по времени (`DROP PARTITION` дешевле `DELETE`). Надгробие можно удалять, когда повтор `Reserve` уже невозможен: через срок больше всех таймаутов и повторов клиента.
5. **Ключ идемпотентности у клиента.** Повтор `POST /v1/orders` после таймаута сейчас создаёт второй заказ. Ключ — из заголовка `Idempotency-Key` или metadata, как в задаче 11/14. Таблица `(key, request_hash, order_id)`: тот же ключ и тело — тот же заказ, тот же ключ и другое тело — `409`/`ALREADY_EXISTS`.
6. **Номера и доступ.** Последовательные `int64` номера заказов наружу — это готовый IDOR (09/01): `GET /v1/orders/2` читает чужой заказ, а по номерам видно число заказов в день. Нужен auth-interceptor с `customer_id` из токена и проверкой владельца в каждом методе. Наружу лучше отдавать непредсказуемый идентификатор, а внутренний `int64` оставить ключом идемпотентности.
7. **Отдельные роли БД и TLS до базы.** Миграции — Job под ролью-владельцем схемы, сервис — роль только с `SELECT/INSERT/UPDATE`, `sslmode=verify-full`, пароль — из файла Secret (12/12, 09/11).
8. **Нагрузка и SLO.** Сформулируйте SLO (например, 99 % `CreateOrder` за 300 мс) и проверьте его нагрузкой (k6, vegeta, ghz для gRPC). Посмотрите, что сломается первым — пул PostgreSQL, горячая строка `stock` или relay. Это мостик к модулю 14.

## Как это в C#

- **Сборка системы.** В .NET тот же стенд описывает Aspire AppHost — проекты, базы, Kafka и дашборд с трассами в одном C#-файле, а `AddServiceDefaults()` подключает OpenTelemetry, health checks и service discovery. В Go это `docker-compose.yml` и функция `run` каждого сервиса: зависимости видны явно, а не появляются из соглашений.
- **Устойчивость.** `AddStandardResilienceHandler()` из Microsoft.Extensions.Http.Resilience (Polly v8) даёт таймаут, повторы и circuit breaker одной строкой. В `labs/` это `resilience.Retry` и `resilience.Breaker` на 300 строк вместе с комментариями. Результат тот же, но то, что выключатель стоит внутри повторов, а бизнес-отказы его не размыкают, здесь видно в коде, а не в настройках по умолчанию.
- **Outbox и консьюмеры.** MassTransit делает outbox (`AddEntityFrameworkOutbox` + `UseBusOutbox`), повторы и `_error`-очереди конфигурацией, а Kafka подключает через Rider. В Go то же — relay и цикл консьюмера на `franz-go`, по сотне строк каждый. Учтите и лицензию: MassTransit начиная с v9 коммерческий, открытой осталась v8.
- **Компенсация.** Ни одна библиотека не решит за вас, какой ответ соседа требует компенсации. Есть и разница в механике. В Go значения запроса и отмена едут в одном `ctx`, поэтому нужен `context.WithoutCancel`. В .NET трасса течёт через `Activity.Current` (AsyncLocal) отдельно от `CancellationToken`, и для компенсации достаточно передать новый токен со своим таймаутом.

## Ловушки

### Компенсация на контексте запроса

```go
if err := wh.Reserve(ctx, id, items); err != nil {
	wh.Release(ctx, id) // клиент ушёл — ctx отменён, Release не выполнится
	return err
}
```

Чаще всего `Reserve` падает именно потому, что истёк дедлайн или клиент ушёл, и `Release` на том же `ctx` тут же получает `context.Canceled`. Резерв висит. Компенсации нужен контекст без отмены, но со значениями (трасса, `request_id`) и своим таймаутом: `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)`.

### «Ошибка — значит, ничего не произошло»

```go
if err := wh.Reserve(ctx, id, items); err != nil {
	return err // DEADLINE_EXCEEDED: склад мог успеть зарезервировать
}
```

Ошибка вызова по сети означает «не знаю», а не «нет». Точный отказ — это ответ сервера с бизнес-кодом (`FAILED_PRECONDITION`, `INVALID_ARGUMENT`). Всё остальное требует компенсации или проверки. Компенсация безопасна, только если она идемпотентна и не даёт опоздавшему `Reserve` выполниться после неё: отсюда «надгробие» в «Складе».

### Метка с номером заказа

```go
// ОШИБКА: ряд на каждый заказ — память Prometheus растёт без предела
releases.WithLabelValues(strconv.FormatInt(orderID, 10), reason).Inc()
```

Номер заказа, покупатель, текст ошибки — в журнал и в атрибуты спана. В метки идут только значения из короткого известного списка: `reason`, `result`, код gRPC (12/04).

## Задание

Напишите ядро саги создания заказа так, чтобы компенсация не терялась: если склад не ответил на `Release`, возврат резерва ставится в очередь, а `RetryReleases` позже его доделывает. Это упражнение 2 из списка выше в миниатюре. Склад и база — интерфейсы, тесты подставляют фейки.

```go
var (
	ErrInvalid       = errors.New("orders: invalid request")
	ErrOutOfStock    = errors.New("orders: out of stock")
	ErrUnavailable   = errors.New("orders: warehouse unavailable")
	ErrCommitUnknown = errors.New("orders: commit outcome unknown") // возвращает Store.Save
)

const (
	MaxItems              = 100
	MaxQuantity           = 1000
	DefaultReleaseTimeout = 5 * time.Second
)

type Item struct {
	ProductID int64
	Quantity  int
}

type Order struct {
	ID         int64
	CustomerID int64
	Items      []Item
}

type Warehouse interface {
	Reserve(ctx context.Context, orderID int64, items []Item) error
	Release(ctx context.Context, orderID int64) error
}

type Store interface {
	NextID(ctx context.Context) (int64, error)
	Save(ctx context.Context, o Order) error // заказ + outbox одной транзакцией
	Exists(ctx context.Context, id int64) (bool, error)
	AddPendingRelease(ctx context.Context, orderID int64) error
	PendingReleases(ctx context.Context) ([]int64, error)
	RemovePendingRelease(ctx context.Context, orderID int64) error
}

type Saga struct {
	Store          Store
	Warehouse      Warehouse
	ReleaseTimeout time.Duration // <= 0 — DefaultReleaseTimeout
}

func (s *Saga) CreateOrder(ctx context.Context, customerID int64, items []Item) (Order, error)
func (s *Saga) RetryReleases(ctx context.Context) (int, error)
```

Ниже «отвязанный контекст» — это `context.WithTimeout(context.WithoutCancel(ctx), timeout)`, где `timeout` — `ReleaseTimeout`, а если он `<= 0` (ноль или отрицательный) — `DefaultReleaseTimeout`. Такой контекст хранит значения `ctx`, не отменяется вместе с ним и имеет свой дедлайн. На каждый вызов, которому он нужен, создаётся свежий. «Ошибка находит X» значит `errors.Is(err, X) == true`. При любой ошибке `CreateOrder` возвращает нулевой `Order{}`. Отдельно `ctx.Err()` в `CreateOrder` не проверяется: `ctx` просто передаётся в `NextID`, `Reserve` и `Save`, а реагировать на отмену — их дело.

`CreateOrder`:

1. **Проверка.** Ошибка, которая находит `ErrInvalid`, если `customerID <= 0`, `items` пуст (или `nil`) или длиннее `MaxItems`, у позиции `ProductID <= 0` или `Quantity` вне `1..MaxQuantity`, либо товар повторяется. В этом случае ни один метод `Store` и `Warehouse` не вызывается.
2. **Позиции** — копия `items`, отсортированная по возрастанию `ProductID`. Переданный срез не меняется. Именно эта копия уходит в `Reserve` и `Save` и возвращается в `Order.Items`.
3. `Store.NextID(ctx)` — ровно один раз. Ошибка — вернуть ошибку, которая её находит, больше ничего не вызывая.
4. `Warehouse.Reserve(ctx, id, позиции)` — ровно один раз (повторы — забота клиента склада). Ошибку классифицируйте по её статусу gRPC: `status.Code(err)` даёт код, `status.FromError(err)` (или `status.Convert(err)`) — статус с деталями (`Details()`). Обе функции разворачивают и обёрнутые через `%w` статусы. У ошибки без статуса gRPC код — `Unknown`:
   - `FailedPrecondition` с деталью `*errdetails.ErrorInfo`, у которой `Reason == "OUT_OF_STOCK"`, — точный отказ. Вернуть ошибку, которая находит `ErrOutOfStock` и ошибку `Reserve`. Компенсации нет;
   - другой `FailedPrecondition` или `InvalidArgument` — тоже точный отказ: ошибка находит ошибку `Reserve`, но не `ErrOutOfStock` и не `ErrUnavailable`. Компенсации нет;
   - любая другая ошибка — исход неизвестен, нужна **компенсация** (п. 6). Ошибка находит ошибку `Reserve`, а если код статуса — `Unavailable` или `DeadlineExceeded`, то ещё и `ErrUnavailable`. Голые `context.Canceled` и `context.DeadlineExceeded` без статуса gRPC (код `Unknown`) — тоже компенсация, но `ErrUnavailable` они **не** находят.
5. `Store.Save(ctx, Order{ID: id, CustomerID: customerID, Items: позиции})` — ровно один раз:
   - успех — вернуть этот заказ и `nil`;
   - ошибка, которая находит `ErrCommitUnknown`, — вызвать `Store.Exists` с отвязанным контекстом. `true` — заказ записан: вернуть его и `nil`, без компенсации. `false` — компенсация. Ошибка `Exists` — компенсации нет и в очередь ничего не ставится (неизвестно, есть ли заказ, а резерв без заказа лучше, чем заказ без резерва). Ошибка результата находит и ошибку `Save`, и ошибку `Exists`;
   - любая другая ошибка — компенсация. `Exists` не вызывается.

   Во всех неуспешных случаях ошибка находит ошибку `Save`.
6. **Компенсация**: `Warehouse.Release(отвязанный контекст, id)` — ровно один раз. Если `Release` вернул ошибку — `Store.AddPendingRelease(свежий отвязанный контекст, id)`. Ошибка `CreateOrder` в этом случае находит, кроме ошибки шага, ошибку `Release` и ошибку `AddPendingRelease`, если она была. Успешная компенсация в ошибку ничего не добавляет.

`RetryReleases`:

- `PendingReleases(ctx)`; ошибка — вернуть `0` и ошибку, которая её находит, не вызывая `Release`;
- номера обрабатываются в полученном порядке. Перед каждым проверяется `ctx.Err()`: если `ctx` отменён — остановиться, не вызывая `Release` для этого и следующих номеров. Счётчик тогда равен числу номеров, выполненных до остановки, а итоговая ошибка находит `ctx.Err()` (и ошибки, накопленные до неё);
- `Warehouse.Release` вызывается с контекстом-потомком `ctx` (не отвязанным: остановка воркера должна его прерывать) с таймаутом `ReleaseTimeout`/`DefaultReleaseTimeout`. Успех — `RemovePendingRelease(ctx, id)`. Ошибка `Release` или `RemovePendingRelease` не останавливает проход: номер остаётся в очереди, ошибка копится;
- результат — число номеров, для которых удались и `Release`, и `RemovePendingRelease`, и `errors.Join` накопленных ошибок (`nil`, если их не было).

`Saga` не хранит изменяемого состояния; `CreateOrder` могут вызывать одновременно из многих горутин.
