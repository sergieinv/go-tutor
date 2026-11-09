# labs — итоговый проект модулей 11–12

Система из трёх сервисов. Она выросла из сервиса «Заказы» модуля 10 и
написана так, как пишут продакшн-сервисы на Go. Здесь нет DI-контейнера,
Microsoft.Extensions.* и фреймворков: зависимости собираются явно в
`main`, бинарники маленькие и стартуют за доли секунды.

| Каталог | Модуль | Что |
|---|---|---|
| [`api/`](api) | `gotutor/labs/api` | контракты `.proto` (buf) и сгенерированный Go-код |
| [`platform/`](platform) | `gotutor/labs/platform` | общий каркас: журнал, трассировка, gRPC/HTTP-серверы, пробы, повторы, выключатель, Kafka-заголовки |
| [`orders/`](orders) | `gotutor/labs/orders` | «Заказы»: gRPC + REST, PostgreSQL, сага, outbox → Kafka |
| [`warehouse/`](warehouse) | `gotutor/labs/warehouse` | «Склад»: остатки и резервы с блокировками, MySQL |
| [`notifications/`](notifications) | `gotutor/labs/notifications` | «Уведомления»: консьюмер Kafka, дедупликация, DLQ |
| [`initdb/`](initdb), [`prometheus/`](prometheus) | — | init-скрипты баз и конфигурация Prometheus для профиля labs |

## Как устроено

```mermaid
flowchart LR
    client([curl / клиент]) -- "REST :8090<br/>gRPC :50051" --> orders
    subgraph orders [Заказы]
        gw[grpc-gateway] --> og[gRPC OrderService] --> saga[сага]
        relay[relay outbox]
    end
    saga -- "gRPC Reserve / Release<br/>дедлайн, повторы, выключатель" --> wh[Склад<br/>gRPC WarehouseService]
    saga -- "заказ + outbox<br/>одна транзакция" --> pg[(PostgreSQL<br/>orders)]
    relay -- читает outbox --> pg
    wh --> my[(MySQL<br/>warehouse)]
    relay -- "orders.events<br/>ключ = order_id" --> kafka[[Kafka]]
    kafka -- consumer group --> notif[Уведомления]
    notif --> sq[(SQLite<br/>обработанные события)]
    notif -- ядовитые --> dlq[[notifications.dlq]]
    orders & wh & notif -. OTLP .-> jaeger[Jaeger :16686]
    prom[Prometheus :9091] -. /metrics .-> orders & wh & notif
```

- **У каждого сервиса своя база.** Общих таблиц нет. Каталог и цены
  хранятся у «Заказов», остатки — у «Склада», обработанные события — у
  «Уведомлений». Связаны сервисы только контрактами из `api/`.
- **Синхронно — то, без чего нельзя ответить клиенту.** Резерв товара —
  gRPC-вызов «Склада» с дедлайном. **Асинхронно — всё остальное.** Событие
  «заказ создан» уходит в Kafka через transactional outbox, и
  «Уведомления» обрабатывают его в своём темпе. Если «Уведомления» лягут,
  заказы всё равно создаются.
- **Сага вместо распределённой транзакции.** Резерв → запись заказа → при
  сбое компенсация `Release`. Каждый шаг идемпотентен по номеру заказа.
  Подробнее — в [`orders/README.md`](orders/README.md#сага-создания-заказа).
- **Доставка at-least-once.** Relay отмечает событие только после
  подтверждения брокера. Консьюмер коммитит офсет только после обработки и
  отсекает дубли по `event_id`.

## Запуск

Из корня репозитория:

```sh
docker compose --profile labs up --build
```

Вместе с PostgreSQL, MySQL и Kafka поднимутся `orders`, `warehouse`,
`notifications`, `jaeger` и `prometheus`. Базы и топики живут в tmpfs: это
учебный стенд, после перезапуска он пуст. Миграции, демо-данные и топики
сервисы создают сами при старте. Демо-каталог: 1 — «Клавиатура» (10 шт.),
2 — «Мышь» (25 шт.), 3 — «Монитор» (3 шт.).

| Адрес (только 127.0.0.1) | Что |
|---|---|
| http://127.0.0.1:8090 | REST «Заказов» (`/v1/orders…`, `/healthz`, `/readyz`) |
| 127.0.0.1:50051 | gRPC «Заказов» (reflection включён) |
| http://127.0.0.1:8091 | REST «Склада» — только `GET /v1/stock/{product_id}` |
| 127.0.0.1:50052 | gRPC «Склада» (reflection включён) |
| http://127.0.0.1:8190, :8191, :8192 | служебные порты orders / warehouse / notifications: `/metrics`, `/healthz`, `/readyz` |
| http://127.0.0.1:16686 | Jaeger UI |
| http://127.0.0.1:9091 | Prometheus UI |

## Сценарий: заказ через всю систему

```sh
# 1. Остаток монитора на складе: available 3, reserved 0
curl -s localhost:8091/v1/stock/3

# 2. Заказ: два монитора и клавиатура. В ответе — заказ со статусом
#    ORDER_STATUS_RESERVED, в заголовке X-Request-Id — id запроса.
curl -si -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 42, "items": [{"product_id": 3, "quantity": 2}, {"product_id": 1, "quantity": 1}]}'

# 3. Резерв на складе: available 1, reserved 2
curl -s localhost:8091/v1/stock/3

# 4. Событие дошло до «Уведомлений» (через Kafka, обычно меньше секунды).
#    В записи журнала есть trace_id — он же в Jaeger.
docker compose logs notifications | grep 'notification sent'
#   {"msg":"notification sent","order_id":1,"customer_id":42,"kind":"order_created",
#    "text":"Заказ 1 оформлен на 54 970,00 ₽","trace_id":"b48f…","span_id":"…"}

# 5. Трасса через три сервиса: Jaeger UI → Search → service orders, или API.
#    Подставьте trace_id из журнала:
curl -s localhost:16686/api/traces/<trace_id> | python3 -c '
import sys, json
t = json.load(sys.stdin)["data"][0]
names = {k: v["serviceName"] for k, v in t["processes"].items()}
for s in sorted(t["spans"], key=lambda s: s["startTime"]):
    print(names[s["processID"]].ljust(14), s["operationName"])'
#   orders         POST /v1/orders
#   orders         orders.v1.OrderService/CreateOrder        (клиент gateway → gRPC)
#   orders         orders.v1.OrderService/CreateOrder        (сервер)
#   orders         warehouse.v1.WarehouseService/Reserve      (клиент)
#   warehouse      warehouse.v1.WarehouseService/Reserve      (сервер)
#   orders         orders.events publish                      (relay, позже)
#   notifications  orders.events process                      (консьюмер)

# 6. Метрики: в Prometheus (http://127.0.0.1:9091, Status → Targets — все три up)
#    или напрямую со служебных портов
curl -s localhost:8191/metrics | grep '^warehouse_reservations_total'
curl -s localhost:8190/metrics | grep '^orders_outbox'
curl -s localhost:8192/metrics | grep '^notifications_events_total'
#    запросы в Prometheus: rate(grpc_server_handled_total[1m]),
#    orders_outbox_lag_seconds, sum by (result) (warehouse_reservations_total)

# 7. Не хватает товара: 400, FAILED_PRECONDITION, в details — ErrorInfo
#    OUT_OF_STOCK и PreconditionFailure "products/3". Резерва нет, остатки целы.
curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 7, "items": [{"product_id": 3, "quantity": 2}]}'

# 8. Отмена: заказ cancelled, резерв возвращён (available 3), в «Уведомлениях» —
#    «Заказ 1 отменён». Повторная отмена возвращает тот же заказ.
curl -s -X POST localhost:8090/v1/orders/1:cancel
curl -s localhost:8091/v1/stock/3
docker compose logs notifications | grep order_cancelled
```

Сбой соседа и компенсация:

```sh
docker compose stop warehouse
# 503 UNAVAILABLE с RetryInfo. «Заказы» сделали три попытки Reserve и вызвали
# компенсирующий Release: исход Reserve неизвестен. После пяти неудач подряд
# выключатель размыкается, и следующие запросы отклоняются мгновенно.
curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 7, "items": [{"product_id": 1, "quantity": 1}]}'
curl -s localhost:8190/metrics | grep -E '^orders_warehouse_(breaker_state|retries_total|releases_total)'
curl -s localhost:8090/v1/orders/1        # чтение работает без склада
docker compose start warehouse            # через ~5 с пробный вызов замкнёт выключатель
```

Компенсацию сбоя *записи* (резерв сделан, заказ не записался) на стенде
вызвать трудно. Её проверяют тесты: `labs/orders/internal/e2e` (настоящий
«Склад» в памяти и Kafka в процессе) и `labs/orders/internal/orders`.

gRPC напрямую (нужен [grpcurl](https://github.com/fullstorydev/grpcurl)):

```sh
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext -d '{"order_id": 1}' localhost:50051 orders.v1.OrderService/GetOrder
grpcurl -plaintext -d '{"order_id": 999, "items": [{"product_id": 1, "quantity": 1}]}' \
  localhost:50052 warehouse.v1.WarehouseService/Reserve
```

## Тесты

В каждом модуле — `go test -race ./...`. Брокер для тестов не нужен:
Kafka поднимается в процессе (`kfake`), gRPC работает через `bufconn`.
Тесты на настоящих базах включаются переменными, у каждого подтеста своя
база с префиксом сервиса, которая удаляется после теста:

```sh
export ORDERS_TEST_POSTGRES='postgres://postgres:labs@127.0.0.1:55432/postgres?sslmode=disable'
export WAREHOUSE_TEST_MYSQL='root:labs@tcp(127.0.0.1:53306)/'
for m in api platform orders warehouse notifications; do (cd labs/$m && go vet ./... && go test -race -count=3 ./...); done
```

## Безопасность

Что сделано (принцип «безопасно по умолчанию»):

- **Таймауты и лимиты.** У HTTP-серверов `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`,
  дедлайн обработки запроса доходит до gRPC и драйвера базы, тело REST
  ограничено 64 КиБ. У gRPC входящее сообщение ограничено 256 КиБ, на
  соединение — `MaxConcurrentStreams`, keepalive enforcement защищает от
  пинг-флуда, вызов без дедлайна получает дедлайн по умолчанию.
- **Ошибки наружу без деталей.** `INTERNAL` всегда приходит как
  «internal error». Тексты `UNAVAILABLE` от транспорта (там бывают
  внутренние адреса) gateway заменяет. Подробности пишутся только в журнал
  вместе с `request_id`. Внутренние metadata gRPC не становятся
  HTTP-заголовками.
- **Журнал.** DSN и пароли имеют тип `logging.Secret` и выводятся как
  `[REDACTED]`. Входящий `X-Request-ID` принимается, только если похож на
  id: подделать запись журнала переводом строки не получится. `traceparent`
  внешнего REST-клиента не становится родителем трассы, а прикрепляется
  ссылкой.
- **Образы.** Multi-stage сборка, `scratch`, uid 65532, `read_only` в
  compose, порты только на 127.0.0.1. В сеть sandbox (программы учеников)
  сервисы не входят.
- **Роли БД.** «Склад» ходит в MySQL пользователем `warehouse` с правами
  только на свою базу ([`initdb/mysql/20-warehouse.sql`](initdb/mysql/20-warehouse.sql)).
  Вход этим пользователем из подсети песочницы заблокирован, как и для
  root.

Чего нет — честно:

- **TLS/mTLS между сервисами.** gRPC, OTLP и Kafka внутри docker-сети идут
  открытым текстом. Платформа к этому готова: `grpcx.ServerConfig.Creds` и
  `ClientConfig.Creds` принимают `credentials.NewTLS(...)`. На стенде
  сертификаты не генерируются — это отдельная инфраструктура (свой CA,
  ротация), и её показывает урок 11-grpc/12 на сертификатах в памяти. В
  проде mTLS обычно даёт service mesh (Istio, Linkerd) или
  cert-manager + `credentials.NewTLS` с `ClientAuth: RequireAndVerifyClientCert`.
  Идентичность клиента берётся из сертификата (`peer` / `credentials.TLSInfo`).
- **Аутентификации пользователей.** `customer_id` берётся из запроса.
  Сервисы нельзя выставлять наружу.
- **«Заказы» ходят в PostgreSQL суперпользователем** (`postgres`). Это
  наследие модуля 10 и допущение стенда. В проде миграции накатываются
  отдельным шагом под ролью-владельцем схемы, сервис ходит ролью с
  `SELECT/INSERT/UPDATE` на свои таблицы через `sslmode=verify-full`, а
  пароли берутся из менеджера секретов (в k8s — Secret, смонтированный
  файлом), а не из `docker-compose.yml`.

## Проверка контрактов в CI

```sh
cd labs/api
buf lint
buf breaking --against '../../.git#branch=main,subdir=labs/api'
```

Подробнее — в [`api/README.md`](api/README.md).
