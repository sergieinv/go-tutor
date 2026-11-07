---
title: Middleware
summary: Middleware как func(http.Handler) http.Handler, порядок обёрток и Chain, обёртка ResponseWriter для статуса и размера (Unwrap и ResponseController), recover, request ID в контексте, TimeoutHandler и MaxBytesHandler.
difficulty: 4
minutes: 35
prerequisites:
  - 08-web/03-routing
  - 05-concurrency/18-context
  - 04-errors/06-panic-recover
  - 03-types/06-embedding
docs:
  - title: "Пакет net/http: Handler, ResponseController"
    url: https://pkg.go.dev/net/http#ResponseController
  - title: "Исходники net/http/server.go"
    url: https://cs.opensource.google/go/go/+/refs/tags/go1.25.0:src/net/http/server.go
---

В Go нет конвейера middleware как отдельной сущности. Middleware — это функция, которая принимает `http.Handler` и возвращает новый `http.Handler`: обёртка делает что-то до вызова `next.ServeHTTP(w, r)`, после него или вместо него. Цепочка — просто вложенные вызовы: `logging(recoverer(requestID(mux)))`. Кто снаружи, тот получает запрос первым и последним видит ответ. Чтобы не считать скобки, пишут `Chain(h, logging, recoverer, requestID)` — функцию из трёх строк, которая применяет обёртки с конца списка.

Обёртка, которой нужно знать статус ответа (логирование, метрики), подменяет `ResponseWriter` своей структурой: встраивает исходный writer и перехватывает `WriteHeader` и `Write`. Встраивание интерфейса даёт только его три метода, поэтому обёртка «теряет» дополнительные интерфейсы исходного writer — `http.Flusher`, `http.Hijacker`, и проверка `w.(http.Flusher)` в хендлере перестаёт срабатывать. С Go 1.20 правильный путь — метод `Unwrap() http.ResponseWriter` у обёртки и `http.NewResponseController(w)` в хендлере: контроллер разворачивает цепочку обёрток и находит `Flush`, `SetWriteDeadline`, `EnableFullDuplex`.

Значения уровня запроса (id запроса, пользователь после аутентификации) middleware кладёт в контекст: `next.ServeHTTP(w, r.WithContext(ctx))`. `WithContext` возвращает **копию** запроса, исходный `r` не меняется. Из готовых обёрток в стандартной библиотеке есть `http.TimeoutHandler(h, d, msg)` — отвечает 503, если `h` не уложился в `d`, и отменяет его контекст, и `http.MaxBytesHandler(h, n)` — ограничивает тело запроса для всех хендлеров сразу.

## Как это в C#

- `app.Use(async (ctx, next) => { ...; await next(ctx); ... })` ≈ `func(next http.Handler) http.Handler`. Порядок тот же: зарегистрированный раньше — снаружи. Только в .NET порядок задаёт последовательность вызовов `Use` на `WebApplication`, а в Go — вложенность выражения.
- `IMiddleware` с DI и `RequestDelegate next` — в Go это обычная функция или структура с полем `next http.Handler`; зависимости передаются параметрами при сборке цепочки.
- `HttpContext.Items` и `HttpContext.TraceIdentifier` → значения `context.Context` с неэкспортируемым ключом. Контекст неизменяем: новое значение — новый контекст и новая копия запроса, а не запись в общий словарь.
- `UseExceptionHandler` есть «из коробки»; в Go сервер сам ловит панику хендлера, но только логирует её и рвёт соединение — ответа 500 клиент не получит. Recover-middleware пишут сами.

## Под капотом

`Server.Serve` принимает соединения в цикле и на каждое запускает горутину `go c.serve(connCtx)`. Метод `conn.serve` читает запросы этого соединения по одному: `readRequest` создаёт `*http.Request` с собственным контекстом (`context.WithCancel` от контекста соединения) и `response` — реализацию `ResponseWriter` с буфером, затем вызывает `serverHandler{c.server}.ServeHTTP(w, w.req)`, то есть корневой хендлер — у нас это внешняя обёртка `logging`. Дальше работает обычный стек вызовов: `logging` подменяет writer на `statusWriter`, `recoverer` ставит `defer`, `requestID` создаёт копию запроса с новым контекстом, и только потом `ServeMux.ServeHTTP` выбирает шаблон `GET /orders/{id}` и записывает в **полученный им** `*Request` поля `Pattern` и значения для `PathValue`. Ответ возвращается через те же функции в обратном порядке: `statusWriter` уже запомнил статус, и `logging` печатает строку после возврата `next.ServeHTTP`. Когда корневой хендлер вернулся, `conn.serve` вызывает `w.finishRequest()` — сбрасывает буфер ответа в сокет — и читает следующий запрос того же keep-alive соединения.

Пока хендлер работает, `conn.serve` запускает фоновое чтение сокета (`connReader.startBackgroundRead`). Если клиент закрыл соединение, это чтение получает `EOF`, `handleReadErrorLocked` вызывает `cancelCtx` — и контекст запроса отменяется. Хендлер, который ждёт базу или другой сервис через `r.Context()`, видит закрытый `ctx.Done()` и прекращает работу вместо того, чтобы считать ответ, который некому отдать. Контекст отменяется и просто по завершении `ServeHTTP` — поэтому горутины, запущенные хендлером «в фон», не должны жить на `r.Context()`. А если хендлер паникует, `conn.serve` ловит панику в своём `defer`, пишет в журнал `http: panic serving ...` и закрывает соединение: клиент получит обрыв, а не 500 — именно поэтому `recoverer` стоит в цепочке.

## Ловушки

### Порядок обёрток

```go
handler := recoverer(logging(mux)) // паника: logging не напечатает ничего
handler := logging(recoverer(mux)) // паника: recoverer ответит 500, logging запишет 500
```

Паника раскручивает стек до ближайшего `recover`. Если логирование внутри `recoverer`, его код после `next.ServeHTTP` не выполнится. Логирование и метрики — снаружи, `recover` — под ними, всё, что меняет запрос (аутентификация, id), — ближе к хендлеру.

### Внешний слой не видит изменений запроса

```go
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Println(r.Pattern) // logging(mux) — "GET /orders/{id}"; logging(requestID(mux)) — ""
	})
}
```

`ServeMux` записывает шаблон в тот `*Request`, который получил сам. Если между ним и `logging` стоит обёртка с `r.WithContext(...)`, это уже копия, и внешний `r.Pattern` пуст. То же с контекстом: значения, добавленные внутренними слоями, наружу не поднимаются. Данные для внешнего слоя передавайте через то, что у слоёв общее: заголовки ответа (`w.Header()`), саму обёртку `ResponseWriter` или изменяемую структуру, которую внешний слой положил в контекст заранее.

### Обёртка прячет `Flusher`

```go
type statusWriter struct {
	http.ResponseWriter // методы: Header, Write, WriteHeader — и всё
	status int
}
// в хендлере за этой обёрткой:
f, ok := w.(http.Flusher) // ok == false, стриминг молча сломан
```

Добавьте обёртке `Unwrap() http.ResponseWriter`, а в хендлерах используйте `http.NewResponseController(w).Flush()` вместо утверждения типа: контроллер пройдёт по `Unwrap` до исходного writer и вернёт ошибку, если `Flush` не поддерживается.

## Задание

Напишите три функции для сборки цепочки middleware:

```go
type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, mws ...Middleware) http.Handler

type Entry struct {
	Method string // r.Method
	Path   string // r.URL.Path
	Status int    // статус ответа
	Bytes  int    // сколько байт тела записано
}

func Logging(record func(Entry)) Middleware

const RequestIDHeader = "X-Request-ID"

func RequestID(gen func() string) Middleware
func RequestIDFrom(ctx context.Context) string
```

`Chain`:

- `Chain(h, a, b, c)` работает как `a(b(c(h)))`: первая middleware в списке — внешняя; без middleware возвращается `h`.

`Logging`:

- вызывает `next`, а после его возврата — `record` ровно один раз;
- `Status` — первый статус, переданный в `WriteHeader`; если первым был `Write` или хендлер ничего не записал — `200`; повторные `WriteHeader` статус не меняют;
- `Bytes` — сумма значений `n`, которые вернули вызовы `Write`;
- обёртка `ResponseWriter` должна иметь метод `Unwrap() http.ResponseWriter`, чтобы `http.NewResponseController(w).Flush()` в хендлере доходил до исходного writer.

`RequestID` и `RequestIDFrom`:

- если в запросе есть заголовок `X-Request-ID` длиной от 1 до 64 символов из `A–Z`, `a–z`, `0–9`, `-` и `_`, используется он; иначе — значение `gen()`; `gen` вызывается только когда заголовок не подошёл;
- id записывается в заголовок ответа `X-Request-ID` до вызова `next`;
- `next` получает запрос, из контекста которого `RequestIDFrom` достаёт этот id; исходный запрос и его контекст не меняются;
- `RequestIDFrom` для контекста без id возвращает `""`.
