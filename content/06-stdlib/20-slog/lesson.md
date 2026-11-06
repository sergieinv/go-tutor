---
title: Структурное логирование (slog)
summary: log/slog — уровни, атрибуты, TextHandler и JSONHandler, With и группы, LogValuer, HandlerOptions.Level и ReplaceAttr, контекст и свой Handler.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/03-interfaces
  - 05-concurrency/18-context
docs:
  - title: "Пакет log/slog"
    url: https://pkg.go.dev/log/slog
  - title: "Go Blog: Structured Logging with slog"
    url: https://go.dev/blog/slog
  - title: "Руководство по написанию slog.Handler"
    url: https://github.com/golang/example/blob/master/slog-handler-guide/README.md
---

Пакет `log/slog` (Go 1.21) — стандартный структурный логгер. Запись — это время, уровень, сообщение и атрибуты `ключ=значение`. `slog.Logger` — фасад с методами `Debug`/`Info`/`Warn`/`Error`, а куда и в каком виде попадёт запись, решает `slog.Handler`: `TextHandler` пишет `key=value`, `JSONHandler` — JSON-строку, свой обработчик может слать записи куда угодно.

Атрибуты передают парами `"key", value` или типизированными конструкторами `slog.Int`, `slog.String`, `slog.Any`. `logger.With(...)` возвращает логгер с атрибутами, которые попадут в каждую запись, `slog.Group` и `WithGroup` вкладывают атрибуты под общий ключ.

## Как это в C#

- `ILogger` + провайдеры ≈ `slog.Logger` + `Handler`. Но в .NET логгер получают через DI с категорией `ILogger<T>`; в Go логгер передают явно или берут глобальный `slog.Default()`.
- Шаблон сообщения `"User {UserId} logged in"` в .NET одновременно текст и источник свойств. В slog сообщение — константная строка, а данные идут отдельными атрибутами: `log.Info("вход", "user_id", id)`.
- `BeginScope` → `logger.With(...)`: атрибуты, общие для группы записей. Только `With` возвращает новый логгер, а не открывает область через `using`.
- Минимальный уровень из `appsettings.json` → `HandlerOptions.Level`; менять его на лету позволяет `slog.LevelVar`. Уровней четыре, но это просто числа: `slog.Level(-8)` — ещё подробнее `Debug`.

## Ловушки

### Нечётное число аргументов

```go
log.Info("заказ создан", "id", 42, "сумма") // у "сумма" нет значения
// level=INFO msg="заказ создан" id=42 !BADKEY=сумма
```

Пары ключ-значение не проверяет компилятор — только `go vet` (анализатор `slog`). Включите `vet` в CI или пишите `slog.Int("id", 42)`: типизированные атрибуты ошибиться не дают.

### Свой `Handler` теряет `With` и группы

```go
func (h *myHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return h.next.WithAttrs(as) // вернули внутренний обработчик — обёртка пропала
}
```

`Logger.With` и `WithGroup` вызывают `WithAttrs`/`WithGroup` у обработчика и дальше работают с тем, что вернулось. Обёртка должна возвращать **новую обёртку** вокруг `h.next.WithAttrs(as)`, иначе после первого `With` её логика перестанет работать.

### Дорогие вычисления в аргументах

```go
log.Debug("состояние", "dump", expensiveDump()) // вызывается, даже если Debug выключен
```

Аргументы вычисляются до вызова, а уровень проверяется внутри. Для дорогих значений реализуйте `LogValuer` — `LogValue` вызывается только при реальном выводе — или проверьте `log.Enabled(ctx, slog.LevelDebug)`.

## Задание

Напишите обработчик, который добавляет к каждой записи идентификатор запроса из `context.Context`:

```go
func WithRequestID(ctx context.Context, id string) context.Context
func RequestID(ctx context.Context) string
func NewContextHandler(next slog.Handler) slog.Handler
```

Требования:

- `WithRequestID` возвращает дочерний контекст с идентификатором, `RequestID` достаёт его (`""`, если идентификатора нет). Ключ контекста — неэкспортируемый тип пакета;
- обработчик, возвращённый `NewContextHandler`, при записи с контекстом, где есть непустой идентификатор, добавляет к записи атрибут `request_id` со строковым значением и передаёт запись в `next`; без идентификатора запись передаётся без изменений;
- атрибут добавляется так же, как атрибут самого вызова: после `logger.WithGroup("g")` он окажется внутри группы `g`, а атрибуты из `logger.With(...)` остаются в записи;
- уровень отсечения определяет `next`: если `next` не пропускает уровень, запись не выводится;
- обёртка сохраняется после `logger.With(...)` и `logger.WithGroup(...)` — `request_id` добавляется и в записи производных логгеров;
- переданный в `Handle` контекст не меняется, обработчик безопасен для одновременного использования из нескольких горутин.
