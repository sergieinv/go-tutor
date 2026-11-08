---
title: PostgreSQL через pgx
summary: "pgx и pgxpool напрямую или через database/sql, $1, RETURNING, ON CONFLICT, CollectRows, jsonb и массивы, Batch и COPY, LISTEN/NOTIFY, advisory locks, коды ошибок *pgconn.PgError — на настоящем PostgreSQL."
difficulty: 4
minutes: 40
runner:
  labs: [postgres]
prerequisites:
  - 08-web/10-database-sql
  - 10-services-db/05-connection-pool
  - 10-services-db/06-transactions
  - 04-errors/04-wrapping
docs:
  - title: "Пакет github.com/jackc/pgx/v5"
    url: https://pkg.go.dev/github.com/jackc/pgx/v5
  - title: "Пакет pgxpool"
    url: https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
  - title: "PostgreSQL: коды ошибок (SQLSTATE)"
    url: https://www.postgresql.org/docs/18/errcodes-appendix.html
---

`github.com/jackc/pgx/v5` — основной драйвер PostgreSQL в Go, и у него два лица. Родной API — `pgx.Conn` (одно соединение) и `pgxpool.Pool` (пул): он знает типы PostgreSQL (`jsonb`, массивы, `numeric`, `interval`), работает по бинарному протоколу и даёт то, чего нет в `database/sql`, — `Batch`, `COPY`, `LISTEN/NOTIFY`. Второе лицо — пакет `pgx/v5/stdlib`, драйвер `"pgx"` для `database/sql`: его берут, когда код или библиотека работает через `*sql.DB` (goose, sqlc в режиме `database/sql`, общий код для нескольких СУБД). Новый сервис только на PostgreSQL обычно пишут на `pgxpool`.

Пул создают один раз: `pgxpool.ParseConfig(dsn)`, поправить поля, `pgxpool.NewWithConfig`. Значения по умолчанию в v5.11: `MaxConns` — большее из 4 и числа CPU, `MinConns` — 0, `MaxConnLifetime` — час, `MaxConnIdleTime` — 30 минут, `HealthCheckPeriod` — минута; их же можно задать прямо в DSN (`pool_max_conns=10`). Шифрование тоже задаёт DSN: без `sslmode` pgx, как и libpq, работает в режиме `prefer` — пробует TLS, не проверяя сертификат, а если сервер TLS не умеет, молча откатывается на открытый канал. От перехвата это не защищает: в продакшне ставьте `sslmode=verify-full` и `sslrootcert=<CA сервера>`. `sslmode=disable` в DSN профиля labs допустим только потому, что база — в docker-сети на той же машине. Методы те же, что у `*sql.DB`, только без суффикса `Context` — контекст обязателен всегда: `Exec`, `Query`, `QueryRow`. Плейсхолдеры — `$1`, `$2`. Для вставки с ключом — `RETURNING id` (`LastInsertId` в PostgreSQL нет), для «вставить или обновить» — `INSERT … ON CONFLICT (key) DO UPDATE SET col = EXCLUDED.col` или `DO NOTHING`. Строки собирают `pgx.CollectRows(rows, pgx.RowToStructByName[T])` — по тегам `db`, с закрытием `rows` и проверкой ошибки внутри.

Типы PostgreSQL маппятся без обёрток: `[]int64` и `[]string` уходят массивом (`WHERE id = ANY($1)` вместо строки `IN (?, ?, ?)`), структура в параметре `jsonb` кодируется через `encoding/json` и так же сканируется обратно. Ошибка сервера — `*pgconn.PgError`: поле `Code` (SQLSTATE), `ConstraintName`, `Detail`. Сравнивайте код, а не текст: `23505` — нарушение уникальности, `23503` — внешнего ключа, `40001` и `40P01` — конфликт сериализации и дедлок (повторить транзакцию, урок 12), `57014` — запрос отменён на сервере (`statement_timeout`, `pg_cancel_backend`). Отмена `context` к `57014` в pgx v5 по умолчанию не приводит: драйвер рвёт соединение и возвращает ошибку, для которой `errors.Is(err, context.DeadlineExceeded)` (или `Canceled`), без `*pgconn.PgError`.

## Как это в C#

- Npgsql ↔ pgx. `NpgsqlDataSource` — это `pgxpool.Pool`: пул внутри, создаётся один раз на приложение. Разница — где живёт пул: в ADO.NET он спрятан в провайдере и включается строкой подключения, в Go это явный объект, который вы создаёте в `main` и передаёте зависимостям.
- `PostgresException` с `SqlState` и `PostgresErrorCodes.UniqueViolation` ↔ `*pgconn.PgError` с `Code == "23505"`. Исключение ловят `catch (...) when`, ошибку в Go — `errors.As` по цепочке обёрток.
- `NpgsqlBatch`, `BeginBinaryImport`, событие `Notification` + `WaitAsync` ↔ `pgx.Batch`, `CopyFrom`, `WaitForNotification`. Возможности те же — это возможности протокола PostgreSQL.
- Маппинг строк. Dapper или EF Core материализуют объекты по именам колонок; в pgx это `RowToStructByName` — без отслеживания изменений: обновить строку можно только явным `UPDATE`.

## Ловушки

### Соединение занято, пока не закрыты `rows`

```go
conn, _ := pgx.Connect(ctx, dsn)
rows, _ := conn.Query(ctx, `SELECT id FROM orders`)
for rows.Next() {
	rows.Scan(&id)
	_, err := conn.Exec(ctx, `UPDATE ...`, id) // err: conn busy
}
```

У одного `pgx.Conn` в каждый момент идёт один запрос: пока `rows` не дочитаны и не закрыты, любой другой запрос на нём возвращает `conn busy`. У пула то же правило звучит иначе: соединение не вернётся в пул, пока не закрыты `rows`. Сначала дочитайте строки (`CollectRows`), потом выполняйте запросы, — а лучше замените цикл одним `UPDATE … WHERE id = ANY($1)`.

### `NULL` в `string`

```go
var email string
err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, id).Scan(&email)
// err: can't scan into dest[0] (col: email): cannot scan NULL into *string
```

Колонка без `NOT NULL` рано или поздно вернёт `NULL`, а в `string` его не положить. Сканируйте в `*string` (`NULL` станет `nil`), `pgtype.Text` (поле `Valid`) или пишите `COALESCE(email, '')` в запросе.

### Батч — одна неявная транзакция

```go
b := &pgx.Batch{}
b.Queue(`INSERT INTO t VALUES (1)`)
b.Queue(`INSERT INTO t VALUES (2)`) // нарушает уникальность
b.Queue(`INSERT INTO t VALUES (3)`)
err := pool.SendBatch(ctx, b).Close() // 23505, и строки 1 в таблице нет
```

pgx отправляет батч одним пакетом с одним `Sync` в конце, и сервер выполняет всё до `Sync` как неявную транзакцию: ошибка откатывает и предыдущие команды, а последующие не выполняются. Результаты `SendBatch` обязательно закрыть (`Close`) — до этого соединение занято. Нужна независимость команд — отправляйте их отдельно.

## Задание

Напишите две функции слоя хранилища: перевод ошибок PostgreSQL в доменные и построение upsert-запроса. Доменные ошибки объявлены в шаблоне:

```go
var (
	ErrDuplicate     = errors.New("duplicate")
	ErrForeignKey    = errors.New("foreign key violation")
	ErrRetry         = errors.New("retryable conflict")
	ErrCanceled      = errors.New("query canceled")
	ErrInvalidUpsert = errors.New("invalid upsert")
)

func Classify(err error) error
func BuildUpsert(table string, cols, conflict, update []string) (string, error)
```

**`Classify(err)`**:

- `nil` → `nil`.
- Если в цепочке `err` есть `*pgconn.PgError` (пакет `github.com/jackc/pgx/v5/pgconn`) с одним из кодов: `23505` → `ErrDuplicate`, `23503` → `ErrForeignKey`, `40001` и `40P01` → `ErrRetry`, `57014` → `ErrCanceled`. Возвращаемая ошибка удовлетворяет `errors.Is(res, <доменная>)` и при этом сохраняет исходную цепочку: `errors.Is(res, err)` истинно, `errors.As(res, &pgErr)` находит тот же `*pgconn.PgError`. С другими доменными ошибками `res` не совпадает.
- Любой другой код или ошибка без `*pgconn.PgError` → тот же `err` без изменений.

**`BuildUpsert(table, cols, conflict, update)`** возвращает текст запроса для pgx или пустую строку и ошибку, для которой `errors.Is(err, ErrInvalidUpsert)`. Ошибка, если:

- `cols` или `conflict` пусты;
- `table` или любое имя в `cols`, `conflict`, `update` не соответствует правилу: первая буква `a–z` или `_`, дальше `a–z`, `0–9`, `_`, всего от 1 до 63 символов (только ASCII в нижнем регистре, без точки, кавычек и пробелов);
- в `cols`, `conflict` или `update` есть повтор;
- колонка из `conflict` или `update` отсутствует в `cols`, или колонка из `update` есть в `conflict`.

Формат результата (имена — в двойных кавычках, значения — плейсхолдеры `$1..$N` в порядке `cols`, части разделены одним пробелом, без `;` в конце):

```
INSERT INTO "items" ("sku", "name", "price") VALUES ($1, $2, $3) ON CONFLICT ("sku") DO UPDATE SET "name" = EXCLUDED."name", "price" = EXCLUDED."price"
```

Колонки в `ON CONFLICT` и `SET` идут в порядке `conflict` и `update`. Если `update` пуст (`nil` или длины 0) — вместо `DO UPDATE SET …` пишется `DO NOTHING`.

С профилем labs скрытые тесты дополнительно выполняют построенные запросы на настоящем PostgreSQL и классифицируют настоящие ошибки: дубликат ключа, нарушение внешнего ключа и `statement_timeout`.
