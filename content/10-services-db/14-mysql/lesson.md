---
title: MySQL
summary: go-sql-driver/mysql через database/sql — mysql.Config и parseTime, utf8mb4 против utf8mb3, LastInsertId и ON DUPLICATE KEY UPDATE, Repeatable Read и gap locks, дедлок 1213 и повтор транзакции — на настоящем MySQL 8.4.
difficulty: 4
minutes: 45
prerequisites:
  - 08-web/10-database-sql
  - 10-services-db/06-transactions
  - 10-services-db/07-locking
  - 10-services-db/12-postgres-mvcc-isolation
docs:
  - title: "go-sql-driver/mysql"
    url: https://pkg.go.dev/github.com/go-sql-driver/mysql
  - title: "MySQL 8.4: Locks Set by Different SQL Statements in InnoDB"
    url: https://dev.mysql.com/doc/refman/8.4/en/innodb-locks-set.html
  - title: "MySQL 8.4: Deadlocks in InnoDB"
    url: https://dev.mysql.com/doc/refman/8.4/en/innodb-deadlocks.html
runner:
  labs: [mysql]
  race: true
---

С MySQL Go работает через тот же `database/sql`, что и с SQLite: драйвер `github.com/go-sql-driver/mysql` регистрируется под именем `"mysql"`, а код поверх `*sql.DB` почти не меняется. Плейсхолдеры — `?`, как в SQLite. По умолчанию драйвер отправляет запрос с аргументами как серверный prepared statement: подготовка, выполнение и закрытие — три команды серверу, две из них ждут ответа. Параметр `InterpolateParams` подставляет значения на стороне клиента и обходится одним обменом. Это безопасно, пока кодировка соединения — `utf8mb4` или другая, где экранирование однозначно. Для многобайтовых кодировок вроде `gbk` и `sjis`, где байт `\` может оказаться внутри символа, драйвер отказывается от такой конфигурации: `ParseDSN` и `NewConnector` вернут ошибку.

Отличия начинаются с DSN. Строка вида `user:pass@tcp(host:3306)/db?parseTime=true` — формат драйвера, а не URL, и собирать её через `fmt.Sprintf` нельзя: пароль с `@` или `/` её сломает. Правильный путь — `mysql.Config`: `mysql.NewConfig()` или `mysql.ParseDSN(dsn)`, правка полей, затем `mysql.NewConnector(cfg)` и `sql.OpenDB(connector)` (или `cfg.FormatDSN()`, если нужна строка). Значения по умолчанию в v1.10 стоит знать наизусть:

| Поле | По умолчанию | Что ставить |
| --- | --- | --- |
| `ParseTime` | `false` — `DATETIME` приходит как `[]byte`, `Scan` в `time.Time` падает | `true` |
| `Loc` | `time.UTC` — в этом поясе драйвер пишет `time.Time` и разбирает `DATETIME` | UTC, если нет веской причины |
| `Collation` | пусто — драйвер просит у сервера `utf8mb4_general_ci` (кодировка соединения — `utf8mb4`) | как у таблиц: в MySQL 8 по умолчанию `utf8mb4_0900_ai_ci` |
| `Timeout` / `ReadTimeout` / `WriteTimeout` | 0 — ждать без ограничений | подключение — секунды; ввод-вывод — с запасом над самым долгим запросом |
| `InterpolateParams` | `false` | `true` ради скорости, если понимаете оговорку выше |
| `TLSConfig` (`tls=` в DSN) | пусто — соединение без шифрования | в продакшне `"true"` (проверка сертификата по системным CA) или имя конфигурации из `mysql.RegisterTLSConfig` с CA сервера; `skip-verify` и `preferred` от перехвата не защищают. В профиле labs TLS нет: база в docker-сети на той же машине |
| `Params` | — | системные переменные сессии: драйвер выполняет `SET` при каждом подключении |

Таймаут запроса задаёт `context`, как и везде в `database/sql`. Но MySQL-драйвер при отмене контекста не посылает `KILL QUERY`, а закрывает соединение: клиент перестаёт ждать, а сервер дорабатывает запрос, пока не заметит обрыв. Для тяжёлых запросов добавляйте серверный лимит — `SET SESSION max_execution_time` (только для `SELECT`) или подсказку `/*+ MAX_EXECUTION_TIME(1000) */`.

Остальные отличия от PostgreSQL видны в примере, он идёт на MySQL 8.4 профиля labs:

- **`RETURNING` нет.** Id новой строки — `res.LastInsertId()`. У многострочного `INSERT` это id **первой** вставленной строки, а не последней.
- **`ON CONFLICT` нет**, есть `INSERT … ON DUPLICATE KEY UPDATE`. С 8.0.19 новые значения берутся через псевдоним строки `VALUES (…) AS new … UPDATE qty = new.qty`, а функция `VALUES(col)` устарела и даёт предупреждение. `RowsAffected` здесь особенный: 1 — вставка, 2 — обновление, 0 — строка уже была такой же. С `ClientFoundRows = true` последний случай вернёт 1.
- **`utf8` — это `utf8mb3`**, до трёх байт на символ. ☕ (U+2615) в такой столбец помещается, а 🍕 (U+1F355) — нет. В строгом режиме (`STRICT_TRANS_TABLES` включён по умолчанию) это ошибка: 3988 при передаче через параметр, 1366 «Incorrect string value» для литерала. Без строгого режима строка молча обрезалась бы. Новые таблицы — только `utf8mb4`.
- **Время.** `DATETIME` хранит «настенное» время без пояса: драйвер переводит `time.Time` в `Loc` и записывает цифры. 12:00 MSK превращается в 09:00, а при чтении получается тот же момент в UTC. `TIMESTAMP` вдобавок пересчитывается по `time_zone` сессии — держите `Loc` и `time_zone` согласованными или используйте `DATETIME` с UTC.

**Блокировки InnoDB.** Уровень изоляции InnoDB по умолчанию — **Repeatable Read** (в PostgreSQL — Read Committed), и устроен он иначе. Обычный `SELECT` читает снимок, как в PostgreSQL. А блокирующие чтения (`SELECT … FOR UPDATE`, `FOR SHARE`, а также `UPDATE` и `DELETE` с условием) блокируют не только найденные строки, но и **промежутки** между ключами индекса. Это next-key lock: запись плюс промежуток перед ней. Так InnoDB не пускает фантомов: пока транзакция A держит `id BETWEEN 15 AND 25 FOR UPDATE` в таблице с ключами 10, 20, 30, вставка 12, 17, 25 или 27 ждёт, а 5 и 35 проходят. В примере это видно по `performance_schema.data_locks`: у A блокировки `X 20` (запись 20 и промежуток до неё) и `X,GAP 30` (промежуток перед 30), а вставка B ждёт с режимом `X,GAP,INSERT_INTENTION`. В Read Committed промежутки не блокируются: вставка проходит сразу, зато фантомы возможны.

Gap locks — частая причина неожиданных ожиданий и дедлоков. Например, два `SELECT … FOR UPDATE` по несуществующему ключу совместимы: блокировки промежутков друг другу не мешают. Но затем обе транзакции пытаются вставить этот ключ — и каждая ждёт другую. Отсюда практические правила. Блокирующие чтения по диапазону делайте по индексу: без индекса InnoDB блокирует всё, что просканировал, то есть всю таблицу. Транзакции держите короткими. Если фантомы для вашей логики не страшны, подумайте о Read Committed.

**Дедлок** InnoDB находит сразу (`innodb_deadlock_detect` включён) и откатывает одну из транзакций — ту, что изменила меньше строк. Она получает ошибку **1213** (SQLSTATE `40001`), и вся её работа уже отменена: правильная реакция — повторить транзакцию **целиком**, с начала. Ошибка **1205** — истёк `innodb_lock_wait_timeout` (50 секунд по умолчанию — для веб-запроса слишком долго, в примере 3). Тут подвох: при `innodb_rollback_on_timeout = OFF` (по умолчанию) откатывается только последний запрос, а транзакция остаётся открытой. Повторяйте и её с начала, не дописывая.

Ошибки сервера драйвер возвращает как `*mysql.MySQLError` с полями `Number` (номер ошибки MySQL) и `SQLState`. Классифицируйте их по `Number` через `errors.As`: 1062 — дубликат ключа, 1213 и 1205 — можно повторить. Метод `Is` у этого типа сравнивает номера, так что работает и `errors.Is(err, &mysql.MySQLError{Number: 1062})`. Ошибки соединения (`mysql.ErrInvalidConn`, `driver.ErrBadConn`) не повторяйте вслепую: `COMMIT` мог дойти до сервера, и повтор запишет данные дважды.

Две транзакции в примере идут на двух соединениях (`db.Conn`), а шаги выполняются по очереди из одной горутины. Запрос, который должен ждать, уходит в горутину, и пример дожидается, пока в `data_locks` появится блокировка в состоянии `WAITING`. Только потом он продолжает. Порядок задают явные шаги, а не `time.Sleep`, поэтому вывод одинаков при каждом запуске.

## Как это в C#

- MySqlConnector ↔ go-sql-driver/mysql, `MySqlDataSource` ↔ `*sql.DB`. Пул и в Go, и в .NET живёт в одном объекте на приложение. `MySqlConnectionStringBuilder` — аналог `mysql.Config`: тоже собирает строку подключения без ручной склейки.
- MySqlConnector сам читает `DATETIME` в `DateTime` и всегда работает в `utf8mb4`. В Go и то и другое — ваша забота: `ParseTime`, `Collation` и таблицы в `utf8mb4`. Зато `RowsAffected` в MySqlConnector по умолчанию считает *найденные* строки (`UseAffectedRows=false`): upsert без изменений вернёт 1, а Go-драйвер — 0.
- `MySqlException.ErrorCode` (`MySqlErrorCode.LockDeadlock`, `DuplicateKeyEntry`) ↔ `(*mysql.MySQLError).Number`. Свойство `IsTransient` в MySqlConnector равно `true` для дедлока и истёкшего ожидания. Повтор в .NET дают Polly или `EnableRetryOnFailure` в EF Core, в Go это цикл из десятка строк вокруг `BeginTx` (он и есть задание).
- `cmd.LastInsertedId` ↔ `res.LastInsertId()`; `TransactionScope` нет и тут — транзакция передаётся явно, `*sql.Tx` живёт на одном соединении.

## Ловушки

### Запрос после 1205 — и `COMMIT` половины

```go
tx, _ := db.BeginTx(ctx, nil)
tx.ExecContext(ctx, `INSERT INTO orders ...`)                  // прошло
_, err := tx.ExecContext(ctx, `UPDATE stock ... WHERE id = ?`) // 1205: ждали блокировку
if err != nil {
	log.Print(err) // УЯЗВИМО: ошибка проглочена, дальше — Commit
}
tx.Commit() // зафиксирует INSERT без UPDATE
```

Истёкшее ожидание откатывает только последний запрос, транзакция жива, и `Commit` зафиксирует её частично. Любая ошибка внутри транзакции — это `return err` и `defer tx.Rollback()`, а повтор — всей транзакции.

### Без `parseTime`

```go
db, _ := sql.Open("mysql", "app:pw@tcp(db:3306)/shop")
var t time.Time
err := db.QueryRow(`SELECT created FROM orders LIMIT 1`).Scan(&t)
// unsupported Scan, storing driver.Value type []uint8 into type *time.Time
```

Без `ParseTime` значения `DATETIME` и `TIMESTAMP` приходят как текст. Включайте его всегда: через `cfg.ParseTime = true` или `?parseTime=true` в DSN.

### Поиск «по несуществующему» под `FOR UPDATE`

```go
// Две транзакции одновременно резервируют слот 17, которого ещё нет.
tx.QueryRowContext(ctx, `SELECT id FROM slots WHERE id = ? FOR UPDATE`, 17).Scan(&id) // ErrNoRows, но gap lock взят
tx.ExecContext(ctx, `INSERT INTO slots VALUES (?)`, 17) // ждёт gap lock другой транзакции → 1213 у одной из них
```

Блокировки промежутков совместимы, поэтому обе транзакции проходят `SELECT`, а затем каждая ждёт другую на `INSERT`. Для «создать, если нет» используйте `INSERT … ON DUPLICATE KEY UPDATE` или `INSERT` с обработкой 1062 — без предварительного блокирующего чтения.

## Задание

Напишите три кусочка инфраструктуры MySQL-хранилища — их можно проверить и без настоящей базы. Типы и ошибки даны в шаблоне:

```go
type Settings struct {
	Host           string
	Port           int // 0 — 3306
	User           string
	Password       string
	Database       string
	Location       *time.Location // nil — time.UTC
	ConnectTimeout time.Duration  // 0 — 5 с
	IOTimeout      time.Duration  // 0 — 30 с
}

var ErrInvalidSettings, ErrDuplicate, ErrRetryable error

func Config(s Settings) (*mysql.Config, error)
func Classify(err error) error

type Retry struct {
	Attempts int
	Wait     func(ctx context.Context, attempt int) error
}

func (r Retry) InTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) error
```

**`Config`** строит конфигурацию на основе `mysql.NewConfig()` — поля, не названные ниже, сохраняют значения по умолчанию:

- пустые `Host`, `User` или `Database`, `Port` вне `0..65535`, отрицательный таймаут — `nil` и ошибка, для которой `errors.Is(err, ErrInvalidSettings)`;
- `Net = "tcp"`, `Addr` — хост и порт (порт 0 значит 3306) в формате `host:port`, IPv6-адрес — в квадратных скобках: `[::1]:3306` (в `Host` адрес приходит без скобок: `::1`);
- `User`, `Passwd`, `DBName` — из настроек; пароль может содержать любые символы, и `mysql.ParseDSN(cfg.FormatDSN())` должен вернуть его без изменений;
- `ParseTime = true`, `Loc = Location` (или `time.UTC`), `Collation = "utf8mb4_0900_ai_ci"`;
- `Timeout = ConnectTimeout`, `ReadTimeout` и `WriteTimeout` = `IOTimeout`, нулевые — значения по умолчанию из комментариев.

**`Classify`** находит в цепочке `err` ошибку `*mysql.MySQLError` (`errors.As`). Номер 1062 — вернуть ошибку, для которой `errors.Is(…, ErrDuplicate)`; 1213 или 1205 — `errors.Is(…, ErrRetryable)`. В обоих случаях исходная ошибка остаётся в цепочке: `errors.As` по-прежнему находит `*mysql.MySQLError` с тем же номером. Любая другая ошибка, включая `nil`, возвращается без изменений — тем же значением.

**`InTx`** выполняет `fn` в транзакции и повторяет её целиком:

- попытка — `db.BeginTx(ctx, nil)`, затем `fn(ctx, tx)`, затем `Commit`, если `fn` вернула `nil`. Ошибка `BeginTx` (например, отменённый контекст) возвращается сразу, без повтора и без вызова `fn`;
- если `fn` вернула ошибку или запаниковала — `Rollback`; при неудачном `Commit` транзакция уже завершена, откатывать её не нужно. В любом случае соединение возвращается в пул; паника продолжается дальше;
- ошибка попытки (из `fn` или из `Commit`) проходит через `Classify`. Если она `ErrRetryable` и попытки не кончились — повтор. Всего попыток `Attempts`, при `Attempts < 1` — одна;
- перед повторной попыткой номер `n` (2, 3, …) вызывается `Wait(ctx, n)`, если он не `nil`. Ошибка `Wait` возвращается как есть, повтора нет; после последней попытки `Wait` не вызывается;
- результат — `nil` при успехе, иначе ошибка последней попытки после `Classify`.

Тесты идут на SQLite с одним соединением в пуле: незавершённая транзакция заблокирует следующий запрос. Ошибки MySQL тесты подставляют сами — `&mysql.MySQLError{Number: 1213}`. С профилем labs скрытый тест вдобавок проверяет всё на настоящем MySQL: эмодзи и время после чтения, классификацию дубликата и настоящий дедлок двух транзакций, после которого обе должны пройти.
