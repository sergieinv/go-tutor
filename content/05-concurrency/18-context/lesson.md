---
title: Context
summary: Отмена, дедлайны и значения запроса через context.Context — дерево контекстов, ctx.Done() в select, Cause и AfterFunc.
difficulty: 4
minutes: 30
prerequisites:
  - 05-concurrency/07-select
  - 05-concurrency/08-timeouts
docs:
  - title: "Пакет context"
    url: https://pkg.go.dev/context
  - title: "Go Blog: Go Concurrency Patterns: Context"
    url: https://go.dev/blog/context
  - title: "Go Blog: Contexts and structs"
    url: https://go.dev/blog/context-and-structs
runner:
  race: true
---

`context.Context` — стандартный способ сказать цепочке вызовов «хватит»: клиент отключился, истёк дедлайн, соседняя задача упала. Функция, которая может ждать (сеть, база, канал), принимает `ctx` первым параметром и слушает `ctx.Done()` — канал, который закрывается при отмене. После закрытия `ctx.Err()` возвращает причину: `context.Canceled` или `context.DeadlineExceeded`.

Контексты образуют дерево. Корень — `context.Background()` (в `main`, тестах, на верхнем уровне сервера) или `context.TODO()` (заглушка «контекст сюда ещё не проброшен»). Производные создают функции `With...`: `WithCancel` возвращает контекст и функцию отмены, `WithTimeout`/`WithDeadline` отменяются сами по времени, `WithValue` добавляет пару ключ–значение. Отмена родителя отменяет всех потомков, отмена потомка не трогает ни родителя, ни соседей. Из Go 1.20–1.21 пригодятся ещё: `WithCancelCause` и `context.Cause` — причина отмены своей ошибкой; `AfterFunc(ctx, f)` — запустить `f` при отмене; `WithoutCancel(ctx)` — сохранить значения, но отвязаться от отмены (например, для фоновой записи аудита после ответа клиенту).

## Как это в C#

- `CancellationToken` ≈ `ctx.Done()` + `ctx.Err()`, а `CancellationTokenSource.Cancel()` — функция `cancel`. Разделение то же: `ctx` — сторона наблюдателя, как токен, `cancel` — сторона управления, как источник. Только отдельного типа-источника в Go нет: `With...` возвращает пару `ctx, cancel`.
- `CancellationTokenSource.CreateLinkedTokenSource(parent)` — это `context.WithCancel(parent)`; в Go так устроены все производные контексты: связь с родителем — обязательная часть.
- `new CancellationTokenSource(TimeSpan)` / `CancelAfter` — это `WithTimeout`. Но токен знает только «отменён или нет», а у контекста можно спросить сам срок — `ctx.Deadline()`. Поэтому в Go функциям не нужен отдельный параметр `timeout`, который часто встречается в .NET API.
- `token.ThrowIfCancellationRequested()` бросает исключение. В Go функция возвращает `ctx.Err()` как обычную ошибку, а вызывающий проверяет её через `errors.Is(err, context.Canceled)`. `token.Register(...)` — это `context.AfterFunc`.

## Под капотом

`cancelCtx` хранит канал `done`, ошибку и карту `children` — производные контексты, которые нужно отменить вместе с ним. При создании потомка `propagateCancel` ищет ближайший отменяемый контекст вверх по цепочке и записывает потомка в его `children`; если родитель не отменяется никогда (`Background`), регистрации нет. Отмена закрывает `done`, рекурсивно отменяет `children` и удаляет контекст из карты родителя. `valueCtx` своего канала не имеет — `Done()` и `Err()` берёт у родителя, а `Value` ищет ключ, поднимаясь по ссылкам на родителя. `WithDeadline` с дедлайном позже родительского создаёт обычный `WithCancel`: позже родителя потомок всё равно не проживёт.

## Ловушки

### Забытый `cancel`

```go
ctx, _ := context.WithTimeout(parent, time.Minute) // go vet: the cancel function ... should be called
return fetch(ctx)
```

Пока `cancel` не вызван, потомок висит в `children` родителя, а таймер ждёт свой минуты. Пишите `defer cancel()` сразу после создания — даже если дедлайн и так когда-нибудь сработает.

### Контекст в поле структуры

```go
type Client struct {
	ctx context.Context // чей это контекст? когда он отменяется?
}
func (c *Client) Get(url string) error { /* использует c.ctx */ }
```

Время жизни контекста — один вызов или один запрос, а не объект. Передавайте `ctx` первым параметром каждого метода: `func (c *Client) Get(ctx context.Context, url string) error`.

### `WithValue` для параметров

```go
ctx = context.WithValue(ctx, "limit", 100) // строковый ключ, нетипизированное значение
limit := ctx.Value("limit").(int)            // паника, если значения нет
```

Значения контекста — для сквозных данных запроса (id запроса, трассировка, пользователь после аутентификации), а не для обычных параметров: из сигнатуры не видно, что функции нужно, а компилятор ничего не проверит. Ключ — неэкспортируемый тип пакета, иначе ключи разных пакетов столкнутся (staticcheck предупреждает про ключи встроенных типов).

## Задание

Напишите функцию, которая опрашивает несколько бэкендов одновременно и возвращает первый успешный ответ:

```go
type Backend func(ctx context.Context) (string, error)

var ErrNoBackends = errors.New("no backends")

func Search(ctx context.Context, timeout time.Duration, backends ...Backend) (string, error)
```

Требования:

- все бэкенды запускаются одновременно, каждый в своей горутине, и получают контекст, производный от `ctx`;
- возвращается результат первого бэкенда, ответившего без ошибки; к моменту возврата `Search` контекст, переданный бэкендам, отменён — остальные получают сигнал остановиться;
- если все бэкенды вернули ошибки, `Search` возвращает ошибку, из которой `errors.Is` достаёт каждую из них (`errors.Join`); но если к этому моменту `ctx` уже отменён или истёк лимит, приоритет у ошибки контекста (следующий пункт) — ошибки бэкендов не возвращаются;
- если `timeout > 0`, на весь поиск действует этот лимит; при `timeout <= 0` собственного лимита нет, действует только `ctx`;
- если `ctx` отменён или истёк лимит раньше, чем пришёл успешный ответ, `Search` сразу возвращает ошибку контекста: `errors.Is(err, context.DeadlineExceeded)` для лимита времени, `errors.Is(err, context.Canceled)` для отмены `ctx`;
- `Search` не ждёт бэкенды, которые не ответили, а их горутины не должны зависнуть навсегда, даже если бэкенд игнорирует отмену и отвечает позже;
- без бэкендов — сразу `ErrNoBackends`.
