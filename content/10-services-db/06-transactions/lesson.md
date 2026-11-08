---
title: Транзакции
summary: "BeginTx и sql.TxOptions, defer tx.Rollback(), транзакция передаётся явно через интерфейс DBTX, хелпер withTx, почему нельзя ходить в db внутри tx, NULL и sql.Null[T], COALESCE; аномалии и уровни изоляции — обзор."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 08-web/10-database-sql
  - 10-services-db/05-connection-pool
  - 04-errors/05-defer
docs:
  - title: "Go: Executing transactions"
    url: https://go.dev/doc/database/execute-transactions
  - title: "database/sql: TxOptions и IsolationLevel"
    url: https://pkg.go.dev/database/sql#TxOptions
  - title: "PostgreSQL: Transaction Isolation"
    url: https://www.postgresql.org/docs/18/transaction-iso.html
---

Транзакция в `database/sql` — это одно соединение пула, взятое в аренду. `tx, err := db.BeginTx(ctx, opts)` берёт соединение, выполняет на нём `BEGIN` и держит его до `tx.Commit()` или `tx.Rollback()`. Все запросы транзакции идут через методы `tx` — `ExecContext`, `QueryContext`, `QueryRowContext`, `PrepareContext`, — и только так они попадают в это соединение. Запрос через `db` уйдёт в другое соединение мимо транзакции. Невидимой «текущей транзакции», как у `TransactionScope`, нет: транзакцию передают явно.

Шаблон всегда один: сразу после успешного `BeginTx` пишут `defer tx.Rollback()`, в конце — `return tx.Commit()`. `Rollback` после `Commit` ничего не делает и лишь возвращает `sql.ErrTxDone`, а на любом раннем выходе — ошибке, панике — откатывает. Контекст из `BeginTx` действует на всю транзакцию: если его отменят, `database/sql` сам откатит транзакцию и вернёт соединение, а `Commit` вернёт ошибку.

`sql.TxOptions` задаёт `Isolation` (`sql.LevelReadCommitted`, `sql.LevelRepeatableRead`, `sql.LevelSerializable` и т.д.; `LevelDefault` — уровень базы по умолчанию) и `ReadOnly`. Это **просьба к драйверу**: `pgx` превращает её в `BEGIN ISOLATION LEVEL ... READ ONLY`, драйвер MySQL — в `SET TRANSACTION ISOLATION LEVEL` и `START TRANSACTION READ ONLY`, а `modernc.org/sqlite` молча игнорирует обе опции (SQLite и так сериализует запись).

**Где граница транзакции.** Функции хранилища не начинают транзакции сами — они принимают маленький интерфейс, который реализуют и `*sql.DB`, и `*sql.Tx`:

```go
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

Вызывающий — обычно сервис, который знает бизнес-операцию целиком («создать заказ и зарезервировать товар»), — решает, нужна ли транзакция, и передаёт `db` или `tx`. Так же устроен код, который генерирует `sqlc` (урок о миграциях и sqlc): там тоже есть интерфейс `DBTX` и метод `queries.WithTx(tx)`. Чтобы не повторять `BeginTx`/`Rollback`/`Commit` в каждом методе, их собирают в хелпер `withTx(ctx, db, func(tx *sql.Tx) error) error`: ошибка `fn` откатывает, `nil` фиксирует.

**`NULL`.** Колонку, где бывает `NULL`, читают в `sql.Null[T]` (обобщённый тип, Go 1.22), в `sql.NullString`/`sql.NullInt64`/… или в указатель (`*string` станет `nil`). При записи `NULL` получается из `nil`, из `sql.Null[T]{}` без `Valid` или из `nil`-указателя. Часто проще убрать `NULL` в самом запросе: `COALESCE(memo, '')`, `COALESCE(SUM(amount), 0)` — агрегат над пустым множеством возвращает `NULL`, и `Scan` в `int64` на нём падает.

**Изоляция — обзор.** Параллельные транзакции мешают друг другу по-разному:

| Аномалия | Что происходит |
| --- | --- |
| Грязное чтение | Видны незафиксированные изменения чужой транзакции |
| Неповторяемое чтение | Повторное чтение строки возвращает другое значение — его зафиксировали между чтениями |
| Фантом | Повторный запрос с тем же условием возвращает новые строки |
| Потерянное обновление | Две транзакции читают значение, каждая пишет своё вычисленное — одно изменение пропадает |
| Write skew | Две транзакции проверяют общее условие по разным строкам и обе пишут — условие нарушено, хотя каждая по отдельности была права |

Уровни по умолчанию разные: в PostgreSQL — Read Committed (каждый запрос видит свежий снимок, грязного чтения нет никогда), в MySQL InnoDB — Repeatable Read (снимок на транзакцию плюс gap-блокировки при записи), в SQL Server — Read Committed. Ни один из уровней по умолчанию не защищает от потерянного обновления в коде вида «прочитал — посчитал — записал». Как с ним бороться — блокировками, версией строки или атомарным `UPDATE`, — разобрано в уроке про блокировки. Repeatable Read и Serializable в PostgreSQL, ошибка `40001` и повтор транзакции — в уроке про MVCC и изоляцию.

## Как это в C#

- **`TransactionScope` → явный `tx`.** В .NET соединение, открытое внутри `using var scope = new TransactionScope(...)`, само записывается в «окружающую» транзакцию, и репозиторий о ней не знает. В Go транзакция не живёт ни в потоке, ни в контексте: функция получает `DBTX` параметром, а вызывающий передаёт ей `db` или `tx`. Ближе всего к Go — явная `DbTransaction` в ADO.NET, где каждой команде надо выставить `cmd.Transaction = tx`.
- **`SaveChanges` → вы сами решаете, что в транзакции.** EF Core копит изменения отслеживаемых сущностей и при `SaveChanges` записывает их одной транзакцией. В Go отслеживания изменений нет: состав транзакции — это запросы, которые вы явно выполнили через `tx`.
- **`Dispose` без `Commit` → `defer tx.Rollback()`.** `await using var tx = await conn.BeginTransactionAsync(IsolationLevel.RepeatableRead)` откатывается в `Dispose`, если не было `Commit`. В Go то же самое пишут явно, сразу после `BeginTx`. `IsolationLevel` из `System.Data` соответствует `sql.IsolationLevel` в `sql.TxOptions`.
- **`DBNull.Value` и `int?` → `sql.Null[T]`.** `reader.IsDBNull(i) ? null : reader.GetInt64(i)` — это `Scan` в `sql.Null[int64]` и проверка `Valid`.

## Под капотом

Две транзакции PostgreSQL над одной таблицей. На Read Committed видно, почему грязного чтения нет, а неповторяемое чтение и фантом есть. На Repeatable Read — как снимок защищает чтение и почему конкурентное обновление той же строки заканчивается ошибкой `40001`, после которой транзакцию повторяют целиком.

## Ловушки

### Запрос через `db` внутри транзакции

```go
err := withTx(ctx, db, func(tx *sql.Tx) error {
	if err := withdraw(ctx, tx, from, amount); err != nil {
		return err
	}
	return deposit(ctx, db, to, amount) // db вместо tx
})
```

`deposit` уйдёт в другое соединение: зачисление зафиксируется сразу и не откатится, если транзакция упадёт позже. Хуже того, второе соединение может упереться в блокировку строки, которую держит своя же транзакция, — или вовсе не получить соединения: с `SetMaxOpenConns(1)` (а под нагрузкой — с любым исчерпанным пулом) оно ждёт соединение, которое держит транзакция, и та ждёт его. Это дедлок внутри процесса, и база его не обнаружит. Передавайте `tx` и не держите `*sql.DB` в замыкании, где работаете с транзакцией.

### Запрос внутри цикла по `rows` той же транзакции

```go
rows, _ := tx.QueryContext(ctx, `SELECT id FROM orders WHERE status = 'new'`)
defer rows.Close()
for rows.Next() {
	// ...
	tx.ExecContext(ctx, `UPDATE orders SET status = 'sent' WHERE id = $1`, id)
}
```

У транзакции одно соединение, а протокол PostgreSQL и MySQL не позволяет отправить новый запрос, пока не дочитан результат предыдущего. Через `database/sql` `pgx` вернёт `driver: bad connection`, драйвер MySQL — `busy buffer`. SQLite такое разрешает, поэтому тесты на нём проходят, а в бою код падает. Сначала дочитайте строки в срез и закройте `rows`, потом делайте обновления — а лучше одним запросом `UPDATE ... WHERE status = 'new' RETURNING id`.

### Долгая транзакция

```go
err := withTx(ctx, db, func(tx *sql.Tx) error {
	if err := reserve(ctx, tx, order); err != nil {
		return err
	}
	return payments.Charge(ctx, order) // HTTP-вызов на секунды — внутри транзакции
})
```

Пока транзакция открыта, она держит соединение пула и блокировки изменённых строк: остальные запросы к этим строкам ждут, пул пустеет. В PostgreSQL долгая транзакция вдобавок не даёт `VACUUM` убрать старые версии строк. Внешние вызовы, ожидание пользователя и тяжёлые вычисления выносят за пределы транзакции, а согласованность с внешней системой обеспечивают иначе — например, outbox (урок `10-outbox`).

## Задание

Напишите резервирование товара под заказ — всё или ничего. Схема, типы, интерфейс `DBTX` и ошибки даны в шаблоне:

```go
type Line struct {
	SKU string
	Qty int64
}

type Order struct {
	ID    int64
	Note  sql.Null[string] // NULL — комментария нет
	Lines []Line
}

var ErrInvalid, ErrUnknownSKU, ErrOutOfStock, ErrNotFound error

func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error
func Reserve(ctx context.Context, db *sql.DB, orderID int64, note sql.Null[string], lines []Line) error
func GetOrder(ctx context.Context, q DBTX, id int64) (Order, error)
```

Таблицы `stock(sku, qty)`, `orders(id, note)` и `reservations(id, order_id, sku, qty)` создают тесты; драйвер SQLite подключают тоже они. Тесты ограничивают пул одним соединением и передают контекст с таймаутом.

- `WithTx` начинает транзакцию с контекстом `ctx` и вызывает `fn`. `fn` вернула `nil` — транзакция фиксируется, а ошибка `Commit` возвращается (так, чтобы `errors.Is` её находил). `fn` вернула ошибку — откат, и `WithTx` возвращает эту ошибку (можно обёрнутой). `fn` запаниковала — откат, и паника продолжается с тем же значением. Не удалось начать транзакцию — ошибка `BeginTx`, `fn` не вызывается. В любом случае соединение возвращается в пул.
- `Reserve` проверяет ввод до обращения к базе: пустой `lines`, строка с пустым `SKU` или `Qty <= 0` — `ErrInvalid`. Затем в одной транзакции (через `WithTx`) добавляет заказ `(orderID, note)` — `note` без `Valid` записывается как `NULL`, — и по каждой строке в порядке следования списывает `Qty` со склада и добавляет строку в `reservations`. Строки с одинаковым `SKU` обрабатываются по отдельности. Первая строка, по которой товара нет на складе, даёт `ErrUnknownSKU`, а по которой остаток меньше `Qty` (с учётом уже списанного этим заказом) — `ErrOutOfStock`; дальше строки не обрабатываются. При любой ошибке, включая занятый `orderID` (ошибку базы, не `nil`), склад, `orders` и `reservations` остаются как были. Ошибки можно оборачивать — тесты проверяют `errors.Is`.
- Одновременные `Reserve` из многих горутин корректны: остаток не уходит в минус, каждый заказ либо проходит целиком, либо возвращает `ErrOutOfStock`.
- `GetOrder` работает и с `*sql.DB`, и с `*sql.Tx` (внутри транзакции видит её незафиксированные изменения). Возвращает заказ: `Note` — `{Valid: false}` для `NULL`, `Lines` — строки резерва в том порядке, в каком они передавались в `Reserve`. Заказа нет — ошибка с `errors.Is(err, ErrNotFound)` и нулевой `Order{}`.
