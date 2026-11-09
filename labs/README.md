# labs — итоговый проект модулей 11–12 (и нагрузка модуля 14)

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
| [`loadgen/`](loadgen) | `gotutor/labs/loadgen` | генератор нагрузки и нагрузочный разбор (модуль 14) |
| [`gameserver/`](gameserver) | `gotutor/labs/gameserver` | игровой сервер модуля 15: Minecraft 1.21.1 + WebSocket-шлюз, нагрузочные боты |
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
    og -- "каталог (кеш, модуль 14)" --> vk[(Valkey)]
    og -- "история (модуль 14)" --> rep[(PostgreSQL<br/>реплика)]
    pg -. WAL .-> rep
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

Вместе с PostgreSQL (и её репликой), MySQL, Kafka и Valkey поднимутся
`orders`, `warehouse`, `notifications`, `jaeger` и `prometheus`. Базы и топики живут в tmpfs: это
учебный стенд, после перезапуска он пуст. Миграции, демо-данные и топики
сервисы создают сами при старте. Демо-каталог: 1 — «Клавиатура» (10 шт.),
2 — «Мышь» (25 шт.), 3 — «Монитор» (3 шт.), 4 — «Кабель USB-C» (миллион
штук, для нагрузочных прогонов модуля 14).

| Адрес (только 127.0.0.1) | Что |
|---|---|
| http://127.0.0.1:8090 | REST «Заказов» (`/v1/orders…`, `/healthz`, `/readyz`) |
| 127.0.0.1:50051 | gRPC «Заказов» (reflection включён) |
| http://127.0.0.1:8091 | REST «Склада» — только `GET /v1/stock/{product_id}` |
| 127.0.0.1:50052 | gRPC «Склада» (reflection включён) |
| http://127.0.0.1:8190, :8191, :8192 | служебные порты orders / warehouse / notifications: `/metrics`, `/healthz`, `/readyz`; у orders ещё `/admin/features` (модуль 14) |
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
# модуль 14: реплика (read-your-writes на настоящей потоковой реплике)
export ORDERS_TEST_POSTGRES_REPLICA='postgres://postgres:labs@127.0.0.1:55433/postgres?sslmode=disable'
for m in api platform orders warehouse notifications loadgen gameserver; do (cd labs/$m && go vet ./... && go test -race -count=3 ./...); done
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

## Модуль 14: нагрузка

Модуль 14 («Высокие нагрузки») встраивает в «Заказы» то, что его уроки
разбирают по отдельности. Всё включено в `docker-compose.yml`, подробности
и переменные окружения — в [`orders/README.md`](orders/README.md#модуль-14-нагрузка).

| Что | Как устроено | Видно в метриках |
|---|---|---|
| Кеш каталога | `GET /v1/products/{id}` и цены в CreateOrder читаются через Valkey: cache-aside, TTL с джиттером, singleflight на ключ (защита от stampede), отрицательный кеш «нет такого товара», деградация в базу при недоступном Valkey | `orders_catalog_cache_lookups_total{result}`, `orders_catalog_db_queries_total` |
| Load shedding | CreateOrder: не больше N одновременно и короткая очередь; не дождался места — сразу `RESOURCE_EXHAUSTED` с `RetryInfo` (REST — 429 и `Retry-After`) | `orders_shed_rejected_total`, `orders_shed_in_flight` |
| История с реплики | ListOrders читает с `postgres-replica`; read-your-writes — токен с позицией WAL из ответа CreateOrder/CancelOrder (`consistency_token`) | `orders_history_reads_total{target,reason}` |
| Шарды по покупателю | по выбору: `docker-compose.shards.yml` — заказы покупателя на шарде `jump_hash(customer_id)`, номер шарда в старших битах номера заказа | `orders_outbox_*{shard}` |

### Нагрузочный разбор одной командой

При запущенном профиле `labs`:

```sh
docker compose --profile labs run --rm loadgen analyze
```

Сервис `loadgen` — в своём профиле `loadtest`: `up` его не запускает,
`run` включает профиль сам. Он ходит в «Заказы» по сети docker (`orders:8080`)
и переключает кеш и реплику через служебный эндпоинт
`orders:8081/admin/features` — без пересборки и перезапуска контейнера.
Разбор идёт около минуты:

1. **база** — кеш и реплика выключены: каждый запрос каталога доходит до
   PostgreSQL, вся история читается с основной базы;
2. **+кеш** — запросы каталога к базе почти исчезают (остаются промахи по
   несуществующим товарам — отрицательный кеш спасает только от повторов — и
   раз в TTL);
3. **+реплика** — история уходит на реплику; проверка read-your-writes
   показывает, что без токена только что созданный заказ в истории не
   виден (реплика отстаёт на 200 мс), а с токеном виден всегда;
4. **перегрузка** — только заказы, 1500/с: часть получает 429 быстро, а
   задержка принятых остаётся ограниченной.

Каждый шаг печатает отчёт генератора (запросы, исходы по кодам, p50–p99.9,
max) и разницу метрик «Заказов» за шаг, в конце — сводная таблица:

```
шаг            сцен/с   каталог   каталог      заказ    история    5xx/    429    400   БД кат.      кеш  реплика    не видно
                            p50       p99        p99        p99    сеть                   в сек      hit     доля  без/с ток.
1 база            150    1.22ms    3.90ms     9.47ms     4.35ms       0      0      0     105.1        —       0%         0/0
2 +кеш            150    1.22ms    3.84ms     9.47ms     4.48ms       0      0      0       4.7      96%       0%         0/0
3 +реплика        150    1.18ms    3.39ms     9.21ms     4.03ms       0      0      0       3.3      97%      81%       215/0
4 перегрузка     1437         —         —      117ms          —       0   3051      0       0.0     100%        —           —
```

Числа зависят от машины и от того, что ещё на ней работает: на ноутбуке с
маленькими таблицами база отвечает за доли миллисекунды, и разница в
задержках между шагами невелика. Смотрите на направление: «БД кат. в сек»
падает почти до нуля, «реплика доля» растёт, «не видно без токена» > 0,
«с токеном» = 0, в перегрузке появляются 429. Как читать отчёт и что
настраивается флагами — в [`loadgen/README.md`](loadgen/README.md).

Ограничения стенда: реплика одна (и только у шарда 0), задержка её
нарочно увеличена; кеш не инвалидируется при смене цены (обновится по TTL);
переключатели `/admin/features` без аутентификации — только служебный порт
на 127.0.0.1 и сеть docker `default`.

## Модуль 15: игровой сервер

[`gameserver/`](gameserver) — сквозной кейс модуля 15 и материал его
последнего урока. Это учебный сервер, совместимый с подмножеством
протокола **Minecraft Java Edition 1.21 / 1.21.1 (протокол 767)**, и
WebSocket-шлюз для браузерного клиента сайта. Оба на одном игровом цикле:
игроки Minecraft и браузера в одном мире, видят друг друга и общий чат.

Сервер независим от остальных сервисов: ему не нужны ни базы, ни Kafka.
Поднимать и пересобирать его можно отдельно:

```sh
docker-compose --profile labs up -d --build gameserver
# Minecraft 1.21.1: Мультиплеер → localhost (offline-режим, только локально)
docker-compose --profile labs run --rm gamebots   # 200 ботов: держит ли 20 TPS
```

| Адрес (только 127.0.0.1) | Что |
|---|---|
| 127.0.0.1:25565 | Minecraft |
| ws://127.0.0.1:25580/ws | WebSocket-шлюз браузерного клиента (JSON, проверка Origin) |
| http://127.0.0.1:25590 | служебный: `/metrics`, `/healthz`, `/readyz`, `/debug/pprof/` |

Что внутри:

- одна горутина владеет миром, тик 20 TPS, фазы ввод → обновление →
  рассылка, лимит догоняния при перегрузке;
- на соединение — горутины чтения и записи, ограниченный входящий канал и
  ограниченные очереди клиентов: переполнение означает отключение, тик не
  ждёт;
- interest management на пространственном хеше, событие кодируется один
  раз на протокол, тик без аллокаций, метрики MSPT/TPS;
- проверка ввода и лимиты одинаковые для Minecraft и браузера;
- fuzz декодеров, асинхронные атомарные снапшоты в томе
  `gameserver-data`, graceful shutdown с финальным сохранением.

Ресурсов Mojang в репозитории нет: реестры передаются через known packs
только именами записей. Протокол WebSocket, метрики, нагрузочный отчёт и
чек-лист ручной проверки с настоящим клиентом описаны в
[`gameserver/README.md`](gameserver/README.md).

## Проверка контрактов в CI

```sh
cd labs/api
buf lint
buf breaking --against '../../.git#branch=main,subdir=labs/api'
```

Подробнее — в [`api/README.md`](api/README.md).
