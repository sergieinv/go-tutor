# Сервис «Заказы»

Сквозной учебный сервис курса «Go для C#-разработчика». В модуле 10 он был
монолитом на одной базе. В модулях 11–12 он стал частью системы из трёх
сервисов (обзор — [`labs/README.md`](../README.md)):

- **gRPC** `orders.v1.OrderService` и **REST** из того же контракта
  (grpc-gateway). Контракт лежит в [`labs/api/proto/orders/v1/orders.proto`](../api/proto/orders/v1/orders.proto);
- остатки и резервы переехали в сервис **«Склад»** ([`labs/warehouse`](../warehouse)).
  Создание заказа — **сага**: `warehouse.Reserve` → транзакция PostgreSQL
  (заказ, позиции, событие в outbox) → при сбое записи компенсация
  `warehouse.Release`;
- **relay outbox → Kafka**: события `events.v1.OrderEvent` уходят в топик
  `orders.events`, их читают «Уведомления» ([`labs/notifications`](../notifications));
- наблюдаемость: JSON-журнал с `trace_id`/`request_id`, трассы OpenTelemetry
  в Jaeger, метрики Prometheus;
- модуль 14: кеш каталога в Valkey, load shedding на CreateOrder, история с
  реплики с read-your-writes, шардирование по покупателю (раздел
  [«Модуль 14: нагрузка»](#модуль-14-нагрузка)).

Это не упражнение. Сервис написан так, как его стоит писать в продакшне,
читайте его как пример. Про то, чего в нём нет, — в последнем разделе.

## Структура

```
labs/orders/
├── cmd/orders/main.go       тонкий main → run(ctx, args, getenv, stdout): конфигурация,
│                            сборка зависимостей, три сервера, relay, graceful shutdown
├── internal/
│   ├── orders/              домен: Order, статусы, ошибки, Service с сагой; интерфейсы
│   │                        Store, Tx, Warehouse, Observer — у потребителя
│   ├── grpcapi/             gRPC OrderService: proto ↔ домен, ошибки → статусы с деталями,
│   │                        курсор пагинации
│   ├── warehouse/           клиент «Склада»: дедлайн → повторы (UNAVAILABLE) → выключатель
│   │                        → gRPC; ошибки gRPC → доменные
│   ├── outbox/              relay outbox → Kafka (franz-go), метрики отставания
│   ├── catalog/             модуль 14: кеш каталога в Valkey (cache-aside, singleflight, отриц. кеш)
│   ├── shed/                модуль 14: ограничение параллелизма CreateOrder → RESOURCE_EXHAUSTED
│   ├── replica/             модуль 14: история с реплики, токен согласованности (LSN)
│   ├── shard/               модуль 14: jump hash по покупателю, номер шарда в номере заказа
│   ├── features/            модуль 14: переключатели кеша и реплики (/admin/features)
│   ├── store/postgres/      pgxpool + sqlc — основная база
│   ├── store/sqlite/        database/sql + modernc.org/sqlite + sqlc — тесты и запуск без Docker
│   ├── storetest/           контрактный набор для обоих хранилищ + фейк «Склада»
│   └── e2e/                 сквозные тесты саги: «Заказы» + настоящий «Склад» на bufconn + kfake
├── migrations/{postgres,sqlite}/   миграции goose (embed); 00003 — переезд остатков в «Склад»,
│                                   00004 — товар 4 для нагрузочных прогонов
├── sqlc.yaml
└── Dockerfile               multi-stage, scratch, не root; контекст сборки — labs/
```

Общий каркас (журнал, трассировка, gRPC- и HTTP-серверы с interceptor'ами и
middleware, пробы, повторы и выключатель) лежит в [`labs/platform`](../platform).
Он общий для трёх сервисов.

## Сага создания заказа

```mermaid
sequenceDiagram
    participant C as Клиент
    participant O as Заказы
    participant W as Склад (MySQL)
    participant DB as PostgreSQL
    C->>O: POST /v1/orders (CreateOrder)
    O->>DB: цены из каталога, nextval → order_id
    O->>W: Reserve(order_id, items) — дедлайн, повторы на UNAVAILABLE
    alt не хватает товара
        W-->>O: FAILED_PRECONDITION OUT_OF_STOCK
        O-->>C: 400 FAILED_PRECONDITION (компенсировать нечего)
    else резерв сделан
        O->>DB: BEGIN; заказ + позиции + outbox(OrderCreated); COMMIT
        alt запись не удалась
            O->>W: Release(order_id) — компенсация
            O-->>C: 500 INTERNAL
        else
            O-->>C: 200 заказ
        end
    end
```

Решения, которые стоит рассмотреть внимательно:

- **Номер заказа — до резерва.** `order_id` — ключ идемпотентности для
  «Склада», поэтому он берётся из последовательности (`nextval`) раньше,
  чем заказ записан. Повтор `Reserve` с тем же номером не спишет товар
  второй раз. Пропуски в нумерации нормальны.
- **Компенсация и при неизвестном исходе.** «Не хватает товара» — точный
  ответ, отменять нечего. Таймаут или обрыв означают, что неизвестно,
  успел ли склад зарезервировать. Поэтому на любую другую ошибку `Reserve`
  сервис вызывает `Release`. Это безопасно: `Release` идемпотентен, а без
  резерва склад оставляет «надгробие», и опоздавший `Reserve` будет
  отклонён.
- **Сбой на COMMIT неоднозначен.** Если упал сам `COMMIT`, сервис сначала
  проверяет, записан ли заказ. Компенсировать записанный заказ нельзя:
  получился бы заказ без товара.
- **Компенсация не зависит от клиента.** Если клиент ушёл или истёк его
  дедлайн, `Release` всё равно выполняется на отвязанном контексте
  (`context.WithoutCancel` + свой таймаут).
- **Отмена: сначала своя база, потом склад.** Транзакция (статус
  `cancelled` + событие `OrderCancelled`), затем `Release`. В худшем случае
  заказ отменён, а резерв ещё висит: клиент получит ошибку и повторит
  отмену, повтор идемпотентен и снова вызывает `Release`. Обратный порядок
  мог бы оставить «зарезервированный» заказ без товара.
- **Устойчивость вызова склада** (`internal/warehouse`): общий дедлайн 3 с
  → до 3 попыток только на `UNAVAILABLE` (экспоненциальная пауза с full
  jitter) → выключатель (5 неудач подряд размыкают его на 5 с, затем один
  пробный вызов). Разомкнутый выключатель отвечает мгновенно, и «Заказы»
  отдают `503 UNAVAILABLE` с `RetryInfo`. Чтение и отмена при этом
  работают.

## Outbox и relay

- Событие пишется в таблицу `outbox` той же транзакцией, что и заказ.
  Payload — бинарный protobuf `events.v1.OrderEvent`, у события
  глобальный `event_id`, рядом сохраняется контекст трассы запроса.
- Relay (`internal/outbox`) раз в 500 мс (сразу снова, если пачка полная)
  в транзакции берёт пачку по порядку, отправляет `ProduceSync` и отмечает
  `published_at` только после подтверждения брокера. Доставка
  **at-least-once**: дубли возможны, потребители отсекают их по `event_id`.
- Публикует одна реплика: `pg_try_advisory_xact_lock`. `SKIP LOCKED` здесь
  не подходит — две реплики отправляли бы события вперемешку и ломали
  порядок по ключу.
- Запись Kafka: ключ — номер заказа (все события заказа идут в одну
  партицию и по порядку), заголовки `event-id`, `event-type`,
  `content-type`, `traceparent`. Продюсер franz-go идемпотентный
  (`acks=all`).
- При остановке relay делает последний проход: события последних запросов
  уходят сразу. Kafka не входит в готовность сервиса. Брокер лёг — заказы
  принимаются, события копятся в outbox (метрика
  `orders_outbox_lag_seconds`) и уйдут позже. Ради этого outbox и нужен.

## Почему PostgreSQL и SQLite, а MySQL больше нет

PostgreSQL — основная база «Заказов». MySQL теперь отдан «Складу», вторая
СУБД курса практикуется там. Держать три диалекта outbox-relay (advisory
lock, массивы, `RETURNING`) ради одного сервиса — лишняя работа без
учебной пользы. SQLite осталась: на ней без Docker идут контрактный набор,
тесты саги и `go run`.

## Запуск

Вся система поднимается из корня репозитория: `docker compose --profile labs up --build`
(curl-сценарий — в [`labs/README.md`](../README.md)).

| Порт на хосте | Что |
|---|---|
| `127.0.0.1:8090` | REST (grpc-gateway), `/healthz`, `/readyz` |
| `127.0.0.1:50051` | gRPC (reflection включён в compose: `grpcurl -plaintext localhost:50051 list`) |
| `127.0.0.1:8190` | служебный: `/metrics`, `/healthz`, `/readyz`, `/admin/features` (если `ORDERS_ADMIN_FEATURES=true`) |

REST:

```sh
curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 42, "items": [{"product_id": 3, "quantity": 2}]}'
curl -s localhost:8090/v1/orders/1
curl -s 'localhost:8090/v1/orders?customer_id=42&page_size=10'   # next_page_token → page_token
curl -s -X POST localhost:8090/v1/orders/1:cancel
curl -s localhost:8090/v1/products/1                              # каталог (модуль 14, через кеш)
```

JSON — по правилам protojson: имена полей как в `.proto` (`customer_id`),
`int64` строкой (`"order_id": "1"`), статусы — именами enum. Неизвестные
поля в запросе — ошибка. Ошибки приходят в одном формате:
`{"error": {"code": "FAILED_PRECONDITION", "message": "...", "request_id": "...", "details": [...]}}`.
В `details` лежат `google.rpc.BadRequest` (поля), `ErrorInfo` (`OUT_OF_STOCK`),
`PreconditionFailure` (какие товары), `RetryInfo`. HTTP-код выбирается по
стандартной таблице grpc-gateway: `INVALID_ARGUMENT` и `FAILED_PRECONDITION`
→ 400, `NOT_FOUND` → 404, `RESOURCE_EXHAUSTED` → 429, `UNAVAILABLE` → 503.
Если в деталях есть `RetryInfo`, ответ несёт и заголовок `Retry-After`.

Без Docker нужны «Склад» и Kafka; база может быть SQLite:

```sh
cd labs/orders
ORDERS_DB=sqlite ORDERS_DSN=orders.db \
ORDERS_WAREHOUSE_ADDR=dns:///localhost:50052 ORDERS_KAFKA_BROKERS=localhost:59092 \
ORDERS_HTTP_ADDR=:18090 ORDERS_GRPC_ADDR=:19090 ORDERS_OPS_ADDR=:18190 \
go run ./cmd/orders
```

### Конфигурация

| Переменная | Значение |
|---|---|
| `ORDERS_DB` | `postgres` или `sqlite` (обязательно) |
| `ORDERS_DSN` | строка подключения; для SQLite — путь к файлу (обязательно; в журнал не пишется) |
| `ORDERS_WAREHOUSE_ADDR` | gRPC-адрес «Склада», например `dns:///warehouse:9090` (обязательно) |
| `ORDERS_KAFKA_BROKERS` | брокеры через запятую (обязательно) |
| `ORDERS_KAFKA_TOPIC` | топик событий, по умолчанию `orders.events` |
| `ORDERS_KAFKA_PARTITIONS`, `ORDERS_KAFKA_REPLICATION` | параметры топика при создании: 3 и 1 |
| `ORDERS_HTTP_ADDR`, `ORDERS_GRPC_ADDR`, `ORDERS_OPS_ADDR` | `:8080`, `:9090`, `:8081` |
| `ORDERS_OTLP_ENDPOINT` | приёмник OTLP/gRPC (`jaeger:4317`); пусто — трассы не экспортируются |
| `ORDERS_LOG_LEVEL` | `debug`, `info` (по умолчанию), `warn`, `error` |
| `ORDERS_GRPC_REFLECTION` | `true` — включить gRPC reflection (в проде обычно выключен) |
| `ORDERS_DRAIN_DELAY` | пауза между снятием готовности и остановкой (k8s: ~5s), по умолчанию 0 |
| `ORDERS_OUTBOX_INTERVAL` | период relay, по умолчанию `500ms` |
| `ORDERS_VALKEY_ADDR` | модуль 14: Valkey для кеша каталога (`valkey:6379`); пусто — без кеша |
| `ORDERS_CATALOG_TTL`, `ORDERS_CATALOG_NEGATIVE_TTL` | время жизни записи о товаре (±10% джиттер) и записи «нет такого»: `5m` и `30s` |
| `ORDERS_CREATE_MAX_IN_FLIGHT` | одновременных CreateOrder, по умолчанию 64; `0` — без ограничения (в compose — 16) |
| `ORDERS_CREATE_MAX_QUEUE`, `ORDERS_CREATE_QUEUE_TIMEOUT` | сколько вызовов ждут места и сколько: `64` и `100ms` (в compose — 16 и `50ms`) |
| `ORDERS_REPLICA_DSN` | реплика шарда 0 для ListOrders (только postgres; в журнал не пишется); пусто — история с основной |
| `ORDERS_SHARD_DSNS` | шарды через запятую (только postgres): первый — шард 0, прежняя база; `ORDERS_DSN` тогда пуст или равен первому |
| `ORDERS_ADMIN_FEATURES` | `true` — эндпоинт `/admin/features` на служебном порту (кеш и реплика вкл/выкл без перезапуска) |

### Остановка

По SIGTERM сервис останавливается в таком порядке:

1. Снимает готовность (`/readyz` → 503, gRPC health → `NOT_SERVING`) и ждёт `ORDERS_DRAIN_DELAY`.
2. Останавливает REST (`Shutdown`) и gRPC (`GracefulStop`, по истечении 20 с — `Stop`).
3. Relay делает последний проход.
4. Останавливается служебный сервер.
5. Закрываются соединения со «Складом», Kafka и пул базы.
6. Последним сбрасываются накопленные спаны.

## Метрики

Помимо RED из платформы (`grpc_server_*`, `grpc_client_*`,
`http_server_*`), Go и process, сервис отдаёт свои:

| Метрика | Что |
|---|---|
| `orders_outbox_pending_events` | событий ждут публикации |
| `orders_outbox_lag_seconds` | возраст самого старого неопубликованного события — главный алерт relay |
| `orders_outbox_published_total`, `orders_outbox_publish_errors_total` | опубликовано / неудачных тактов |
| `orders_warehouse_releases_total{reason, result}` | возвраты резерва: `reserve_failed`, `write_failed` (компенсации), `cancel`; `result="failed"` — резерв остался висеть |
| `orders_warehouse_retries_total` | повторы вызовов склада |
| `orders_warehouse_breaker_state` | выключатель: 0 — замкнут, 1 — разомкнут, 2 — пробный вызов |
| `orders_catalog_cache_lookups_total{result}` | обращения к кешу каталога по ключу: `hit`, `negative_hit`, `miss`, `error` (Valkey недоступен), `bypass` (кеш выключен) |
| `orders_catalog_cache_coalesced_total` | промахи, которые дождались чужой загрузки (singleflight) вместо своего запроса в базу |
| `orders_catalog_db_queries_total` | запросы каталога, дошедшие до базы |
| `orders_shed_rejected_total{method, reason}` | отброшено с `RESOURCE_EXHAUSTED`: `queue_full` (очередь полна), `timeout` (не дождался места) |
| `orders_shed_in_flight`, `orders_shed_waiting`, `orders_shed_max_in_flight` | сколько CreateOrder выполняется, ждёт, и лимит |
| `orders_history_reads_total{target, reason}` | чтения истории: `replica` (`no_token`, `caught_up`) или `primary` (`lagging`, `no_replica`, `disabled`, `forced`, `replica_error`) |

При шардировании метрики relay (`orders_outbox_*`) получают метку `shard`.

## Тесты

```sh
cd labs/orders
go test -race ./...
ORDERS_TEST_POSTGRES='postgres://postgres:labs@127.0.0.1:55432/postgres?sslmode=disable' \
  go test -race -count=3 ./...
```

- `internal/storetest` — контрактный набор для SQLite (всегда) и
  PostgreSQL (с переменной; у каждого подтеста своя база
  `orders_test_<случайное>`, удаляется в `t.Cleanup`). В наборе:
  уникальность номеров, пагинация, конкурентная отмена (ровно одно
  событие), публикация outbox пачками и сохранение событий при сбое
  брокера.
- `internal/orders` — сага на SQLite и фейке склада. Сбои базы (до COMMIT,
  на COMMIT, после COMMIT) делает обёртка хранилища, а не мок.
  Проверяется, когда компенсация нужна и когда нельзя.
- `internal/warehouse` — повторы, выключатель, дедлайн и перевод ошибок
  на `bufconn`. Время внедрено: пауз нет.
- `internal/outbox` — relay на `kfake` (Kafka в процессе): ключ, заголовки,
  порядок, недоступный брокер, продолжение трассы (`tracetest`),
  последний проход при остановке.
- `internal/grpcapi` — gRPC и REST через gateway на `bufconn`: коды,
  детали ошибок, курсоры.
- `internal/e2e` — сквозная сага: настоящий «Склад» ([`warehousetest`](../warehouse/warehousetest))
  и «Заказы» на `bufconn`, `kfake`. Покрыты успех и отмена, нехватка
  товара, компенсация сбоя записи, потерянный ответ `Reserve` (повторы не
  списывают трижды, компенсация возвращает товар) и временная
  недоступность.
- `cmd/orders` — `run()` целиком: SQLite, «Склад» на loopback, kfake, REST,
  пробы, метрики и мягкая остановка; с Valkey (miniredis) — кеш каталога и
  `/admin/features`.
- Модуль 14: `internal/catalog` — cache-aside, TTL и джиттер, отрицательный
  кеш, деградация и восстановление Valkey (miniredis), stampede и уход
  первого клиента (`testing/synctest`, без пауз); `internal/shed` — очередь,
  отказы, отмена (таймер ожидания срабатывает по команде теста);
  `internal/replica` — выбор реплики/основной по токену на фейках;
  `internal/store/postgres` с `ORDERS_TEST_POSTGRES_REPLICA` —
  read-your-writes на настоящей потоковой реплике; `internal/shard` —
  контрольные значения jump hash, равномерность, минимальный переезд и
  сервис на двух шардах SQLite; `internal/grpcapi` — `GetProduct`, токен и
  429 с `Retry-After` через gateway.

## Генерация кода

```sh
cd labs/orders && sqlc generate   # sqlc v1.31.1; запросы — internal/store/*/query.sql
cd labs/api && buf generate       # контракты (см. labs/api/README.md)
```

`query.sql` для SQLite пишется только в ASCII: парсер sqlite в sqlc v1.31
сбивается на кириллице.

## Чего нет: не выставляйте наружу

- **Аутентификации и авторизации.** `customer_id` берётся из запроса как
  есть. Любой клиент может прочитать и отменить любой заказ. В проде нужен
  auth-interceptor (токен в metadata → `Unauthenticated`/`PermissionDenied`)
  и `customer_id` из токена (урок 11-grpc/12).
- **TLS между сервисами.** Внутри docker-сети gRPC идёт открытым текстом.
  Подробнее — в [`labs/README.md`](../README.md#безопасность).
- **Продакшн-настроек базы.** DSN в compose учебный: суперпользователь
  `postgres`, пароль в репозитории, `sslmode=disable`. В проде миграции
  накатываются отдельным шагом под ролью-владельцем схемы, сервис ходит
  ролью только с `SELECT/INSERT/UPDATE` на свои таблицы, `sslmode=verify-full`,
  пароль берётся из менеджера секретов.
- **Фоновых повторов компенсации.** Если `Release` не удался даже после
  повторов, это видно в журнале и в метрике `result="failed"`, но
  автоматически не дочищается. В проде компенсацию записывают в свою базу
  (тот же outbox) и повторяют до успеха.
- **Уборки outbox.** Опубликованные строки не удаляются. В проде их чистят
  периодически или партиционируют таблицу по времени.
- **Идемпотентности CreateOrder для клиента.** Повтор `POST /v1/orders`
  после таймаута создаст второй заказ. Лечится ключом идемпотентности
  от клиента (задача урока 11-grpc/14).

## Модуль 14: нагрузка

Четыре приёма из модуля 14, встроенные в сервис. Домен видит их через
интерфейсы `orders.Catalog`, `orders.Router`, `orders.History`
(`internal/orders/routing.go`), реализации собираются в `main`. Без
настроек (пустые переменные) сервис ведёт себя как в модулях 11–12.

### Кеш каталога (`internal/catalog`)

`GET /v1/products/{product_id}` (новый `GetProduct` в `orders.v1`,
совместимое изменение) и цены в CreateOrder читаются через Valkey:

- **cache-aside**: `MGET` ключей `orders:catalog:v1:<id>`, промахи — из
  базы, результат — обратно с TTL. Версия в префиксе позволяет сменить
  формат значения без чтения старых записей;
- **TTL с джиттером ±10%**: записи, загруженные одной волной, не истекают
  в одну секунду;
- **защита от stampede**: одновременные промахи по одному ключу идут в базу
  одним запросом (`singleflight` на ключ), остальные ждут его. Загрузка —
  на контексте, отвязанном от первого клиента: его уход не роняет
  остальных;
- **отрицательный кеш**: «товара нет» хранится 30 с — запросы
  несуществующих id не долбят базу;
- **деградация**: Valkey недоступен или отвечает дольше 100 мс — читаем из
  базы, ошибок клиенту нет (метрика `result="error"`, журнал не чаще раза
  в 10 с). Недоступный при старте Valkey не мешает старту, клиент
  переподключается сам.

Инвалидации нет: каталогом через API никто не управляет, цена обновится
по TTL. Цена в заказе фиксируется на момент заказа — устаревшая на минуты
цена здесь допустима (в другой системе удаляли бы ключ по событию
изменения).

### Load shedding (`internal/shed`)

CreateOrder — самый дорогой вызов (склад, транзакция, outbox). Interceptor
пускает не больше `ORDERS_CREATE_MAX_IN_FLIGHT` одновременно, ещё
`ORDERS_CREATE_MAX_QUEUE` ждут места не дольше `ORDERS_CREATE_QUEUE_TIMEOUT`.
Остальные сразу получают `RESOURCE_EXHAUSTED` с `google.rpc.RetryInfo`
(1 с), через gateway — **429** с заголовком `Retry-After` (его ставит
платформа для любого `RetryInfo`). Запрос, получивший 429, не выполнялся
вовсе — повторять безопасно. Чтение не ограничено. Лимит статический;
адаптивный (AIMD по задержке) честнее, но сложнее объяснить и проверить.

### История с реплики и read-your-writes (`internal/replica`)

С `ORDERS_REPLICA_DSN` ListOrders читает с потоковой реплики. Реплика стенда
нарочно отстаёт на 200 мс, поэтому покупатель, открывший историю сразу
после заказа, «потерял» бы его. Решение — **токен согласованности на LSN**:

1. CreateOrder и CancelOrder после записи берут `pg_current_wal_lsn()`
   основной базы и возвращают его в новом поле `consistency_token`
   (`"lsn:0/3266B4C0"`; для клиента — непрозрачная строка).
2. Клиент передаёт токен в ListOrders (`?consistency_token=…`). Сервис идёт
   на реплику, только если `pg_last_wal_replay_lsn()` реплики не меньше
   токена, иначе — на основную. Без токена — на реплику.
3. Позиция реплики только растёт, последнее увиденное значение кешируется:
   для уже выполненного токена лишнего запроса к реплике нет.

GetOrder читает всегда с основной: заказ виден сразу после CreateOrder.

```sh
R=$(curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 77, "items": [{"product_id": 4, "quantity": 1}]}')
TOK=$(echo "$R" | python3 -c 'import sys, json; print(json.load(sys.stdin)["consistency_token"])')
curl -s 'localhost:8090/v1/orders?customer_id=77'                         # с реплики: заказа ещё нет
curl -s "localhost:8090/v1/orders?customer_id=77&consistency_token=$TOK"  # заказ есть
curl -s localhost:8190/metrics | grep ^orders_history_reads_total          # primary/lagging + replica/no_token
```

Почему токен в поле, а не в заголовке: поле видно в контракте и
одинаково работает в gRPC и REST (gateway не пропускает metadata наружу —
так задумано в платформе). Почему не «читать с основной N секунд после
записи»: задержка реплики не ограничена сверху, окно пришлось бы угадывать;
LSN проверяет сам факт применения и не требует памяти о клиенте на
конкретном экземпляре сервиса.

Ограничения: токен защищает только того, кто его передал (другая вкладка
без токена может увидеть старую историю — это eventual consistency);
реплика одна и только у шарда 0; недоступная реплика — чтение с основной
(`reason="replica_error"`), ошибок клиенту нет; если позицию узнать не
удалось, токен — `primary` (читать с основной).

### Шардирование по покупателю (`internal/shard`)

По умолчанию выключено. С `ORDERS_SHARD_DSNS` заказы покупателя живут на
шарде `jump_hash(splitmix64(customer_id), N)`:

- **jump consistent hash** — стабильная функция без состояния (ни таблиц,
  ни кольца), равномерная, при добавлении шарда переезжает минимум —
  1/(N+1) покупателей и только на новый шард. Ограничение — шарды
  нумеруются подряд и добавляются в конец; здесь это не мешает, номер шарда
  и так неизменен. Rendezvous hashing позволил бы удалять любой шард, но
  стоит O(N) на выбор и требует стабильных имён;
- **номер шарда — в старших битах номера заказа**: `id = shard<<48 | seq`.
  GetOrder и CancelOrder находят шард без опроса всех. Совместимость: у
  шарда 0 номера те же (1, 2, 3…), и база модулей 11–12 становится шардом 0
  как есть. Номер заказа шарда 1 — `281474976710657`: в JSON int64 всё равно
  идёт строкой;
- у каждого шарда свои миграции, outbox и relay (advisory-блокировка — в
  своей базе), готовность проверяет все шарды; порядок событий заказа
  сохраняется — заказ целиком живёт на одном шарде;
- каталог — справочник: читается с шарда 0, демо-миграции заводят его на
  каждом шарде (позиции ссылаются на `products` внешним ключом);
- реплика — только у шарда 0: `postgres-replica` — копия `postgres`. Токен
  для покупателей шарда 1 пустой, их история читается с основной.

Включить на стенде (пересоздаётся только контейнер `orders`):

```sh
docker compose -f docker-compose.yml -f docker-compose.shards.yml --profile labs up -d orders
curl -s -X POST localhost:8090/v1/orders -H 'Content-Type: application/json' \
  -d '{"customer_id": 3, "items": [{"product_id": 4, "quantity": 1}]}'   # order_id 281474976710657…
curl -s localhost:8190/metrics | grep '^orders_outbox_published_total'   # {shard="0"}, {shard="1"}
```

Выключить — тот же `up -d orders` без второго файла. Заказы, созданные на
шарде 1, после этого не находятся (NOT_FOUND): решардирования нет. Его же
нет и при добавлении шарда: jump hash отправит новые заказы части
покупателей на новый шард, а старые останутся на прежнем (GetOrder их
найдёт, история покажет только новые). Перед добавлением шарда
переезжающих покупателей переносят отдельным инструментом.

### Переключатели (`internal/features`)

С `ORDERS_ADMIN_FEATURES=true` служебный порт отдаёт `GET/POST
/admin/features`: `{"catalog_cache": false}` выключает кеш,
`{"replica_reads": false}` — чтение с реплики, без перезапуска. Им
пользуется нагрузочный разбор `loadgen analyze` ([`labs/loadgen`](../loadgen)).
Включить функцию без настройки (нет Valkey или реплики) нельзя — 409.
Аутентификации нет: эндпоинт на служебном порту (в compose — только
127.0.0.1 и сеть docker `default`), тело ограничено 1 КиБ, смена пишется в
журнал. В проде это работа системы feature flags с аудитом.

## Что дальше

Чего не хватает до продакшна в части модуля 14: инвалидации кеша по
событиям, адаптивного лимита вместо статического, нескольких реплик с
балансировкой и решардирования.
