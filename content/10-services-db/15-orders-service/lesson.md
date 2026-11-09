---
title: Сервис «Заказы»
summary: Итог модуля — сквозной сервис заказов на SQLite (домен, хранилище, HTTP, outbox, сборка в main), устройство полноценного сервиса labs/orders на трёх базах и итоговое ревью сервиса с семью замечаниями из чек-листа.
difficulty: 5
minutes: 60
prerequisites:
  - 10-services-db/02-project-layout
  - 10-services-db/03-errors-context-shutdown
  - 10-services-db/05-connection-pool
  - 10-services-db/06-transactions
  - 10-services-db/07-locking
  - 10-services-db/09-n-plus-one-batches
  - 10-services-db/10-outbox
docs:
  - title: "Go: Accessing relational databases"
    url: https://go.dev/doc/database/
  - title: "Go Code Review Comments"
    url: https://go.dev/wiki/CodeReviewComments
  - title: "Organizing a Go module"
    url: https://go.dev/doc/modules/layout
runner:
  race: true
taskKind: review
---

Модуль разбирал практики по одной: раскладку, ошибки и `context` через слои, пул, транзакции, блокировки, N+1, outbox. Этот урок собирает их в один сервис. Пример — сервис «Заказы» в одном файле на SQLite. Разделы файла повторяют пакеты настоящего проекта, поэтому их легко сопоставить:

| Раздел примера | Пакет в `labs/orders` | Что в нём важно |
| --- | --- | --- |
| `Status`, `Order`, ошибки, `Store`, `Service` | `internal/orders` | доменные ошибки для `errors.Is`, `ValidationError` с полем, интерфейс `Store` объявлен здесь — у потребителя |
| `SQLiteStore`, `withTx` | `internal/store/sqlite` (и `postgres`; резерв — в `labs/warehouse/internal/store/mysql`) | явная транзакция с `defer tx.Rollback()`, атомарный резерв `UPDATE … WHERE available >= ?`, событие outbox в той же транзакции, заказ с позициями — два запроса, а не 1 + N |
| `handler`, `fail` | `internal/grpcapi` (REST — grpc-gateway, middleware — `labs/platform/httpx`) | `MaxBytesReader` и `DisallowUnknownFields`, `r.Context()` до самого SQL, перевод ошибок в статусы в одном месте, подробности — только в журнал |
| `run` | `cmd/orders` | ручная сборка: база → хранилище → сервис → обработчик → `http.Server` с таймаутами; `run` тестируем, `main` — три строки |

Сценарий примера проходит основные пути. Создание заказа резервирует товар. Второй заказ не проходит целиком: кружка, списанная первой позицией, возвращается вместе с откатом транзакции. Неизвестный товар даёт `422`, лишнее поле в JSON — `400`, повторная отмена — `409`, несуществующий заказ — `404`. В конце пример печатает остатки и события outbox в порядке изменений.

**Полноценный сервис — `labs/orders`.** Это отдельный Go-модуль `gotutor/labs/orders` в репозитории учебника. Его стоит прочитать целиком: он написан так, как пишут в продакшне. Сейчас он уже в состоянии после модулей 11–12 (gRPC, сага со «Складом», Kafka) — устройство системы описано в `labs/README.md`.

```
labs/orders/
  cmd/orders/main.go          main → run(ctx, args, getenv, stdout): конфигурация из env,
                              миграции при старте, сборка зависимостей, graceful shutdown
  internal/orders/            домен и сервис; интерфейсы Store и Tx объявлены здесь
  internal/grpcapi/           gRPC OrderService (REST — grpc-gateway), ошибки → статусы (errors.go)
  internal/store/postgres/    pgxpool + sqlc
  internal/store/sqlite/      database/sql + modernc.org/sqlite + sqlc
  internal/storetest/         один контрактный набор тестов для обоих хранилищ
  migrations/{postgres,sqlite}/   миграции goose, встроены через embed
  sqlc.yaml                   генерация кода запросов
```

Конфигурация — переменные `ORDERS_*`: база (`ORDERS_DB` — `postgres` или `sqlite`, `ORDERS_DSN`), адреса «Склада» и Kafka; полный список — в README. Команда `docker compose --profile labs up --build` поднимает сервис на `localhost:8090` с PostgreSQL вместе с остальной системой. Миграции и демо-каталог он применяет сам, проверить можно через `curl -s localhost:8090/v1/orders/1` после создания заказа (`POST /v1/orders`). Тесты: `go test -race ./...` гоняет контрактный набор на SQLite. С переменной `ORDERS_TEST_POSTGRES` (DSN администратора базы профиля labs) тот же набор идёт ещё на PostgreSQL; конкурентный резерв (24 покупателя на 7 единиц товара — продано ровно 7) проверяют тесты «Склада» (`WAREHOUSE_TEST_MYSQL`). Команды и примеры запросов — в `labs/orders/README.md`. Два упрощения там названы прямо: аутентификации пока нет (любой клиент читает и отменяет любой заказ), а DSN из `docker-compose.yml` — учебный: суперпользователь, пароль в репозитории, `sslmode=disable`. В продакшне сервис ходит в базу отдельной ролью без прав на DDL, с `sslmode=verify-full` и паролем из менеджера секретов.

Чем сервис богаче примера: цена фиксируется на момент заказа, а порядок резерва по возрастанию `product_id` (в примере он тоже есть) проверяет тест — без сортировки встречные заказы на PostgreSQL и MySQL ловят дедлок. Транзакция передаётся явно через `Store.Begin` → `Tx`. Повторная отмена идемпотентна: она возвращает `200` и тот же заказ, а не `409`, как в примере, — клиент может безопасно повторить запрос после таймаута. Ещё у запроса есть дедлайн, который доходит до драйвера, а ответ об ошибке несёт `request_id`. Дальше сервис растёт вместе с курсом. В модуле 11 появится gRPC рядом с REST, а резерв уйдёт в отдельный сервис «Склад» на MySQL — с сагой и компенсацией. В модуле 12 публикатор outbox начнёт отправлять `order.created` и `order.cancelled` в Kafka, добавятся метрики и трассировка. В модуле 14 — кеш каталога, защита от перегрузки и чтение с реплик.

## Как это в C#

- Сборка: в ASP.NET Core граф зависимостей собирает DI-контейнер (`builder.Services.AddScoped<…>()`), конфигурацию — `IConfiguration`, жизненный цикл — Generic Host. В Go всё это делает функция `run`: десяток строк, которые видно целиком и легко тестировать. `Microsoft.Extensions.*` заменяют стандартная библиотека и явный код.
- Ошибки: `UseExceptionHandler` и `ProblemDetails` прячут подробности исключения и превращают его в ответ. В Go это функция `fail`, единственное место, где `errors.Is`/`errors.As` переводят ошибку в статус. Доменные исключения (`OutOfStockException`) становятся значениями (`ErrOutOfStock`).
- Данные: вместо `DbContext` с change tracking и `SaveChanges` здесь SQL, написанный явно или сгенерированный `sqlc`, и явная транзакция, переданная параметром. Атомарный резерв в EF Core пришлось бы писать через `ExecuteUpdateAsync(… .Where(p => p.Available >= q))` или `FromSql`. В Go это просто SQL.
- Лимиты Kestrel (`MaxRequestBodySize`, `RequestHeadersTimeout`) включены по умолчанию. У `http.Server` и `json.Decoder` их нет, пока вы не задали `ReadHeaderTimeout` и `MaxBytesReader` сами.

## Ловушки

### `context.Background()` «чтобы запись не оборвалась»

```go
// не обрываем запись, если клиент отвалился
o, err := s.create(context.Background(), req.Items)
```

Если клиент ушёл или истёк дедлайн, сервер продолжает работать вхолостую. При перегрузке такие «осиротевшие» запросы занимают пул соединений и делают всё только хуже. Прокидывайте `r.Context()`. Если какая-то работа действительно должна пережить запрос (отправка письма), ей нужен свой контекст с таймаутом — `context.WithoutCancel(ctx)` плюс `WithTimeout` — и понятный владелец. Атомарность записи обеспечивает транзакция, а не отказ от отмены.

### Проверка «до транзакции»

```go
// УЯЗВИМО: остаток прочитан вне транзакции — между проверкой и списанием
// другой заказ успеет забрать тот же товар
db.QueryRowContext(ctx, `SELECT available FROM products WHERE id = ?`, id).Scan(&available)
if available < qty { return errOutOfStock }
tx.ExecContext(ctx, `UPDATE products SET available = available - ? WHERE id = ?`, qty, id)
```

Такая проверка не защищает даже от самого себя: две позиции одного товара по отдельности помещаются в остаток, а вместе — нет. Проверка и изменение должны быть одним атомарным шагом: `UPDATE … WHERE available >= ?` и `RowsAffected`, либо `SELECT … FOR UPDATE` внутри транзакции (урок 07).

### Ошибка, которую «не страшно» потерять

```go
tx.ExecContext(ctx, `INSERT INTO outbox ...`) // результат не проверен
return tx.Commit()
```

Outbox нужен ровно для того, чтобы событие не терялось. Проглоченная ошибка возвращает ту самую проблему двойной записи: заказ есть, события нет, и никто об этом не узнает. Внутри транзакции у каждой ошибки один путь: `return err`, а дальше `defer tx.Rollback()`.

## Задание

В `task.go` — сервис заказов на `database/sql` и `net/http`. Он работает, и функциональные тесты проходят, но ревью он бы не прошёл: в коде **семь** замечаний из чек-листа модуля. Найдите их и исправьте, не меняя API. Каждое замечание проверяет свой скрытый тест `TestReview…`.

```go
type Item struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

type Order struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Items  []Item `json:"items"`
}

func New(db *sql.DB) (*Service, error)
func (s *Service) Handler() http.Handler
```

`New` создаёт таблицы `products (id, name, available)`, `orders (id, status)`, `order_items (order_id, product_id, quantity)` и `outbox (id, type, payload)`; товары заводят тесты. Тесты открывают SQLite-файл с пулом из нескольких соединений и транзакциями, которые сразу берут блокировку записи, и вызывают обработчик напрямую. Ответы — JSON, ошибка — `{"error": "<текст>"}`.

- `POST /orders` с `{"items": [{"product_id": …, "quantity": …}, …]}` создаёт заказ со статусом `reserved` и резервирует товар: `available` каждого товара уменьшается на `quantity` позиции. Ответ — `201` и созданный `Order`, позиции — в порядке запроса. Позиции одного товара могут повторяться, их количества складываются. Заказ, резерв и событие `order.created` с `payload` вида `{"order_id":N}` в таблице `outbox` создаются **вместе или не создаются вовсе**.
  - тело не JSON, `items` пуст или длиннее 50, `quantity` вне `1..1000` — `400`, `"invalid request"`; тело больше 64 КиБ — `413`; лишние поля в JSON в этой задаче допустимы (в примере и в `labs/orders` их отклоняет `DisallowUnknownFields`, здесь это не входит в замечания);
  - товара нет — `422`, `"unknown product"`; товара не хватает (с учётом всех позиций заказа и одновременных заказов — остаток никогда не уходит в минус) — `409`, `"out of stock"`.
- `GET /orders/{id}` — `200` и заказ с позициями в порядке добавления; нет заказа или `id` не число — `404`, `"not found"`.
- `GET /orders` — `200` и все заказы по возрастанию `id`, у каждого — позиции; без заказов — `[]`. Число запросов к базе не зависит от числа заказов.
- `POST /orders/{id}/cancel` — заказ в статусе `reserved` переходит в `cancelled`, товар возвращается на склад, в `outbox` пишется `order.cancelled`; ответ — `200` и заказ. Уже отменённый — `409`, `"conflict"`; нет заказа — `404`.
- Любая другая ошибка (сбой базы, битые данные) — `500` и ровно `{"error": "internal error"}`; подробности — в журнал, не клиенту. Ни один запрос, в том числе завершившийся ошибкой, не оставляет занятым соединение пула или незавершённую транзакцию.
- Если контекст запроса отменён (клиент ушёл), заказ не создаётся; код ответа в этом случае не проверяется.

Чек-лист ревью — на странице `/review/`.
