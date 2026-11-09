# Сервис «Склад»

Второй сервис итогового проекта курса «Go для C#-разработчика» (модули 11–12).
Владеет остатками товара и резервами заказов. «Заказы» вызывают его по gRPC:
резервируют товар при создании заказа и отменяют резерв при отмене
(компенсирующее действие саги). Как и «Заказы», это не упражнение, а сервис,
написанный так, как его стоит писать в продакшне: читайте его как пример.

Что умеет (контракт — `labs/api/proto/warehouse/v1/warehouse.proto`):

- `Reserve(order_id, items)` — зарезервировать все позиции заказа или ни
  одной. Повтор с теми же позициями возвращает существующий резерв.
- `Release(order_id)` — вернуть товар на склад. Повторный `Release` и
  `Release` без резерва — не ошибка; второй оставляет «надгробие», и
  опоздавший `Reserve` этого заказа отклоняется.
- `GetStock(product_id)` — остаток; неизвестный товар — нули, а не
  `NOT_FOUND` (каталогом склад не владеет). Единственный метод, выставленный
  наружу через REST: `GET /v1/stock/{product_id}`.

Ошибки `Reserve` — статус gRPC с `google.rpc.ErrorInfo` (domain
`warehouse.gotutor.local`, reason — имя значения `ErrorReason` без префикса):

| Ситуация | Код | `reason` | Ещё детали |
|---|---|---|---|
| не хватает товара | `FAILED_PRECONDITION` | `OUT_OF_STOCK` | `PreconditionFailure`: по нарушению на **каждую** нехватающую позицию (`STOCK`, `products/<id>`, `requested N, available M`) |
| резерв заказа уже отменён | `ABORTED` | `RESERVATION_RELEASED` | — |
| у заказа резерв с другими позициями | `ALREADY_EXISTS` | `RESERVATION_MISMATCH` | — |
| запрос нарушает правила контракта | `INVALID_ARGUMENT` | — | `BadRequest` (protovalidate) |

## Структура

```
labs/warehouse/
├── cmd/warehouse/main.go       тонкий main → run(ctx, args, getenv, stdout): конфигурация,
│                               миграции, сборка зависимостей, три сервера, graceful shutdown
├── internal/
│   ├── stock/                  домен: Item, Reservation, Level, ошибки, Service, метрики;
│   │                           интерфейсы Store и Tx объявлены здесь (у потребителя)
│   ├── grpcapi/                WarehouseService: тонкие обработчики и перевод доменных
│   │                           ошибок в статусы с деталями — в одном месте (errors.go)
│   ├── store/
│   │   ├── mysql/              database/sql + go-sql-driver/mysql, транзакции с повторами
│   │   └── memstore/           в памяти, один мьютекс: для тестов и warehousetest
│   └── storetest/              контрактный набор для обоих хранилищ
├── migrations/                 миграции goose (MySQL), встроены в бинарник (embed)
├── warehousetest/              публичный пакет: настоящий «Склад» в памяти для тестов клиентов
└── Dockerfile                  multi-stage, контекст — labs/, финальный образ scratch, не root
```

Общий каркас (журнал, трассировка, gRPC- и HTTP-серверы с interceptor'ами и
middleware, health, повторы) — в `labs/platform`, контракт и сгенерированный
код — в `labs/api`. Сторонние зависимости — только драйвер MySQL, `goose`,
gRPC/grpc-gateway/protovalidate, Prometheus, OpenTelemetry и
`golang.org/x/sync`, тех же версий, что в песочнице учебника.

## Ключевые решения

- **Всё или ничего в одной транзакции.** `Reserve` блокирует строки `stock`
  (`SELECT … FOR UPDATE`), собирает все нехватки, и только если их нет —
  списывает `available` и увеличивает `reserved`. Ошибка — откат, остатки не
  тронуты.
- **Порядок блокировок.** Позиции сортируются по `product_id`, и строки
  остатков блокируются в этом порядке во всех транзакциях (`Release` —
  тоже): встречные резервы `(1,2)` и `(2,1)` не ловят deadlock. Остатки
  блокируются раньше резерва — конкурирующие резервы одного товара встают в
  очередь ещё до таблицы `reservations`.
- **Резерв «занимается» вставкой, а не чтением.** Очевидная схема «прочитать
  `reservations` с `FOR UPDATE`, не нашли — вставить» в REPEATABLE READ
  берёт **блокировку гэпа**: гэп над последним `order_id` у всех новых
  заказов общий, и две транзакции, взявшие его, взаимно блокируют друг другу
  вставку. Первая версия сервиса так и была написана — контрактный тест на
  MySQL (20 пар Reserve/Release параллельно) исчерпывал 5 попыток на
  deadlock даже у *разных* заказов. Сейчас `ClaimReservation` делает
  `INSERT … ON DUPLICATE KEY UPDATE order_id = order_id`: нет строки — вставка
  (1 затронутая строка), есть — эксклюзивная блокировка существующей (0
  строк). Вставки разных ключей друг другу не мешают. Для этого `Open`
  снимает флаг `CLIENT_FOUND_ROWS` (иначе MySQL считал бы найденные строки,
  а не изменённые).
- **Повтор транзакции — свойство хранилища.** Гонки одного `order_id` (повтор
  клиента, `Reserve` и `Release` наперегонки) всё равно возможны: InnoDB
  откатывает одну транзакцию с ошибкой 1213 (deadlock), бывает и 1062
  (duplicate key). `mysql.Store.InTx` повторяет транзакцию целиком — до 5
  попыток с короткой случайной паузой (`resilience.Retry` из platform, full
  jitter), счётчик — `warehouse_tx_retries_total`. Домен пишет функцию
  транзакции так, чтобы её можно было выполнить заново: результат — только в
  переменных, которые каждая попытка перезаписывает (в .NET то же делает
  `EnableRetryOnFailure` + `ExecutionStrategy.Execute`).
- **Идемпотентность по `order_id`.** Повтор `Reserve` сравнивает позиции как
  множество (`product_id → quantity`): тот же набор — вернуть резерв, другой —
  `RESERVATION_MISMATCH`. «Надгробие» не даёт опоздавшему `Reserve`
  зарезервировать товар для уже отменённого заказа навсегда.
- **Ошибки — в одном месте.** Домен возвращает `*OutOfStockError` (с данными о
  каждой нехватке), `ErrReservationReleased`, `ErrReservationMismatch`;
  `grpcapi/errors.go` переводит их в коды и детали. Всё неизвестное —
  `INTERNAL "internal error"`, оригинал — в журнал с `request_id` и
  `trace_id` (`grpcx.Internal`). Тексты SQL-ошибок клиенту не уходят.
- **Валидация — по контракту.** Правила (`order_id > 0`, 1–100 позиций,
  количество 1–1000, товары не повторяются) записаны в `.proto` и проверяются
  interceptor'ом protovalidate до обработчика. Домен их не дублирует.
- **REST через сеть.** grpc-gateway ходит в собственный gRPC-сервер по
  соединению (`grpcx.NewClient`), а не вызывает обработчик напрямую: REST-запрос
  проходит те же interceptor'ы — валидацию, журнал, метрики, дедлайн.
- **Хранилище в памяти — честное.** `memstore` держит один мьютекс на всю
  транзакцию и работает с копией состояния: ошибка — копия выбрасывается.
  Контрактный набор гоняет одно и то же на `memstore` и MySQL, поэтому
  `warehousetest` ведёт себя как настоящий сервис.

## Конфигурация

| Переменная | Значение |
|---|---|
| `WAREHOUSE_MYSQL_DSN` | DSN драйвера: `user:pass@tcp(host:3306)/db` (обязательно). `parseTime=true` и `loc=UTC` сервис ставит сам. В журнал и тексты ошибок не попадает |
| `WAREHOUSE_HTTP_ADDR` | REST (grpc-gateway), по умолчанию `:8080` |
| `WAREHOUSE_GRPC_ADDR` | gRPC, по умолчанию `:9090` |
| `WAREHOUSE_OPS_ADDR` | `/healthz`, `/readyz`, `/metrics`, по умолчанию `:8081` |
| `WAREHOUSE_OTLP_ENDPOINT` | `host:port` приёмника OTLP/gRPC (в labs — `jaeger:4317`); пусто — спаны не экспортируются |
| `WAREHOUSE_LOG_LEVEL` | `debug`, `info` (по умолчанию), `warn`, `error` |
| `WAREHOUSE_DRAIN_DELAY` | пауза между снятием готовности и остановкой, по умолчанию `0` (в Kubernetes ~`5s`) |
| `WAREHOUSE_GRPC_REFLECTION` | `true` — включить gRPC reflection (для `grpcurl` без `.proto`), по умолчанию выключено |

Все ошибки конфигурации сообщаются сразу, одной ошибкой при старте.

Остановка по SIGTERM: готовность снимается (`/readyz` → 503, gRPC health →
`NOT_SERVING`), пауза `DRAIN_DELAY`, REST перестаёт принимать и дорабатывает
начатое, затем gRPC (`GracefulStop`, по истечении 15 с — `Stop`), служебный
HTTP — последним, затем закрывается пул базы и сбрасываются спаны.

## База и роли

`labs/initdb/mysql/20-warehouse.sql` (выполняется при первом создании тома
MySQL) заводит базу `warehouse` и пользователя `warehouse`/`labs-warehouse` с
`ALL PRIVILEGES ON warehouse.*`: сервис сам применяет миграции при старте,
поэтому он владелец схемы. Вход под `warehouse` из подсети песочницы
`172.29.99.%` заблокирован отдельной записью `ACCOUNT LOCK` — программы
учеников знают пароли из репозитория.

На уже запущенной MySQL init-скрипт не выполнится — выполните его вручную:

```sh
docker compose exec -T mysql mysql -uroot -plabs < labs/initdb/mysql/20-warehouse.sql
```

**Как в проде:** две роли. Миграции (DDL) выполняет роль-владелец схемы
отдельным шагом деплоя (Job перед выкаткой), а сервис работает под ролью с
`SELECT, INSERT, UPDATE, DELETE ON warehouse.*` — уязвимость в сервисе не
даст удалить таблицы. TLS до базы (`tls=true` в DSN), пароль — из менеджера
секретов.

**Миграции и несколько реплик.** У goose нет блокировки сессии (session
locker) для MySQL — только для PostgreSQL. Две реплики, стартовавшие
одновременно, могут начать одну миграцию дважды; в labs реплика одна, в
проде миграции — отдельный шаг деплоя.

## Запуск

В профиле `labs` (после того как сервис добавлен в `docker-compose.yml`):

```sh
docker compose --profile labs up --build warehouse
```

Демо-миграция заводит остатки для товаров каталога «Заказов»: 1 «Клавиатура»
— 10 шт., 2 «Мышь» — 25 шт., 3 «Монитор» — 3 шт. Миграция 00003 (модуль 14)
добавляет 4 «Кабель USB-C» — миллион штук для нагрузочных прогонов
`labs/loadgen`: заказы генератора не кончают демо-остатки. Все резервы на
него идут в одну строку `stock` под `SELECT … FOR UPDATE` — под нагрузкой
это горячая строка, которая ограничивает пропускную способность
CreateOrder (её и защищает load shedding «Заказов»).

```sh
# готовность (пинг MySQL) и метрики — служебный порт
curl -s localhost:8191/readyz
curl -s localhost:8191/metrics | grep ^warehouse_

# остаток через REST
curl -s localhost:8091/v1/stock/3
# {"stock":{"product_id":"3", "available":3, "reserved":0}}

# gRPC (нужен WAREHOUSE_GRPC_REFLECTION=true или -proto)
grpcurl -plaintext -d '{"order_id": 1, "items": [{"product_id": 3, "quantity": 2}]}' \
  localhost:50052 warehouse.v1.WarehouseService/Reserve
grpcurl -plaintext -d '{"order_id": 2, "items": [{"product_id": 3, "quantity": 5}]}' \
  localhost:50052 warehouse.v1.WarehouseService/Reserve   # FAILED_PRECONDITION + детали
grpcurl -plaintext -d '{"order_id": 1}' localhost:50052 warehouse.v1.WarehouseService/Release
grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check
```

Без Docker-образа, на MySQL профиля labs:

```sh
cd labs/warehouse
WAREHOUSE_MYSQL_DSN='warehouse:labs-warehouse@tcp(127.0.0.1:53306)/warehouse' \
WAREHOUSE_HTTP_ADDR=127.0.0.1:8091 WAREHOUSE_GRPC_ADDR=127.0.0.1:50052 \
WAREHOUSE_OPS_ADDR=127.0.0.1:8191 WAREHOUSE_GRPC_REFLECTION=true \
go run ./cmd/warehouse
```

Образ: `docker build -f labs/warehouse/Dockerfile labs` (контекст — `labs/`:
модуль ссылается на `labs/api` и `labs/platform` через `replace`).

## Тесты

```sh
cd labs/warehouse
go test -race ./...
```

Без переменных окружения контрактный набор идёт на хранилище в памяти, а
тесты MySQL (контракт и дымовой тест `run`) пропускаются. С профилем `labs`:

```sh
WAREHOUSE_TEST_MYSQL='root:labs@tcp(127.0.0.1:53306)/' go test -race -count=3 ./...
```

`WAREHOUSE_TEST_MYSQL` — DSN администратора: каждый подтест создаёт свою базу
`warehouse_test_<случайное>` и удаляет её в `t.Cleanup`.

Что проверяется:

- `internal/storetest` (memstore и MySQL): резерв и отмена, сортировка
  позиций, идемпотентный повтор (в том числе когда остатка уже нет), MISMATCH,
  атомарность при нехватке и нехватка по каждой позиции, повторный
  `Release`, надгробие и отказ последующему `Reserve`; конкурентно — 24
  горутины на 7 единиц товара (ровно 7 успехов, остальные `OUT_OF_STOCK`),
  встречные резервы `(1,2)`/`(2,1)` (только `OUT_OF_STOCK`), 20 пар
  параллельных `Reserve`/`Release` одного заказа (итог согласован: товар
  вернулся, либо `Reserve` отклонён), 8 параллельных повторов одного `Reserve`
  (списано один раз).
- `internal/stock`: доменные метрики, пригодность функции транзакции к
  повтору (результат и метрика — от последней попытки).
- `internal/grpcapi` через `bufconn` с interceptor'ами platform: коды и детали
  ошибок (`ErrorInfo`, `PreconditionFailure`), валидация по контракту, REST
  `GetStock` через grpc-gateway (формат ответа и ошибок, отсутствие маршрутов
  для `Reserve`/`Release`).
- `warehousetest`: поведение как у сервиса, `Fault` `Before` (операция не
  выполнена) и `After` (резерв сделан, клиент получил ошибку, повтор не
  списывает второй раз), счётчик вызовов.
- `cmd/warehouse`: конфигурация и её ошибки, DSN не утекает, быстрый отказ без
  базы; с MySQL — старт, миграции, `/readyz`, REST, gRPC, health, метрики,
  мягкая остановка.

## warehousetest — «Склад» для тестов клиентов

```go
w := warehousetest.New(map[int64]int32{1: 10}) // начальные остатки
lis := bufconn.Listen(1 << 20)
w.Serve(t, lis) // тот же grpcapi-сервер с interceptor'ами; Stop — в t.Cleanup

// «Склад» недоступен — операция не выполнена:
w.SetFault(func(method string, phase warehousetest.Phase) error {
	if method == "Reserve" && phase == warehousetest.Before {
		return status.Error(codes.Unavailable, "warehouse is down")
	}
	return nil
})
// Phase After — операция выполнена, ответ потерян.

available, reserved := w.Stock(1)
st, ok := w.Reservation(42)
n := w.Calls("Reserve") // включая отклонённые Fault и валидацией
```

Это не мок: отвечает настоящий домен, поэтому тест клиента проверяет работу
с реальной семантикой (идемпотентность, надгробие, коды и детали ошибок).

## Метрики

| Метрика | Метки |
|---|---|
| `warehouse_reservations_total` | `result`: `reserved`, `duplicate`, `out_of_stock`, `released`, `mismatch` |
| `warehouse_releases_total` | `result`: `released`, `already_released`, `tombstone` |
| `warehouse_tx_retries_total` | `reason`: `deadlock`, `duplicate` |
| `grpc_server_handled_total`, `grpc_server_handling_seconds` | RED gRPC (platform) |
| `http_server_requests_total`, `http_server_request_duration_seconds` | RED REST по шаблону маршрута (platform) |
| `go_*`, `process_*` | рантайм Go и процесс |

Все значения меток доменных метрик создаются при старте с нулём — ряды видны
в Prometheus до первого события.

## Чего нет и как в проде

- **Аутентификации между сервисами.** gRPC без TLS, любой в docker-сети может
  вызвать `Reserve`. В проде — mTLS (`grpcx.ServerConfig.Creds`) или
  сервисная сетка, плюс проверка, что вызывает именно «Заказы».
- **Приёмки и инвентаризации.** Остатки заводятся миграцией; API пополнения
  склада нет.
- **Срока жизни резерва.** Резерв, который «Заказы» не подтвердили и не
  отменили (сбой саги), висит до ручного `Release`. В проде — TTL резерва и
  фоновая уборка, либо гарантированная компенсация на стороне саги.
- **Уборки надгробий.** Они копятся вечно; в проде их удаляют, когда повтор
  `Reserve` уже невозможен (через срок, превышающий все таймауты и повторы
  клиента).
- **Горячих товаров.** Все резервы одного товара сериализуются на одной
  строке `stock`. Для распродаж — шардирование остатка по нескольким строкам
  или предварительное распределение квот.
- **Продакшн-настроек базы** — см. «База и роли»: отдельные роли, TLS,
  секреты, миграции отдельным шагом.
