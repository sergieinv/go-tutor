---
title: Секреты, логи и ошибки
summary: Секреты из окружения и файлов, тип Secret с Format, LogValue и MarshalJSON, ReplaceAttr для опасных ключей в slog, ответ клиенту без деталей — ошибка в лог с id запроса, паника в 500 без стека.
difficulty: 4
minutes: 30
prerequisites:
  - 06-stdlib/20-slog
  - 06-stdlib/19-environment
  - 08-web/04-middleware
  - 04-errors/03-custom-errors
  - 09-security/01-threat-model
docs:
  - title: "Пакет log/slog: LogValuer и HandlerOptions.ReplaceAttr"
    url: https://pkg.go.dev/log/slog#HandlerOptions
  - title: "Пакет fmt: Formatter, Stringer, GoStringer"
    url: https://pkg.go.dev/fmt#Formatter
  - title: "OWASP Logging Cheat Sheet"
    url: https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
runner:
  race: true
---

Пароль базы чаще всего утекает не через взлом сервера, а через артефакты, которые читают многие: репозиторий, логи, трекер ошибок, ответ API. Три правила. **Секреты — не в коде**: строка в репозитории остаётся в истории git навсегда, её видит каждый, у кого есть клон. Секрет приходит из окружения или из файла, смонтированного оркестратором (соглашение `NAME_FILE` — путь к файлу с секретом, так делают Docker secrets и Kubernetes). **Секреты — не в логах**: логи хранят дольше и показывают шире, чем базу. **Подробности ошибок — не клиенту**: текст `pq: password authentication failed for user "app"` или путь к файлу помогает атакующему, а не пользователю.

В Go эти правила держатся на типах. Тип `Secret` со своими методами печати делает утечку невозможной «случайно»: `fmt` спрашивает у значения `Format` (для всех глаголов, включая `%#v` и `%d`), `slog` — `LogValue`, `encoding/json` — `MarshalJSON`. Настоящее значение — только через явный `Reveal()`, его легко найти поиском по коду. Второй рубеж — `HandlerOptions.ReplaceAttr` у `slog`: функция видит каждый атрибут (и атрибуты внутри групп, с путём групп в `groups`) и заменяет значения под ключами вроде `password`, `authorization`, `cookie`, даже если кто-то положил пароль в лог обычной строкой.

Ошибки делятся в одной точке. Хендлеры возвращают `error`, а обёртка решает: полный текст ошибки — в лог вместе с id запроса, клиенту — общее «internal error» и тот же id, по которому поддержка найдёт запись. Ошибка, которую можно показать (`404 order not found`), — отдельный тип с публичным сообщением и внутренней причиной. Паника — тот же путь: recover-middleware из урока 08-web/04 пишет значение и стек (`debug.Stack()`) в лог, а клиенту — 500 без подробностей.

## Как это в C#

- `IConfiguration` с User Secrets, переменными окружения и Key Vault — в Go `os.Getenv`/`os.LookupEnv` и чтение файла; провайдеров конфигурации нет, функция `loadSecret` из примера — десять строк.
- `UseExceptionHandler` + `ProblemDetails` в Production не показывают исключение клиенту, а `UseDeveloperExceptionPage` показывает. В Go «страницы разработчика» нет, но есть её аналог в каждом `http.Error(w, err.Error(), 500)` — утечку пишут руками, и защиту тоже.
- Редакцию в `Microsoft.Extensions.Logging` делает пакет `Microsoft.Extensions.Compliance.Redaction` с атрибутами классификации данных. В Go — `LogValuer` у типа и `ReplaceAttr` у обработчика, без пакетов.
- `ToString()` у record печатает все свойства, включая пароль — та же ловушка, что `%+v` у структуры. В C# переопределяют `PrintMembers` или `ToString`, в Go — методы у типа поля.
- `HttpContext.TraceIdentifier` — готовый id запроса. В Go его создаёт middleware: `crypto/rand.Text()` (Go 1.24) — 26 символов base32, не меньше 128 бит случайности.

## Ловушки

### `String()` есть, а секрет всё равно печатается

```go
type Secret string
func (Secret) String() string { return "[REDACTED]" }

type Config struct {
	Token Secret
	key   Secret // неэкспортируемое поле
}
c := Config{"tok", "k"}
fmt.Printf("%#v\n", c) // УЯЗВИМО: main.Config{Token:"tok", key:"k"}
fmt.Printf("%d\n", c.Token) // УЯЗВИМО: %!d(main.Secret=tok)
```

`String()` вызывается только для `%v`, `%s`, `%q`, `%x`, `%X`; `%#v` спрашивает `GoString()`, а неподходящий глагол печатает значение в диагностике `%!d(...)`. Методы неэкспортируемых полей `fmt` не вызывает вовсе — нет доступа через рефлексию. Надёжно — метод `Format(f fmt.State, verb rune)`: он перехватывает все глаголы, — и только экспортируемые поля с типом `Secret`.

### JSON-лог печатает структуру через `encoding/json`

```go
log.Info("config", "cfg", cfg)
// УЯЗВИМО без MarshalJSON: {"cfg":{"Token":"tok"}} —
// LogValue вызывается для самого значения атрибута, а не для полей структуры
```

`JSONHandler` сериализует структуру через `encoding/json`, `TextHandler` — через `fmt` (`%+v`). `LogValue` поля структуры ни один из них не вызовет. Поэтому у `Secret` три метода: `Format`, `LogValue` и `MarshalJSON`.

### `err.Error()` в ответе

```go
if err := repo.Save(ctx, order); err != nil {
	// УЯЗВИМО: клиент видит имена таблиц, адрес базы и текст SQL
	http.Error(w, err.Error(), http.StatusInternalServerError)
	return
}
```

Ошибка из глубины программы обёрнута `%w` несколько раз и несёт всё, что знала каждая функция по пути. Клиенту — `http.Error(w, "internal error", 500)` и id запроса, ошибку целиком — в лог.

## Задание

Напишите тип секрета, логгер и обёртку хендлеров. Типы `Secret` (с методом `Reveal`) и `PublicError` даны в шаблоне:

```go
type Secret string
func (s Secret) Reveal() string

type PublicError struct {
	Status int    // код ответа
	Msg    string // можно показать клиенту
	Err    error  // причина — только для лога
}
func (e *PublicError) Error() string // Msg или "Msg: Err.Error()"
func (e *PublicError) Unwrap() error

func NewLogger(w io.Writer) *slog.Logger
func Handle(log *slog.Logger, h func(w http.ResponseWriter, r *http.Request) error) http.Handler
```

**`Secret`.** Добавьте методы так, чтобы значение печаталось строкой `[REDACTED]`:

- в `fmt` — любым глаголом (`%v`, `%+v`, `%#v`, `%s`, `%q`, `%x`, `%d`, с шириной и точностью — флаги игнорируются, всегда печатается ровно `[REDACTED]`) и в составе структуры, где `Secret` — экспортируемое поле;
- в `encoding/json` — как JSON-строка `"[REDACTED]"`, в том числе как поле структуры;
- в `slog` — как значение атрибута.

**`NewLogger(w)`** — `slog.New(slog.NewJSONHandler(w, opts))` с уровнем по умолчанию (Info). Значение атрибута с ключом `password`, `secret`, `token`, `authorization`, `cookie` или `api_key` (без учёта регистра) заменяется строкой `[REDACTED]` на любом уровне вложенности групп (сама группа под таким ключом не заменяется — как и в `ReplaceAttr`, проверяются атрибуты внутри неё по их ключам), включая атрибуты из `With` и `WithGroup`. Остальные атрибуты не меняются.

**`Handle(log, h)`** для каждого запроса:

1. Создаёт id запроса `crypto/rand.Text()` и ставит заголовок ответа `X-Request-ID` до вызова `h`.
2. Вызывает `h`. Если `h` вернул `nil` — больше ничего не делает (ответ написал `h`). Если `h` вернул ошибку, считается, что он ничего не писал.
3. Ошибка `err` записывается в лог одной записью: `log.Error("request failed", "request_id", id, "method", r.Method, "path", r.URL.Path, "err", err)`. Затем ответ: если в цепочке `err` есть `*PublicError` (`errors.As`) — его `Status` и сообщение `Msg`; иначе — `500` и сообщение `internal error`.
4. Паника в `h` не выходит из `Handle`: в лог — одна запись `log.Error("panic", "request_id", id, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))`, ответ — `500` и `internal error`. Исключение — паника значением `http.ErrAbortHandler`: её `Handle` пробрасывает дальше (`panic(v)`), ничего не логируя и не отвечая.

Ответ с ошибкой — заголовок `Content-Type: application/json` и тело — JSON-объект ровно с двумя строковыми полями: `{"error": сообщение, "request_id": id}`. Ни текст внутренней ошибки, ни причина `PublicError.Err`, ни значение паники в ответ не попадают.
