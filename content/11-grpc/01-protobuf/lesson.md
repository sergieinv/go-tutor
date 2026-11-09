---
title: Protocol Buffers и proto3
summary: "Контракт в .proto как источник истины: скалярные типы и их Go-типы, enum, oneof, map, optional и presence, well-known types, бинарное кодирование (varint и теги через protowire), protojson и proto.Equal."
difficulty: 3
minutes: 35
prerequisites:
  - 03-types/01-structs
  - 06-stdlib/05-json
  - 06-stdlib/11-hashing-and-encoding
docs:
  - title: "Language Guide (proto3)"
    url: https://protobuf.dev/programming-guides/proto3/
  - title: "Encoding — бинарный формат"
    url: https://protobuf.dev/programming-guides/encoding/
  - title: "Пакет google.golang.org/protobuf/proto"
    url: https://pkg.go.dev/google.golang.org/protobuf/proto
---

Protocol Buffers — язык описания данных и компактный бинарный формат. Контракт пишут один раз в файле `.proto`, а код для Go, C#, Java, TypeScript генерируют из него. Сервис на Go и клиент на C# не договариваются о JSON-полях на словах: оба собраны из одного файла, и расхождение видно при генерации, а не в проде. На protobuf держится весь модуль: gRPC передаёт сообщения protobuf, buf проверяет контракт, protovalidate берёт правила прямо из `.proto`.

В песочнице `.proto` лежит рядом с `main.go`, и перед каждым запуском выполняется `buf generate`: из `orders.proto` получается `orders.pb.go` с типами `Order`, `Order_Item`, `OrderStatus`. Сгенерированный код открыт только для чтения, а контракт можно править: после правки код сгенерируется заново. `option go_package = "lesson/proto;main"` говорит генератору, в какой Go-пакет класть код (в песочнице всё лежит в `main`). Пакет proto `shop.v1` — пространство имён контракта, у типов полные имена вида `shop.v1.Order`.

Сообщение — набор полей, у каждого тип, имя и **номер**. По проводу идёт номер, а не имя, поэтому номер нельзя менять, когда контракт уже используется (подробно — в следующем уроке). Соответствие типов:

| proto3 | Go | Кодирование |
| --- | --- | --- |
| `int32`, `int64` | `int32`, `int64` | varint; отрицательное — всегда 10 байт |
| `sint32`, `sint64` | `int32`, `int64` | varint после zigzag: −1 → 1, 1 → 2 |
| `uint32`, `uint64` | `uint32`, `uint64` | varint |
| `fixed32`, `fixed64`, `sfixed*` | `uint32`, `uint64`, `int32`, `int64` | ровно 4 или 8 байт |
| `float`, `double` | `float32`, `float64` | 4 или 8 байт |
| `bool` | `bool` | varint |
| `string`, `bytes` | `string`, `[]byte` | длина + байты; `string` обязан быть UTF-8 |
| `enum E` | `type E int32` | varint |
| `message M` | `*M` | длина + байты вложенного сообщения |
| `repeated T` | `[]T` (`[]*M` для сообщений) | числа — упаковано одним полем |
| `map<K, V>` | `map[K]V` | повторяющиеся пары ключ–значение |

Остальные конструкции proto3:

- **enum** — первое значение обязано быть нулём, и это значение по умолчанию. Его называют `*_UNSPECIFIED`: тогда «не задано» не путается с осмысленным статусом. Значения enum живут в области видимости пакета, поэтому их принято начинать с имени типа: `ORDER_STATUS_PAID`.
- **oneof** — не больше одного поля из группы. В Go это поле-интерфейс (`Payment isOrder_Payment`) и по типу-обёртке на вариант (`*Order_Card`, `*Order_CashOnDelivery`); какой выбран, узнают переключателем по типу.
- **Presence.** У обычного скалярного поля proto3 нет «не задано»: ноль и пустая строка на провод не попадают, и получатель видит те же нули. Если разница важна (скидка 0% против «скидки нет»), поле объявляют `optional`: в Go оно становится указателем, `nil` — не задано. У полей-сообщений и полей `oneof` presence есть всегда.
- **Well-known types** — готовые сообщения из `google/protobuf/*.proto` с пакетами-помощниками в Go: `Timestamp` (`timestamppb.New(t)`, `ts.AsTime()`), `Duration` (`durationpb`), `FieldMask` — список путей полей для частичного обновления (`update_mask` в `UpdateOrderRequest`: «поменять только `status` и `comment`»), обёртки `Int64Value`, `StringValue` и др. (`wrapperspb`) — способ получить presence до появления `optional`; в новых контрактах для скаляров берут `optional`, а обёртки остаются в старых API. `Struct` и `Any` — для произвольного JSON и «сообщения любого типа», в контрактах их избегают: проверка типов пропадает.

Сериализация — `proto.Marshal` и `proto.Unmarshal`, сравнение — `proto.Equal`, глубокая копия — `proto.Clone`. JSON — пакет `protojson`: это отдельное каноническое отображение со своими правилами, и `encoding/json` для сообщений protobuf не годится (он не знает про `oneof`, enum по именам, `Timestamp` строкой).

## Как это в C#

- Генерация. В .NET `Grpc.Tools` встраивает `protoc` в MSBuild: `<Protobuf Include="orders.proto" GrpcServices="Server" />` в `.csproj`, и классы появляются при сборке, а в репозитории их нет. В Go код генерируют отдельным шагом (`buf generate`) и обычно коммитят: `go build` про `.proto` ничего не знает.
- Сгенерированный тип. В C# — класс со свойствами, `optional` даёт `HasComment` и `ClearComment()`, `oneof` — перечисление `PaymentOneofCase`, `repeated` — `RepeatedField<T>` только для чтения (добавляют `Add`). В Go — структура с открытыми полями, `optional` — указатель, `oneof` — интерфейс с обёртками.
- Равенство. C#-классы protobuf переопределяют `Equals` — сравнение по значению, `==` для них — по ссылке. В Go `==` у `*Order` тоже сравнивает указатели, а по значению — только `proto.Equal`; `reflect.DeepEqual` не годится: он полезет во внутреннее состояние сообщения.
- JSON. `JsonFormatter` / `JsonParser` из `Google.Protobuf` ↔ `protojson`; правила одинаковые, потому что это одно каноническое отображение proto3: lowerCamelCase, `int64` строкой, enum именем. `System.Text.Json` для сообщений protobuf тоже не подходит — как и `encoding/json`.

## Ловушки

### Сообщение нельзя копировать по значению

```go
a := &Order{Id: 1}
b := *a // go vet: assignment copies lock value to b: ... contains sync.Mutex
```

Сгенерированная структура содержит служебное `protoimpl.MessageState` с мьютексом и кешем размера. Копия по значению делит с оригиналом внутренние указатели и ломает кеш — `go vet` ловит это правилом copylocks. Сообщения передают указателями, копию делают `proto.Clone(a).(*Order)`. По той же причине не пишут `[]Order` и `func f(o Order)`.

### Байты одного сообщения бывают разными

```go
b1, _ := proto.Marshal(order) // в order есть map
b2, _ := proto.Marshal(order)
bytes.Equal(b1, b2) // может быть false
```

Записи `map` кодируются в порядке обхода, а он в Go случайный. `proto.MarshalOptions{Deterministic: true}` сортирует ключи — но и это стабильно только в рамках одной версии библиотеки и одного бинарника. Сравнивайте сообщения `proto.Equal`, а не байты. С JSON ещё строже: `protojson` **нарочно** вставляет случайные пробелы, чтобы никто не сравнивал его вывод побайтно — нужен стабильный текст, прогоняйте через `json.Compact`/`json.Indent` (как в примере).

### Нулевое значение enum — осмысленный статус

```proto
enum OrderStatus {
  ORDER_STATUS_NEW = 0; // всё, где статус не задан, — «новое»
  ORDER_STATUS_PAID = 1;
}
```

Нулевое значение — то, что получатель видит, если поле не пришло: старый клиент, который про статус не знает, или забытый `Status:` в Go. С `NEW = 0` такой заказ тихо станет «новым». С `ORDER_STATUS_UNSPECIFIED = 0` сервер видит «не задано» и может вернуть ошибку. buf lint требует такой суффикс правилом `ENUM_ZERO_VALUE_SUFFIX` (урок 03).

## Задание

Прокси и маршрутизаторы иногда читают одно поле из сообщения, не разбирая его целиком и даже не зная схемы. Напишите такой разбор на `google.golang.org/protobuf/encoding/protowire`. Контракт `order.proto` лежит рядом с тестами: по нему тесты собирают байты через `proto.Marshal`.

```go
var (
	ErrMalformed = errors.New("protobuf: malformed message")
	ErrWireType  = errors.New("protobuf: unexpected wire type")
)

type Field struct {
	Num  protowire.Number
	Type protowire.Type
}

func Fields(b []byte) ([]Field, error)
func Varint(b []byte, num protowire.Number) (uint64, bool, error)
```

**`Fields(b)`** проходит сообщение целиком и возвращает поля верхнего уровня в порядке появления в байтах — каждое вхождение отдельно (у `repeated string` их несколько). Вложенные сообщения и строки внутрь не разбираются.

- Пустой или `nil` вход — результат длины 0 и `nil`-ошибка.
- Допустимые wire type: `VarintType`, `Fixed32Type`, `Fixed64Type`, `BytesType`. Группы (`StartGroupType`, `EndGroupType`) и неизвестные типы 6 и 7 — ошибка `ErrMalformed` (как и ниже).
- Ошибка — любой сбой разбора: обрезанный или переполненный varint (тег, значение, длина), номер поля 0 (верхняя граница номера, `protowire.MaxValidNumber`, не проверяется: такие номера принимаются как обычные), длина или фиксированное значение, выходящие за конец данных. Тогда результат `nil`, а ошибка удовлетворяет `errors.Is(err, ErrMalformed)`.

**`Varint(b, num)`** возвращает значение поля `num` как `uint64` (`int64(-1)` даёт `math.MaxUint64`, `sint64` приходит в zigzag — его не декодируйте).

- Сначала проверяется всё сообщение по правилам `Fields`: если оно испорчено где угодно, даже после нужного поля, — `0, false` и ошибка `ErrMalformed`.
- Поле встречается несколько раз — побеждает последнее вхождение (так склейка двух сообщений работает как слияние). Поля нет — `0, false, nil`.
- Хотя бы одно вхождение `num` с wire type не `VarintType` — `0, false` и ошибка `errors.Is(err, ErrWireType)`.
