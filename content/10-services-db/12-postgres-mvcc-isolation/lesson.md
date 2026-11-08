---
title: "PostgreSQL: MVCC и уровни изоляции"
summary: "Версии строк и снимки, VACUUM и раздувание от долгих транзакций; Read Committed, Repeatable Read и Serializable (SSI) на настоящем PostgreSQL: неповторяемое чтение, 40001, write skew и повтор транзакции."
difficulty: 5
minutes: 45
runner:
  race: true
  labs: [postgres]
prerequisites:
  - 10-services-db/06-transactions
  - 10-services-db/07-locking
  - 10-services-db/11-postgres-pgx
docs:
  - title: "PostgreSQL: изоляция транзакций"
    url: https://www.postgresql.org/docs/18/transaction-iso.html
  - title: "PostgreSQL: регулярная очистка (VACUUM)"
    url: https://www.postgresql.org/docs/18/routine-vacuuming.html
  - title: "pgx.TxOptions"
    url: https://pkg.go.dev/github.com/jackc/pgx/v5#TxOptions
---

PostgreSQL не обновляет строку на месте. `UPDATE` записывает новую **версию** строки, а старую помечает удалённой: в служебных полях версии хранится, какая транзакция её создала (`xmin`) и какая удалила (`xmax`). Каждый запрос работает со **снимком** — списком транзакций, чьи изменения ему видны, — и из нескольких версий строки берёт ту, что видна снимку. Поэтому читатели не ждут писателей, а писатели — читателей: в примере транзакция `a` видит версию `(0,1)`, хотя `b` уже записала `(0,2)`. Ждут друг друга только два писателя одной строки.

Расплата — мёртвые версии. Их убирает `VACUUM` (обычно фоновый autovacuum): отмечает место свободным для новых версий, но файл не уменьшает (это делает `VACUUM FULL` под эксклюзивной блокировкой таблицы). Удалить версию можно, только если её не видит ни один живой снимок. Одна забытая транзакция — `BEGIN` без `Commit`, пользователь в состоянии `idle in transaction`, многочасовой отчёт — держит горизонт очистки для всей базы, и таблицы пухнут: в примере таблица выросла с 89 до 133 страниц, пока был открыт один снимок. Защита — короткие транзакции без сетевых вызовов внутри, `idle_in_transaction_session_timeout` на сервере и мониторинг `pg_stat_activity` (`xact_start`, `state`).

Уровней изоляции в PostgreSQL фактически три (Read Uncommitted работает как Read Committed — грязного чтения нет вообще):

- **Read Committed** — по умолчанию. Снимок берётся заново для каждого запроса: два `SELECT` в одной транзакции могут вернуть разное (неповторяемое чтение). Если `UPDATE` упёрся в строку, которую параллельно изменили, он дождётся чужого `COMMIT` и применится к новой версии — поэтому `SET qty = qty - 1` безопасен, а «прочитать в Go, посчитать, записать» теряет обновления (урок 07).
- **Repeatable Read** — один снимок на всю транзакцию (snapshot isolation). Фантомов в PostgreSQL на этом уровне тоже нет. Попытка изменить строку, которую после начала снимка изменила и зафиксировала другая транзакция, — ошибка `40001` (`could not serialize access due to concurrent update`).
- **Serializable** — Repeatable Read плюс SSI (Serializable Snapshot Isolation): сервер отслеживает, кто прочитал данные, которые другой изменил, и если зависимости складываются в цикл — обрывает одну транзакцию с `40001`. Так ловится write skew, который снимок пропускает. Ошибки бывают и ложными, и на `Commit` — приложение обязано уметь повторять.

Уровень задаётся при начале транзакции: в pgx — `pgx.TxOptions{IsoLevel: pgx.Serializable}` (есть ещё `AccessMode: pgx.ReadOnly` и `DeferrableMode`), в `database/sql` — `sql.TxOptions{Isolation: sql.LevelSerializable}`. Повторять при `40001` и `40P01` (дедлок) нужно **всю транзакцию** с `BEGIN`: после любой ошибки транзакция PostgreSQL отклоняет все команды до `ROLLBACK`, а всё прочитанное в ней устарело. Число попыток ограничивают, между попытками — backoff с джиттером (урок 07), а побочные эффекты вне базы (письма, HTTP) не делают внутри повторяемой функции.

## Как это в C#

- `IsolationLevel` в ADO.NET и `pgx.TxOptions` задают одно и то же — уровни реализует сервер. Но если вы привыкли к SQL Server: там Read Committed по умолчанию построен на блокировках (читатель ждёт писателя), пока не включён `READ_COMMITTED_SNAPSHOT`. В PostgreSQL MVCC всегда, а уровень `Snapshot` из SQL Server ближе всего к Repeatable Read в PostgreSQL.
- `TransactionScope` по умолчанию открывает транзакцию с уровнем `Serializable` и неявно подхватывает её во всех командах внутри `using`. В Go окружающей транзакции нет: `pgx.Tx` передаётся явно, уровень выбирается явно.
- `EnableRetryOnFailure` в EF Core (`NpgsqlRetryingExecutionStrategy`) повторяет операцию при `40001`/`40P01` и других временных ошибках. В Go такой стратегии из коробки нет — это функция `RunInTx` из задания на 30 строк, и она у вас под контролем: что повторять, сколько раз, с какой паузой.

## Ловушки

### Повтор одного запроса вместо всей транзакции

```go
_, err := tx.Exec(ctx, `UPDATE accounts SET balance = $1 WHERE id = $2`, b, id)
if IsRetryable(err) {
	_, err = tx.Exec(ctx, `UPDATE accounts SET balance = $1 WHERE id = $2`, b, id)
	// ERROR: current transaction is aborted, commands ignored until end of transaction block (25P02)
}
```

После ошибки транзакция PostgreSQL сломана: любая команда, кроме `ROLLBACK`, вернёт `25P02`. И даже если бы команда прошла, значение `b` посчитано по устаревшему снимку. Повторять — с `BEGIN`, заново читая всё, от чего зависит запись.

### Serializable только у части транзакций

```go
// «главная» операция — Serializable
RunInTx(ctx, pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, 5, takeShift)
// фоновая задача правит те же строки на уровне по умолчанию
pool.Exec(ctx, `UPDATE doctors SET on_call = false WHERE name = $1`, name)
```

SSI отслеживает зависимости только между сериализуемыми транзакциями. Запрос на Read Committed для него невидим, и write skew снова возможен. Если инвариант держится на Serializable, все, кто читает и пишет эти данные, должны работать на Serializable (проще всего — `default_transaction_isolation` для роли приложения).

### Долгая транзакция ради «удобства»

```go
tx, _ := pool.Begin(ctx)
defer tx.Rollback(ctx)
rows := loadOrders(ctx, tx)
for _, o := range rows {
	sendEmail(o) // секунды на каждый заказ, транзакция открыта
}
tx.Commit(ctx)
```

Пока цикл идёт, транзакция держит снимок (VACUUM не чистит базу), блокировки и соединение пула. Читайте в короткой транзакции, медленную работу делайте после `Commit`, а надёжную отправку событий — через outbox (урок 10).

## Задание

Напишите помощник, который выполняет функцию в транзакции и повторяет её целиком при конфликтах сериализации:

```go
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

var ErrRetriesExhausted = errors.New("retries exhausted")

func IsRetryable(err error) bool
func RunInTx(ctx context.Context, db Beginner, opts pgx.TxOptions, maxAttempts int,
	fn func(ctx context.Context, tx pgx.Tx) error) error
```

`*pgxpool.Pool` и `*pgx.Conn` уже реализуют `Beginner`; тесты передают фейк.

**`IsRetryable(err)`** — `true`, если в цепочке `err` есть `*pgconn.PgError` с кодом `40001` или `40P01`; иначе (в том числе для `nil` и для ошибок без `*pgconn.PgError`) — `false`.

**`RunInTx`** делает до `maxAttempts` попыток; если `maxAttempts < 1`, попытка одна. Каждая попытка:

1. Если `ctx.Err() != nil` — вернуть ошибку, для которой `errors.Is(err, ctx.Err())`, без `BeginTx`. Это проверяется и перед первой попыткой, и перед каждым повтором.
2. `db.BeginTx(ctx, opts)` с переданными `opts`. Ошибка `BeginTx` возвращается сразу, без повторов (обёрнутой или как есть — `errors.Is` должен её находить).
3. `fn(ctx, tx)` вызывается ровно один раз с транзакцией этой попытки.
4. Если `fn` вернула ошибку — вызвать `tx.Rollback`, `Commit` не вызывать. Если `fn` паникует — вызвать `tx.Rollback` и пропустить панику дальше с тем же значением.
5. Если `fn` вернула `nil` — вызвать `tx.Commit`. Вызывать ли `Rollback` после `Commit`, успешного или нет, — на ваше усмотрение (после успешного `Commit` pgx возвращает `pgx.ErrTxClosed`, это не ошибка `RunInTx`).

Итог попытки — ошибка `fn` или ошибка `Commit`:

- `nil` — `RunInTx` возвращает `nil`;
- ошибка, для которой `IsRetryable` ложно, — возвращается сразу (обёрнутой или как есть, `errors.Is` должен её находить), без повторов и без `ErrRetriesExhausted`;
- ошибка, для которой `IsRetryable` истинно, — следующая попытка; если попытки кончились — вернуть ошибку, для которой одновременно `errors.Is(err, ErrRetriesExhausted)` и `errors.As(err, &pgErr)` находит `*pgconn.PgError` последней попытки.

Ошибку `Rollback` можно игнорировать. Пауз между попытками не нужно.

С профилем labs скрытый тест воспроизводит write skew «два дежурных врача» на настоящем PostgreSQL: две горутины одновременно вызывают `RunInTx` с `Serializable`, первые попытки гарантированно пересекаются, и после повтора дежурным должен остаться ровно один.
