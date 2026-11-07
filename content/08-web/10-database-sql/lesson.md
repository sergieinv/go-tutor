---
title: database/sql
summary: Пул *sql.DB, ExecContext/QueryContext/QueryRowContext, плейсхолдеры вместо Sprintf, rows.Close и rows.Err, sql.ErrNoRows, NULL, транзакции с defer Rollback — на SQLite.
difficulty: 4
minutes: 35
prerequisites:
  - 08-web/07-request-context
  - 04-errors/02-sentinel-errors
  - 04-errors/05-defer
docs:
  - title: "Пакет database/sql"
    url: https://pkg.go.dev/database/sql
  - title: "Go: Accessing relational databases"
    url: https://go.dev/doc/database/
  - title: "modernc.org/sqlite"
    url: https://pkg.go.dev/modernc.org/sqlite
---

`database/sql` — это ADO.NET Go: общий интерфейс к SQL-базам из стандартной библиотеки, а конкретную СУБД подключает драйвер. Драйвер регистрируется импортом ради побочного эффекта — `import _ "modernc.org/sqlite"` (SQLite на чистом Go, имя драйвера `"sqlite"`), — после чего `sql.Open("sqlite", dsn)` возвращает `*sql.DB`. Но `Open` ничего не подключает: он проверяет аргументы и готовит **пул соединений**. Первая ошибка подключения появится при первом запросе — проверяйте связь при старте через `db.PingContext(ctx)`.

`*sql.DB` — не соединение, а пул: безопасен для горутин, сам открывает и переиспользует соединения. Его создают один раз на программу и передают зависимостям, а не открывают на каждый запрос. Методы три: `ExecContext` — для `INSERT`/`UPDATE`/`DDL` (результат — `sql.Result` с `LastInsertId` и `RowsAffected`), `QueryContext` — для многих строк (`*sql.Rows`, обход `for rows.Next()`, затем обязательно `rows.Err()`), `QueryRowContext` — для одной строки (ошибку, в том числе `sql.ErrNoRows`, возвращает `Scan`). Значения передаются только плейсхолдерами (`?` в SQLite и MySQL, `$1` в PostgreSQL): драйвер отправляет их отдельно от текста запроса, поэтому SQL-инъекция невозможна в принципе (подробно — в модуле 9 «Безопасность»).

`NULL` читают в `sql.NullString`, `sql.NullInt64`, обобщённый `sql.Null[T]` (Go 1.22) или в указатель: `*string` станет `nil`. Транзакция — `tx, err := db.BeginTx(ctx, nil)`, сразу `defer tx.Rollback()`, все запросы через `tx`, в конце `tx.Commit()`. Откат после успешного `Commit` ничего не делает, поэтому `defer` безопасен и гарантирует, что ранний `return` не оставит транзакцию открытой. Пул, уровни изоляции, PostgreSQL через `pgx`, генерация кода `sqlc` и миграции — в модуле 10 «Сервисы и базы данных».

## Как это в C#

- `DbConnection` + `DbCommand` + `DbDataReader` → `*sql.DB` + `QueryContext` + `*sql.Rows`. Разница: в ADO.NET вы открываете соединение сами (`using var conn = ...; conn.Open()`), и пул прячется в провайдере; в Go вы работаете с пулом, а соединение он выдаёт на время запроса. Ближе всего к этому `DbDataSource` (.NET 7+): `dataSource.CreateCommand(sql)` берёт соединение из пула сам.
- `cmd.Parameters.AddWithValue("@id", id)` → аргументы после SQL: `db.QueryRowContext(ctx, "... WHERE id = ?", id)`. Конкатенация строк опасна одинаково в обоих языках.
- `DBNull.Value` → `nil` в аргументах и `sql.NullString`/указатель при чтении. `reader.IsDBNull(i)` → поле `Valid`.
- Dapper (`conn.Query<User>(sql, new { id })`) ближе всего к Go-стилю: SQL пишется руками, маппинг тонкий. Аналога EF Core с LINQ и отслеживанием изменений в стандартной библиотеке нет — в Go обычно пишут SQL сами или генерируют код по нему (`sqlc`).

## Ловушки

### `:memory:` и пул

```go
db, _ := sql.Open("sqlite", ":memory:")
db.Exec(`CREATE TABLE t (x INTEGER)`)
// другое соединение пула — другая, пустая база:
_, err := db.Exec(`INSERT INTO t VALUES (1)`) // иногда: no such table: t
```

Для SQLite каждое соединение с `:memory:` — отдельная база. Пока пул обходится одним соединением, всё работает, под нагрузкой — таблицы «пропадают». Для тестов используйте файл во временном каталоге (`t.TempDir()`) или ограничьте пул: `db.SetMaxOpenConns(1)`.

### Незакрытые `rows` держат соединение

```go
rows, err := db.QueryContext(ctx, `SELECT name FROM users`)
if err != nil {
	return err
}
for rows.Next() {
	if bad { return errStop } // rows не закрыт — соединение не вернулось в пул
}
```

`rows.Next()` закрывает `rows` сам, только когда строки кончились. Любой ранний выход без `rows.Close()` оставляет соединение занятым; через несколько таких запросов пул исчерпан, и программа висит. Пишите `defer rows.Close()` сразу после проверки ошибки и проверяйте `rows.Err()` после цикла — иначе обрыв посреди выборки выглядит как «строк больше нет».

### `QueryRow` без `Scan`

```go
db.QueryRowContext(ctx, `UPDATE accounts SET balance = 0 WHERE id = ?`, id) // ошибка потеряна
```

`QueryRow` не возвращает ошибку — её хранит `*sql.Row` до вызова `Scan`. Без `Scan` ошибка теряется, а соединение остаётся занятым. Для запросов без результата — `ExecContext`.

## Задание

Напишите репозиторий счетов поверх `*sql.DB`. Схема и типы даны в шаблоне:

```go
const Schema = `CREATE TABLE accounts (
	id      INTEGER PRIMARY KEY,
	owner   TEXT NOT NULL,
	email   TEXT,
	balance INTEGER NOT NULL CHECK (balance >= 0)
)`

type Account struct {
	ID      int64
	Owner   string
	Email   *string // NULL → nil
	Balance int64
}

var (
	ErrNotFound          = errors.New("account not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidAmount     = errors.New("invalid amount")
)

func NewRepo(db *sql.DB) *Repo
func (r *Repo) Create(ctx context.Context, owner string, email *string, balance int64) (int64, error)
func (r *Repo) Get(ctx context.Context, id int64) (Account, error)
func (r *Repo) List(ctx context.Context, minBalance int64) ([]Account, error)
func (r *Repo) Transfer(ctx context.Context, from, to, amount int64) error
```

Таблицу создают тесты; драйвер (`modernc.org/sqlite`) импортируют тоже тесты — `task.go` импортирует только стандартную библиотеку. Все методы передают `ctx` в запросы: при отменённом контексте возвращается ошибка, для которой `errors.Is(err, context.Canceled)`. Ошибки базы возвращаются обёрнутыми или как есть. Значения — только через плейсхолдеры `?`.

- `Create` добавляет строку и возвращает её `id` (`LastInsertId`). `email == nil` записывается как `NULL`, указатель на пустую строку — как `''`. При `balance < 0` — `ErrInvalidAmount`, в таблицу ничего не пишется.
- `Get` возвращает счёт; `Email` — `nil` для `NULL`. Если строки нет — ошибка с `errors.Is(err, ErrNotFound)`. При любой ошибке — нулевой `Account{}`.
- `List` возвращает счета с `balance >= minBalance`, отсортированные по возрастанию `id`; если таких нет — срез длины 0 (`nil` допустим).
- `Transfer` переводит `amount` со счёта `from` на счёт `to`:
  - `amount <= 0` или `from == to` — `ErrInvalidAmount` без обращения к базе;
  - проверки и изменения выполняются в одной транзакции; ошибки в порядке приоритета: нет счёта `from` или `to` — `ErrNotFound` (даже если денег и так не хватает); на `from` меньше `amount` — `ErrInsufficientFunds`;
  - при любой ошибке балансы не меняются, транзакция откатывается и соединение возвращается в пул;
  - одновременные переводы корректны (тесты вызывают их из многих горутин с пулом из одного соединения): каждый перевод либо проходит целиком, либо возвращает `ErrInsufficientFunds` — но не ошибку ограничения `CHECK`, и баланс не уходит в минус.
- Ни один метод не оставляет занятым соединение пула: тесты проверяют это с `db.SetMaxOpenConns(1)`.
