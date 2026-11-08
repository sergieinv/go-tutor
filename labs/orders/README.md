# Сервис «Заказы»

Сквозной учебный сервис курса «Go для C#-разработчика». Его разбирает
итоговый урок модуля 10 («Сервисы и базы данных»), а дальше он растёт в
модулях 11, 12 и 14. Это не упражнение, а сервис, написанный так, как его
стоит писать в продакшне: читайте его как пример.

Что умеет:

- создать заказ: позиции, цены на момент заказа, резервирование товара на
  складе — всё в одной транзакции вместе с событием `order.created` в
  таблице `outbox`;
- показать заказ и остаток товара;
- отменить заказ: товар возвращается на склад, в `outbox` пишется
  `order.cancelled`. Повторная отмена безопасна (идемпотентна).

Статусы: `new` → `reserved` → `cancelled` (и `new` → `cancelled`). Сейчас
заказ сразу создаётся в `reserved`: резерв делается в той же транзакции.
`new` станет видимым в модуле 11, когда резерв уйдёт в отдельный «Склад».

Одна и та же логика работает на PostgreSQL, MySQL и SQLite — отличия
диалектов собраны в пакетах хранилищ и видны в коде.

## Структура

```
labs/orders/
├── cmd/orders/main.go        тонкий main → run(ctx, args, getenv, stdout): конфигурация,
│                             миграции, сборка зависимостей, HTTP-сервер, graceful shutdown
├── internal/
│   ├── orders/               домен: Order, Item, Product, статусы, доменные ошибки,
│   │                         Service; интерфейсы Store и Tx объявлены здесь (у потребителя)
│   ├── httpapi/              JSON API на net/http.ServeMux: DTO, middleware
│   │                         (request id, журнал, recover, дедлайн), ошибки → статусы
│   ├── store/
│   │   ├── postgres/         pgxpool + sqlc (query.sql → sqlcgen/)
│   │   ├── mysql/            database/sql + go-sql-driver/mysql + sqlc
│   │   └── sqlite/           database/sql + modernc.org/sqlite + sqlc
│   └── storetest/            контрактный набор тестов для всех трёх хранилищ
├── migrations/               миграции goose, встроенные в бинарник (embed)
│   ├── postgres/  mysql/  sqlite/
├── sqlc.yaml                 генерация кода доступа к базе
└── Dockerfile                multi-stage, финальный образ scratch, не root
```

Зависимости — только драйверы, `goose` и `golang.org/x/sync`, тех же версий,
что в песочнице учебника. Ни роутеров, ни DI-контейнеров, ни конфиг-библиотек:
всё это в Go закрывает стандартная библиотека и десяток строк в `main`.

## Ключевые решения

- **Атомарный резерв.** Остаток проверяет и списывает сама база:
  `UPDATE stock SET available = available - $1 WHERE product_id = $2 AND available >= $1`.
  0 затронутых строк — `ErrOutOfStock`. Схема «прочитал остаток, сравнил,
  записал» продала бы больше, чем есть.
- **Порядок блокировок.** Позиции резервируются по возрастанию `product_id`,
  поэтому встречные заказы не ловят deadlock (тест это проверяет: без
  сортировки PostgreSQL и MySQL падают с `deadlock detected`).
- **Транзакции явно.** `Store.Begin` возвращает `Tx`, сервис передаёт её из
  метода в метод и закрывает идиомой `defer tx.Rollback(ctx)` + `Commit`.
  Никакого «окружающего» `TransactionScope`.
- **Outbox.** Событие пишется в той же транзакции, что и заказ, — его не
  потерять и не отправить для откатившегося заказа. Публикатора пока нет.
- **Ошибки.** Доменные `ErrNotFound`, `ErrOutOfStock`, `ErrConflict`,
  `*ValidationError`; в HTTP-статусы они переводятся в одном месте
  (`httpapi/errors.go`). Наружу — код и безопасное сообщение, подробности —
  в журнал с `request_id`.
- **Безопасно по умолчанию.** Таймауты `http.Server`, дедлайн на обработку
  запроса (доходит до драйвера), `MaxBytesReader`, `DisallowUnknownFields`,
  только плейсхолдеры в SQL, DSN не пишется в журнал, образ без оболочки и
  не от root.

## Чего пока нет: не выставляйте наружу

- **Аутентификации и авторизации.** `customer_id` берётся из тела запроса
  как есть, любой клиент может прочитать и отменить любой заказ. Проверка
  личности и прав появится в модуле 11; до этого сервис — только на
  `localhost` (в `docker-compose.yml` порт открыт на `127.0.0.1`).
- **Продакшн-настроек базы.** DSN в `docker-compose.yml` — учебный: суперпользователь
  `postgres`, пароль в открытом виде и `sslmode=disable` (база в docker-сети
  на той же машине). В продакшне: отдельная роль сервиса только с правами
  `SELECT/INSERT/UPDATE` на свои таблицы, миграции — под ролью-владельцем
  схемы отдельным шагом деплоя (урок 8 модуля 10), `sslmode=verify-full` с
  `sslrootcert`, пароль — из менеджера секретов, а не из файла в репозитории.

## Запуск

В профиле `labs` сервис поднимается вместе с PostgreSQL:

```sh
docker compose --profile labs up --build
```

Сервис слушает `localhost:8090`, миграции применяет при старте и заводит
демо-каталог: 1 — «Клавиатура» (10 шт.), 2 — «Мышь» (25 шт.), 3 — «Монитор» (3 шт.).

```sh
# живость и готовность (пинг базы)
curl -s localhost:8090/healthz
curl -s localhost:8090/readyz

# остаток товара
curl -s localhost:8090/products/3

# создать заказ → 201, Location: /orders/1
curl -si -X POST localhost:8090/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_id": 42, "items": [{"product_id": 3, "quantity": 2}, {"product_id": 1, "quantity": 1}]}'

# заказ и остаток после резерва (available: 1)
curl -s localhost:8090/orders/1
curl -s localhost:8090/products/3

# сверх остатка → 409 {"error":"out_of_stock",...}
curl -si -X POST localhost:8090/orders \
  -H 'Content-Type: application/json' \
  -d '{"customer_id": 7, "items": [{"product_id": 3, "quantity": 2}]}'

# отмена → status: cancelled, остаток снова 3
curl -s -X POST localhost:8090/orders/1/cancel
curl -s localhost:8090/products/3

# события в outbox
docker compose exec postgres psql -U postgres -d orders -c 'SELECT id, event_type, payload FROM outbox'
```

Без Docker, на SQLite:

```sh
cd labs/orders
ORDERS_DB=sqlite ORDERS_DSN=orders.db go run ./cmd/orders
```

Конфигурация — переменные окружения:

| Переменная | Значение |
|---|---|
| `ORDERS_DB` | `postgres`, `mysql` или `sqlite` (обязательно) |
| `ORDERS_DSN` | строка подключения; для SQLite — путь к файлу (обязательно) |
| `ORDERS_ADDR` | адрес HTTP, по умолчанию `:8080` |

Ошибки ответа — JSON вида `{"error": "out_of_stock", "message": "...", "request_id": "..."}`;
у ошибок валидации (`422`) есть ещё `field`. Коды: `400` — не JSON или плохой id,
`404` — нет заказа/товара, `409` — нет остатка или конфликт статуса,
`413` — тело больше 64 КиБ, `415` — не `application/json`, `422` — валидация.

## Тесты

```sh
cd labs/orders
go test -race ./...
```

Без переменных окружения контрактный набор идёт против SQLite, а тесты
PostgreSQL и MySQL пропускаются. С профилем `labs`:

```sh
ORDERS_TEST_POSTGRES='postgres://postgres:labs@127.0.0.1:55432/postgres?sslmode=disable' \
ORDERS_TEST_MYSQL='root:labs@tcp(127.0.0.1:53306)/' \
go test -race -count=3 ./...
```

Переменные — DSN администратора: каждый подтест создаёт свою базу
`orders_test_<случайное>` и удаляет её в `t.Cleanup`. Один и тот же набор
(`internal/storetest`) проверяет все три хранилища, включая конкурентное
резервирование (24 покупателя на 7 единиц товара — продано ровно 7) и
параллельную отмену (остаток возвращается ровно один раз). HTTP-слой
тестируется через `httptest` на SQLite.

## Генерация кода

Запросы — в `internal/store/<db>/query.sql`, схема для sqlc — те же миграции.
После правки запросов или миграций:

```sh
cd labs/orders
sqlc generate   # sqlc v1.31.1
```

Сгенерированные пакеты `sqlcgen` закоммичены, руками их не правят.
`query.sql` для SQLite — только ASCII: парсер sqlite в sqlc v1.31 сбивается на
кириллице в комментариях.

## Что дальше

- **Модуль 11** — gRPC рядом с REST; резервирование уходит в отдельный
  сервис «Склад» (MySQL), заказ проходит статус `new`, отмена резерва —
  компенсирующее действие (saga); TLS и аутентификация в interceptors.
- **Модуль 12** — публикатор outbox отправляет `order.created` и
  `order.cancelled` в Kafka (`Store.PendingEvents` уже есть); метрики,
  трассировка и структурные журналы.
- **Модуль 14** — кеш каталога с защитой от stampede, load shedding на
  создании заказов, шардирование по покупателю (`customer_id`), чтение
  истории заказов с реплик с учётом задержки репликации.
