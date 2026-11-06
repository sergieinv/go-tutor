---
title: JSON
summary: encoding/json — Marshal и Unmarshal, struct tags (omitempty, omitzero, "-", string), только экспортируемые поля, RawMessage, числа как float64, строгий Decoder и свой MarshalJSON.
difficulty: 3
minutes: 30
prerequisites:
  - 03-types/01-structs
  - 03-types/10-standard-interfaces
  - 04-errors/01-errors
docs:
  - title: "Пакет encoding/json"
    url: https://pkg.go.dev/encoding/json
  - title: "Go Blog: JSON and Go"
    url: https://go.dev/blog/json
---

JSON в Go — пакет `encoding/json` из стандартной библиотеки, без NuGet. `json.Marshal(v)` превращает значение в `[]byte`, `json.Unmarshal(data, &v)` заполняет значение по указателю. Работают они через рефлексию и видят только **экспортируемые** поля — с заглавной буквы.

Как называть ключи и что пропускать, описывают struct tags — строковые литералы после типа поля: `` `json:"name,omitempty"` ``. Это обычные строки, которые пакет читает через `reflect`; компилятор их не проверяет, поэтому опечатку в теге найдёт только `go vet` или тест.

## Как это в C#

- Атрибуты `[JsonPropertyName("name")]`, `[JsonIgnore]`, `[JsonIgnore(Condition = WhenWritingDefault)]`, `[JsonNumberHandling(AllowReadingFromString)]` → теги `json:"name"`, `json:"-"`, `json:",omitzero"` (Go 1.24+) или `omitempty`, `json:",string"`.
- `System.Text.Json` по умолчанию различает регистр ключей при чтении; `encoding/json` — нет: `"NAME"` попадёт в поле с тегом `name`. Лишние ключи в обоих молча игнорируются; строгий режим — `Decoder.DisallowUnknownFields()` (аналог `JsonUnmappedMemberHandling.Disallow` из .NET 8).
- `JsonDocument`/`JsonNode` → `map[string]any` и `[]any`, а числа в них — всегда `float64`. Отложить разбор части документа — `json.RawMessage`, как `JsonElement`.
- `JsonConverter<T>` → методы `MarshalJSON() ([]byte, error)` и `UnmarshalJSON([]byte) error` на самом типе — пакет находит их через интерфейсы `json.Marshaler` и `json.Unmarshaler`.

## Ловушки

### Неэкспортируемые поля молча пропадают

```go
type point struct {
	x, y int // с маленькой буквы
}
b, _ := json.Marshal(point{1, 2})
fmt.Println(string(b)) // {}
```

Ни ошибки, ни предупреждения: пакет `json` просто не видит такие поля. Тег на неэкспортируемом поле не помогает — `go vet` об этом предупредит.

### `omitempty` не работает для структур

```go
type event struct {
	At time.Time `json:"at,omitempty"`
}
// {"at":"0001-01-01T00:00:00Z"} — структура никогда не «пустая»
```

`omitempty` пропускает `false`, `0`, `""`, `nil` и пустые срез и map, но не структуры. С Go 1.24 есть `omitzero`: он пропускает нулевое значение любого типа, а если у типа есть метод `IsZero() bool` (как у `time.Time`) — спрашивает его.

### Числа в `any` — это `float64`

```go
var m map[string]any
json.Unmarshal([]byte(`{"id": 9007199254740993}`), &m)
id := m["id"].(int) // паника: там float64, и значение уже 9007199254740992
```

`float64` точно хранит целые только до 2⁵³. Разбирайте в структуру с полем `int64` или включите `Decoder.UseNumber()` — тогда число придёт как `json.Number` (строка) без потерь.

## Задание

Напишите разбор и запись конфигурации сервиса.

```go
type Config struct {
	Name  string
	Port  int
	Tags  []string
	Debug bool
}
```

Ключи JSON: `name`, `port`, `tags`, `debug`.

`func ParseConfig(data []byte) (Config, error)`:

- `data` должен содержать ровно один JSON-объект; пробельные символы вокруг допустимы, любые другие данные после объекта — ошибка;
- неизвестный ключ — ошибка; значение не того типа — ошибка;
- ключи сопоставляются с полями без учёта регистра, как это делает `encoding/json`: `{"NAME":"api"}` задаёт `name` и неизвестным ключом не считается;
- `name` обязателен: если ключа нет или строка пустая — ошибка;
- если `port` отсутствует или равен `0`, подставляется `8080`; порт вне диапазона `1..65535` — ошибка;
- при любой ошибке возвращается нулевой `Config{}`.

`func EncodeConfig(c Config) ([]byte, error)`:

- компактный JSON без пробелов и перевода строки, ключи в порядке `name`, `port`, `tags`, `debug`;
- `tags` не выводится, если срез пустой или `nil`; `debug` не выводится, если `false`; `name` и `port` выводятся всегда;
- значения не проверяются и не меняются: `EncodeConfig(Config{Name: "api"})` → `{"name":"api","port":0}`.
