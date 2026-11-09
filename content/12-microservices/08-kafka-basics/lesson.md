---
title: "Kafka: топики, партиции, продюсер"
summary: Модель Kafka — лог из партиций и офсетов, ключ выбирает партицию и задаёт порядок; franz-go — kgo.NewClient, ProduceSync и Produce с колбэком, kadm.CreateTopic, JSON и заголовки; kfake как брокер в процессе.
difficulty: 3
minutes: 35
prerequisites:
  - 12-microservices/01-architecture
  - 10-services-db/10-outbox
  - 11-grpc/01-protobuf
docs:
  - title: "franz-go: пакет kgo"
    url: https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo
  - title: "franz-go: kfake — Kafka в процессе"
    url: https://pkg.go.dev/github.com/twmb/franz-go/pkg/kfake
  - title: "Apache Kafka: Introduction"
    url: https://kafka.apache.org/documentation/#introduction
runner:
  race: true
---

Kafka — не очередь, а **распределённый лог**. Топик `orders.events` состоит из **партиций**; партиция — файл, куда записи только дописываются в конец, и каждая получает **офсет** — номер 0, 1, 2… в своей партиции. Запись не удаляется после чтения: её хранят по **retention** (время `retention.ms` или размер; либо compaction — последняя запись на ключ), а каждый читатель сам помнит, докуда дочитал. Поэтому один топик читают независимо «Уведомления», аналитика и новый сервис, который захотел перечитать историю с начала.

**Ключ → партиция → порядок.** Продюсер выбирает партицию по ключу записи: `murmur2(key) % число_партиций` — та же функция, что у Java-клиента и Confluent.Kafka, так что разные языки кладут один ключ в одну партицию. Порядок Kafka гарантирует **только внутри партиции**: события заказа `A-1` с ключом `A-1` придут потребителю ровно в том порядке, в каком записаны, а относительно событий `B-2` из другой партиции — в любом. Отсюда правило: ключ — id сущности, порядок изменений которой важен (заказ, счёт), а не тип события и не случайный UUID. Записи без ключа клиент раскладывает сам (липкий партиционер: пачка в одну партицию, потом в другую) — порядка между ними нет.

**Партиции — единица параллелизма.** В группе потребителей каждую партицию читает один участник (урок 9), поэтому число партиций — потолок параллельности чтения. Увеличить его можно (`adm.CreatePartitions`), уменьшить — нет, и после увеличения `hash % N` даёт другие партиции: новые события заказа уйдут не туда, где лежат старые, и порядок на стыке сломается. Число партиций выбирают с запасом заранее.

**Репликация.** У каждой партиции есть лидер и реплики на других брокерах (фактор репликации, в проде — 3). Пишут и читают через лидера, реплики догоняют; те, что успевают, — ISR (in-sync replicas). Падает брокер — лидером становится реплика из ISR. Сколько реплик должны подтвердить запись, задаёт продюсер (`acks`) и топик (`min.insync.replicas`) — это урок 10. С Kafka 4.0 кластер работает без ZooKeeper (KRaft): метаданные хранят сами брокеры-контроллеры.

**franz-go.** Клиент `kgo.Client` — один на процесс: он и продюсер, и потребитель, держит соединения со всеми брокерами и сам узнаёт, где лидеры партиций. Создаётся функциональными опциями (`kgo.SeedBrokers`, `kgo.ConsumeTopics`, …), конфигурационного объекта нет. Запись — `*kgo.Record` с полями `Topic`, `Key`, `Value`, `Headers`; после отправки клиент заполняет в ней `Partition` и `Offset`. Отправлять можно двумя способами:

- `cl.ProduceSync(ctx, recs...)` — ждёт подтверждения всех записей и возвращает `ProduceResults`; ошибка — `results.FirstErr()`. Просто, но поток ждёт сетевой круг.
- `cl.Produce(ctx, rec, func(r *kgo.Record, err error))` — кладёт запись в буфер и сразу возвращается; клиент сам собирает пачки по партициям, колбэк вызывается по подтверждению из горутины клиента. Перед остановкой — `cl.Flush(ctx)`, иначе буфер пропадёт вместе с процессом.

Топики создаёт админ-клиент `kadm.NewClient(cl)`: `CreateTopic(ctx, partitions, replicationFactor, configs, name)`. В профиле labs автосоздание топиков выключено — так и в проде: опечатка в имени топика не должна молча создавать новый.

**Сериализация.** Kafka хранит байты; формат — договорённость. JSON читается глазами и проще в отладке; protobuf из модуля 11 компактнее и имеет контракт с правилами совместимости (`buf breaking`) — для событий между командами это весомый довод. Тип события и формат кладут в **заголовки** (`event-type`, `content-type`), туда же — id трассы (урок 3) и ключ идемпотентности (урок 10). Schema Registry (Confluent, Apicurio) хранит схемы централизованно — в курсе его нет, контракт живёт в buf-модуле.

**kfake.** Пакет `github.com/twmb/franz-go/pkg/kfake` поднимает кластер Kafka прямо в процессе на `127.0.0.1` — для песочницы и тестов без Docker. Протокол настоящий, поэтому тот же `kgo` работает с ним без изменений: на брокере Kafka 4.3 из профиля labs (`localhost:59092`) ключи примера попадают в те же партиции с теми же офсетами. Чтобы запустить код на настоящем брокере, достаточно заменить `cluster.ListenAddrs()` адресом из `KAFKA_BROKERS` и добавить к именам топиков префикс `KAFKA_TOPIC_PREFIX`.

## Как это в C#

- **Confluent.Kafka** — обёртка над нативной librdkafka: `ProducerBuilder<TKey, TValue>` с `ProducerConfig`, отдельно `ConsumerBuilder` и `AdminClientBuilder`. В franz-go всё — один `kgo.Client` на чистом Go, без cgo и нативных библиотек в образе.
- **`ProduceAsync` → `ProduceSync`**, **`Produce(msg, handler)` → `Produce(ctx, rec, promise)`**: в C# асинхронность — `Task`, в Go синхронный вызов блокирует горутину, а асинхронный — колбэк. `producer.Flush(timeout)` ↔ `cl.Flush(ctx)`.
- **Сериализаторы** (`ISerializer<T>`, `SetValueSerializer`) превращают объект в байты внутри клиента; в kgo `Value` — уже `[]byte`, `json.Marshal`/`proto.Marshal` вызывается явно перед отправкой.
- **Партиционер** по умолчанию в librdkafka — `consistent_random` (CRC32), а в Java-клиенте и franz-go — `murmur2`. Один ключ из .NET и из Go может попасть в разные партиции! В смешанной системе в Confluent.Kafka ставят `Partitioner = Partitioner.Murmur2Random`.

## Ловушки

### Порядок между партициями

```go
cl.ProduceSync(ctx, &kgo.Record{Topic: t, Key: []byte("cart"), Value: created})
cl.ProduceSync(ctx, &kgo.Record{Topic: t, Key: []byte("payment"), Value: paid})
```

Ключ — тип события, а не заказ: «создан» и «оплачен» одного заказа лежат в разных партициях, и потребитель может увидеть оплату раньше создания. Порядок есть только внутри партиции — ключом делают id сущности.

### Produce без Flush

```go
for _, e := range events {
	cl.Produce(ctx, record(e), nil) // nil — ошибки никто не увидит
}
cl.Close() // буфер не отправлен
```

`Produce` только кладёт запись в буфер. Без `Flush` перед `Close` (или при выходе из `main`) часть записей не уйдёт, а с колбэком `nil` о сбоях отправки никто не узнает. Либо `ProduceSync`, либо колбэк с обработкой ошибки и `Flush` при остановке.

### Результаты ProduceSync — в порядке подтверждений

```go
res := cl.ProduceSync(ctx, recs...)
for i, r := range res {
	fmt.Println(events[i].OrderID, r.Record.Offset) // события и результаты перепутаны
}
```

`ProduceResults` собираются по мере подтверждений, а партиции подтверждаются в разном порядке — `res[i]` не обязан соответствовать `recs[i]`. Партиция и офсет записываются в сами `*kgo.Record`: читайте `recs[i].Partition` и `recs[i].Offset`, а ошибку — `res.FirstErr()`.

## Задание

Напишите продюсер событий заказа и функцию, которая восстанавливает историю каждого заказа из прочитанных записей. Типы и константы даны в шаблоне:

```go
const (
	HeaderEventType   = "event-type"
	HeaderContentType = "content-type"
	ContentTypeJSON   = "application/json"
)

var (
	ErrNoOrderID = errors.New("events: пустой order id")
	ErrSplitKey  = errors.New("events: ключ в нескольких партициях")
)

type OrderEvent struct {
	OrderID string `json:"order_id"`
	Type    string `json:"type"`
	Amount  int64  `json:"amount"`
}

type Position struct {
	Partition int32
	Offset    int64
}

func NewRecord(topic string, e OrderEvent) (*kgo.Record, error)
func Publish(ctx context.Context, cl *kgo.Client, topic string, events []OrderEvent) ([]Position, error)
func Histories(records []*kgo.Record) (map[string][]string, error)
```

`NewRecord(topic, e)`:

- если `e.OrderID == ""` — `nil` и ошибка, для которой `errors.Is(err, ErrNoOrderID)`;
- иначе запись с `Topic: topic`, `Key` — байты `e.OrderID`, `Value` — `json.Marshal(e)` (поля `order_id`, `type`, `amount`), `Headers` — ровно два заголовка в таком порядке: `event-type` со значением `e.Type` и `content-type` со значением `application/json`. Партицию не задавайте — её выбирает партиционер клиента по ключу.

`Publish(ctx, cl, topic, events)`:

- сначала проверяет все события: если хотя бы у одного пустой `OrderID` — возвращает `nil` и ошибку с `ErrNoOrderID`, и **ничего не отправляет**;
- пустой `events` — пустой срез и `nil`, без обращения к брокеру;
- иначе отправляет все записи синхронно и возвращает позиции — `Position` для `events[i]` стоит на месте `i` (партиция и офсет, которые брокер присвоил этой записи);
- если хотя бы одна запись не отправлена — `nil` и ошибка этой отправки (`errors.Is` находит её).

Тесты создают клиент на `kfake` и топик с несколькими партициями сами; клиент — с настройками по умолчанию.

`Histories(records)` — записи приходят так, как их отдал потребитель: из разных партиций вперемешку.

- Возвращает для каждого `OrderID` (ключа записи) список типов событий (`type` из JSON в `Value`) в порядке офсетов. Записи одной партиции могут прийти не по порядку — упорядочьте их по `Offset`.
- Если записи одного ключа лежат в разных партициях, порядок между ними не определён — `nil` и ошибка с `ErrSplitKey`.
- Запись с пустым ключом — `nil` и ошибка с `ErrNoOrderID`; `Value`, который не разбирается как JSON, — `nil` и ошибка разбора.
- Пустой вход — пустая (не `nil`) карта и `nil`.
- Если во входе несколько проблем сразу, можно вернуть любую из перечисленных ошибок (например, первую встреченную).
