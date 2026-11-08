---
title: Transactional outbox
summary: Двойная запись в БД и брокер и почему она теряет события; событие в таблице outbox в той же транзакции, relay с FOR UPDATE SKIP LOCKED, at-least-once и идемпотентный потребитель, порядок событий и очистка.
difficulty: 4
minutes: 35
prerequisites:
  - 10-services-db/06-transactions
  - 10-services-db/07-locking
  - 10-services-db/09-n-plus-one-batches
docs:
  - title: "microservices.io: Transactional outbox"
    url: https://microservices.io/patterns/data/transactional-outbox.html
  - title: "PostgreSQL: SELECT — FOR UPDATE SKIP LOCKED"
    url: https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE
  - title: "Пакет database/sql"
    url: https://pkg.go.dev/database/sql
---

Сервис создал заказ и должен сообщить об этом другим — отправить событие `order.created` в Kafka. Это **двойная запись**: две системы, общей транзакции у которых нет. Закоммитили заказ, а брокер недоступен (или процесс упал между двумя строчками) — событие потеряно, склад о заказе не узнает никогда. Отправили событие до коммита, а коммит упал — по системе гуляет событие о заказе, которого нет. Повторы не спасают: процесс может умереть в любой точке. Распределённые транзакции (2PC, XA) брокеры вроде Kafka не поддерживают, да и в Go их нет — как нет `TransactionScope`.

**Outbox.** Событие пишется не в брокер, а в таблицу `outbox` той же базы и **в той же транзакции**, что и заказ: либо есть и заказ, и событие, либо ничего. Публикует отдельный процесс — **relay**: он периодически читает неопубликованные события по порядку, отправляет в брокер и помечает (`published_at`) или удаляет. Брокер недоступен — событие просто подождёт следующего прохода. Вместо опроса можно читать журнал транзакций (CDC: Debezium с outbox event router) — тогда relay писать не надо, но появляется ещё одна система в эксплуатации.

**Relay на PostgreSQL и MySQL 8.** Чтобы несколько экземпляров relay не публиковали одно и то же, пачку берут с блокировкой строк и пропуском уже заблокированных:

```go
tx, err := db.BeginTx(ctx, nil)
// ...
defer tx.Rollback()
rows, err := tx.QueryContext(ctx, `SELECT id, topic, key, payload FROM outbox
	WHERE published_at IS NULL ORDER BY id LIMIT 100
	FOR UPDATE SKIP LOCKED`)
// ... дочитать пачку, опубликовать ...
// PostgreSQL через pgx: ids — []int64 одним параметром; в MySQL — IN (?, ?, ...)
_, err = tx.ExecContext(ctx, `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, ids)
return tx.Commit()
```

Второй relay в это время получит следующие строки, а не будет ждать (проверено на PostgreSQL 18 и MySQL 8.4: две транзакции с `LIMIT 2` получают `[1 2]` и `[3]`). Цена — транзакция и соединение заняты, пока идёт публикация, поэтому пачки небольшие, а у публикации — таймаут через `ctx`. В SQLite `FOR UPDATE` нет, а писатель и так один — как в примере, достаточно одного relay. Будить relay сразу после коммита можно через `LISTEN/NOTIFY` (урок 11), а опрос по таймеру оставить страховкой.

**At-least-once.** Relay может упасть после публикации, но до отметки, — и опубликует событие повторно. Exactly-once между базой и брокером не бывает, поэтому **потребитель идемпотентен**: у события есть уникальный id (здесь — id строки outbox; в реальной системе — UUID в заголовке сообщения), и потребитель в одной транзакции записывает id в таблицу обработанных (`INSERT ... ON CONFLICT DO NOTHING`) и применяет эффект; если id уже был — пропускает. Это зеркальный приём — **inbox**.

**Порядок.** Один relay, публикующий по возрастанию `id` и останавливающийся на первой ошибке, сохраняет порядок событий. Несколько relay со `SKIP LOCKED` публикуют пачки параллельно, и события одного заказа могут уйти не по порядку. Обычно нужен порядок не глобально, а в пределах сущности: в Kafka его даёт ключ сообщения (одна партиция на ключ), а relay либо работает в одном экземпляре (лидер через advisory lock, урок 11), либо делит события по ключу (`hash(key) % N`). Потребитель всё равно должен переживать устаревшие события — например, сравнивать версию сущности.

**Очистка.** Таблица outbox растёт с каждым событием. Опубликованные удаляют фоновой задачей (`DELETE ... WHERE published_at < now() - interval '7 days'`, пачками, чтобы не держать долгие блокировки) или сразу удаляют вместо отметки. Частичный индекс `WHERE published_at IS NULL` держит выборку relay быстрой при любом размере таблицы.

## Как это в C#

- **`TransactionScope` над базой и MSMQ** через MSDTC — старое решение двойной записи; в .NET Core распределённые транзакции вернулись только в .NET 7 и только на Windows, а Kafka и RabbitMQ в них не участвуют. В Go `TransactionScope` нет вовсе: транзакция — явный `*sql.Tx`, и outbox — стандартный ответ в обоих мирах.
- **MassTransit Transactional Outbox, NServiceBus Outbox, CAP** делают outbox «из коробки»: `SaveChanges` пишет сообщения в таблицу вместе с сущностями, фоновый `IHostedService` их отправляет, а inbox дедуплицирует входящие. В Go это сотня строк своего кода: `INSERT` в той же `tx`, relay-горутина с тикером и таблица обработанных у потребителя.
- **`BackgroundService` с `PeriodicTimer`** → горутина с `time.Ticker` и `ctx.Done()` для остановки (graceful shutdown — урок 3).
- **EF Core `SaveChanges`** сам открывает транзакцию на все изменения контекста; в Go заказ и событие окажутся в одной транзакции, только если оба `INSERT` идут через один `tx`.

## Ловушки

### Событие мимо транзакции

```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()
tx.ExecContext(ctx, `INSERT INTO orders ...`)
db.ExecContext(ctx, `INSERT INTO outbox ...`) // через db, а не tx
return tx.Commit()
```

Событие записано в другом соединении и закоммичено сразу — откат заказа его не отменит. А с `SetMaxOpenConns(1)` такой код просто повиснет: единственное соединение занято транзакцией. Внутри транзакции все запросы — через `tx`; функции хранилища принимают `DBTX`, а не `*sql.DB`.

### Публикация с открытой выборкой

```go
rows, _ := db.QueryContext(ctx, `SELECT ... FROM outbox WHERE published_at IS NULL`)
defer rows.Close()
for rows.Next() {
	rows.Scan(&e.ID, &e.Topic, &e.Key, &e.Payload)
	pub.Publish(ctx, e)                         // секунды на медленном брокере
	db.ExecContext(ctx, `UPDATE outbox ...`)    // второе соединение
}
```

Пока цикл идёт, `rows` держит соединение, а брокер может отвечать секундами — пул тает. Отметка в цикле берёт второе соединение, а при исчерпанном пуле — ждёт вечно. Дочитайте пачку в срез, закройте `rows`, потом публикуйте.

### Пропуск упавшего события

```go
for _, e := range batch {
	if err := pub.Publish(ctx, e); err != nil {
		log.Printf("событие %d: %v", e.ID, err)
		continue // следующие уйдут раньше упавшего
	}
	markPublished(e.ID)
}
```

Событие `order.cancelled` уйдёт раньше `order.created`, и потребитель отменит заказ, которого ещё не видел. Если порядок важен — на первой ошибке остановитесь и верните её; упавшее событие повторится в следующем проходе.

## Задание

Реализуйте создание заказа с событием в outbox и relay. Схема `Schema` (таблицы `orders(id, customer, total)` и `outbox(id, topic, key, payload, published_at)`) и типы даны в шаблоне; таблицы создают тесты, драйвер `modernc.org/sqlite` импортируют тоже тесты.

```go
const TopicOrderCreated = "order.created"

var ErrInvalidOrder = errors.New("outbox: неверный заказ")

type Event struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

type OrderCreated struct {
	OrderID  int64  `json:"order_id"`
	Customer string `json:"customer"`
	Total    int64  `json:"total"`
}

type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

func CreateOrder(ctx context.Context, db *sql.DB, customer string, total int64) (int64, error)

type Relay struct {
	DB        *sql.DB
	Pub       Publisher
	BatchSize int // <= 0 — 100
}

func (r *Relay) RunOnce(ctx context.Context) (int, error)
```

Тесты используют пул из одного соединения и контекст с таймаутом; `ctx` передаётся во все запросы (с отменённым контекстом — ошибка, для которой `errors.Is(err, context.Canceled)`). Значения — только через плейсхолдеры.

`CreateOrder(ctx, db, customer, total)`:

- если `customer == ""` или `total <= 0` — `0` и `ErrInvalidOrder`, в базу ничего не пишется;
- иначе в **одной транзакции** вставляет строку в `orders` (`customer` — как передан) и событие в `outbox`: `topic` — `TopicOrderCreated`, `key` — id заказа десятичной строкой (`"3"`), `payload` — JSON `OrderCreated{OrderID: id, Customer: customer, Total: total}`, `published_at` — `NULL`; возвращает id заказа;
- если любая из вставок или коммит не удались — возвращает `0` и ошибку, и в базе не остаётся ни заказа, ни события; транзакция завершена, соединение вернулось в пул.

`RunOnce(ctx)` — один проход relay:

- выбирает до `BatchSize` (при `BatchSize <= 0` — до 100) событий с `published_at IS NULL` по возрастанию `id` и публикует их по одному в этом порядке, передавая в `Publish` все поля (`ID`, `Topic`, `Key`, `Payload`) из строки outbox;
- после успешной публикации событие помечается: `published_at` получает любое не-`NULL` значение (например, `CURRENT_TIMESTAMP`). Уже помеченные события не публикуются и не меняются;
- если `Publish` вернул ошибку — проход останавливается: это и следующие события остаются непомеченными и в этом вызове больше не публикуются; `RunOnce` возвращает число опубликованных и помеченных до сбоя и ошибку, для которой `errors.Is(err, <ошибка Publish>)`;
- если не удалось пометить событие — проход тоже останавливается с ошибкой; событие остаётся непомеченным и будет опубликовано повторно в следующем проходе (at-least-once);
- `Publish` вызывается, когда выборка уже прочитана и `Rows` закрыты: публикатор в тестах сам обращается к той же базе через тот же пул из одного соединения;
- без ошибок возвращает число опубликованных событий (`0, nil`, если публиковать нечего) — тогда `Publish` не вызывается вовсе.
