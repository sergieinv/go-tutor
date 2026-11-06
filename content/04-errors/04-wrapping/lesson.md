---
title: Обёртки ошибок
summary: "%w и цепочки ошибок, errors.Is/As/Unwrap, errors.Join, свой Unwrap — и когда детали лучше скрыть."
difficulty: 3
minutes: 25
prerequisites:
  - 04-errors/02-sentinel-errors
  - 04-errors/03-custom-errors
docs:
  - title: "Go Blog: Working with Errors in Go 1.13"
    url: https://go.dev/blog/go1.13-errors
  - title: "Пакет errors"
    url: https://pkg.go.dev/errors
  - title: "Пакет fmt: Errorf"
    url: https://pkg.go.dev/fmt#Errorf
---

Ошибка проходит через несколько уровней: репозиторий, сервис, обработчик запроса. Каждый добавляет контекст — какой пользователь, какой запрос, — но вызывающий код должен по-прежнему узнать исходную причину. Для этого ошибку оборачивают: `fmt.Errorf("load profile %d: %w", id, err)` создаёт новую ошибку, внутри которой лежит старая.

Получается цепочка. `errors.Unwrap` снимает один слой, а `errors.Is` и `errors.As` обходят цепочку целиком в поисках нужного значения или типа. С Go 1.20 ошибка может оборачивать сразу несколько причин — через `errors.Join` или несколько `%w`, и тогда цепочка становится деревом.

## Как это в C#

- `%w` — это `InnerException`, но без отдельного конструктора: обёртка создаётся тем же `fmt.Errorf`, что и обычная ошибка с текстом.
- `errors.Is` / `errors.As` — обход цепочки `InnerException` с проверкой на каждом шаге, готовый и единый для всех ошибок. В C# такой цикл пишут вручную или ловят исключение по типу.
- `errors.Join` — аналог `AggregateException`: несколько ошибок в одной, и `errors.Is` проверяет каждую.
- Свою обёртку в C# делают наследованием и передачей `inner` в базовый конструктор. В Go — методом `Unwrap() error` (или `Unwrap() []error` для нескольких причин) у своего типа.

## Под капотом

`fmt.Errorf` с одним `%w` возвращает `*fmt.wrapError` с методом `Unwrap() error`, с несколькими — `*fmt.wrapErrors` с методом `Unwrap() []error`. `errors.Join` возвращает `*errors.joinError`, тоже с `Unwrap() []error`.

`errors.Is(err, target)` обходит дерево в глубину: сначала сравнивает текущую ошибку с `target` через `==`, затем, если у ошибки есть метод `Is(error) bool`, спрашивает его, потом спускается по `Unwrap`. Метод `Is` позволяет своему типу «притвориться» sentinel'ом: например, `*QueryError` со статусом 404 может отвечать `true` на `errors.Is(err, ErrNotFound)`. Так же `errors.As` уважает метод `As(any) bool`. `errors.Unwrap` вызывает только `Unwrap() error` и для `Join`-ошибок возвращает `nil`.

С Go 1.26 есть и обобщённый вариант `errors.AsType[*QueryError](err)`, который возвращает найденное значение без отдельной переменной; в этом курсе (Go 1.25) пользуемся `errors.As`.

## Ловушки

### `%w` делает ошибку частью API

```go
// пакет storage
return fmt.Errorf("get user: %w", pgErr) // наружу торчит *pgconn.PgError
```

Обёрнутую ошибку вызывающий может достать через `errors.As` и начать от неё зависеть. Смените базу — и чужой код сломается. На границе пакета переводите внутренние ошибки в свои (`ErrNotFound`) или оборачивайте через `%v`, если причину нужно показать только в тексте.

### Обёртка `nil`-ошибки — не `nil`

```go
var err error // nil
wrapped := fmt.Errorf("save: %w", err)
fmt.Println(wrapped != nil) // true, текст — "save: %!w(<nil>)"
```

`fmt.Errorf` всегда возвращает ошибку. Оборачивайте только внутри `if err != nil`. А вот `errors.Join(nil, nil)` возвращает `nil`.

### Повтор контекста на каждом уровне

```go
return fmt.Errorf("error: failed to load user: %w", err)
// error: failed to load user: error: failed to query: error: ...
```

Не пишите «error» и «failed to» — что это ошибка, и так ясно. Каждый уровень добавляет только то, чего не знает нижний: что он делал и с какими аргументами.

## Задание

Напишите функцию `Validate(u User) error`, которая проверяет все поля пользователя и возвращает **все** найденные ошибки сразу.

Типы и ошибки объявлены в шаблоне:

```go
var (
	ErrRequired = errors.New("required")
	ErrTooLong  = errors.New("too long")
	ErrInvalid  = errors.New("invalid")
)

type User struct {
	Name  string
	Email string
	Age   int
}

type FieldError struct {
	Field string // "name", "email" или "age"
	Err   error  // одна из трёх ошибок выше
}
```

Требования:

- допишите `FieldError` метод `Unwrap() error`, чтобы `errors.Is(err, ErrRequired)` видел причину сквозь `*FieldError`;
- `Name`: пустое — `ErrRequired`, длиннее 32 байт — `ErrTooLong`;
- `Email`: пустой — `ErrRequired`, без символа `@` — `ErrInvalid`;
- `Age`: меньше 0 или больше 150 — `ErrInvalid`;
- для каждого поля — не больше одной ошибки, в виде `&FieldError{Field: ..., Err: ...}`;
- ошибки объединяются через `errors.Join` в порядке name, email, age; если ошибок нет — вернуть `nil`.
