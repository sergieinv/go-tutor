---
title: Контекст запроса
summary: r.Context() — когда он отменяется, передача в БД и исходящие вызовы, дедлайн через middleware и http.TimeoutHandler, WithoutCancel для фоновой работы, значения запроса и ResponseController.
difficulty: 4
minutes: 30
prerequisites:
  - 05-concurrency/18-context
  - 08-web/04-middleware
docs:
  - title: "Пакет net/http: Request.Context"
    url: https://pkg.go.dev/net/http#Request.Context
  - title: "Пакет net/http: TimeoutHandler"
    url: https://pkg.go.dev/net/http#TimeoutHandler
  - title: "Пакет net/http: ResponseController"
    url: https://pkg.go.dev/net/http#ResponseController
runner:
  race: true
---

Каждый входящий запрос несёт `context.Context` — его возвращает `r.Context()`. Сервер отменяет этот контекст, когда клиент разорвал соединение (для HTTP/2 — сбросил поток) и когда `ServeHTTP` вернулся. Хендлер обязан передавать `r.Context()` во все зависимые вызовы: `db.QueryContext(ctx, ...)`, `http.NewRequestWithContext(ctx, ...)`, свои функции с `ctx` первым параметром. Тогда ушедший клиент не держит запрос к базе, а его отмена доходит до самого нижнего уровня.

А вот `srv.Shutdown` контексты активных запросов **не** отменяет — он ждёт, пока они закончатся сами. Чтобы при остановке прервать долгие запросы, сервер создают с `BaseContext` (следующий урок). `srv.Close()` закрывает соединения, и контексты запросов отменяются вместе с ними.

Свой дедлайн на запрос ставит middleware: `context.WithTimeout(r.Context(), d)` и `next.ServeHTTP(w, r.WithContext(ctx))`. Это мягкий лимит: хендлер сам замечает `ctx.Done()` и выбирает ответ, обычно `504 Gateway Timeout`. Жёсткий вариант — `http.TimeoutHandler(h, d, msg)`: по истечении `d` он сам отвечает `503` с текстом `msg`, а запись опоздавшего хендлера возвращает `http.ErrHandlerTimeout`. Ответ он буферизует в памяти, поэтому для стриминга не годится. Таймауты записи на уровне соединения (`srv.WriteTimeout`) можно сдвинуть для одного запроса: `http.NewResponseController(w).SetWriteDeadline(t)` — например, продлить для долгой выгрузки файла (нулевое `t` снимает дедлайн).

## Как это в C#

- `HttpContext.RequestAborted` и параметр `CancellationToken` в action — это `r.Context()`. В ASP.NET Core токен приходит отдельным параметром, а `HttpContext` живёт сам по себе; в Go контекст — часть `*http.Request`, и новый контекст «прикрепляют» через `r.WithContext(ctx)`.
- `RequestTimeoutsMiddleware` (.NET 8, `[RequestTimeout]`) отменяет `RequestAborted` по таймауту — как middleware с `context.WithTimeout`. Ответ `504` в ASP.NET Core пишет middleware; в Go — сам хендлер, увидев `ctx.Err()`.
- `HttpContext.Items` → `context.WithValue` с неэкспортируемым ключом. Но в Go значения контекста неизменяемы: новое значение — новый контекст и новый `*http.Request`.
- Фоновая работа после ответа в .NET — `IHostedService` и очередь, а не `Task.Run` с `RequestAborted`. В Go то же предупреждение: горутине нужен `context.WithoutCancel(r.Context())` (значения есть, отмены нет) и свой лимит.

## Ловушки

### `context.Background()` вместо `r.Context()`

```go
func handler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(context.Background(), heavySQL) // клиент ушёл — запрос идёт
	// ...
}
```

Тяжёлый запрос к базе переживёт клиента, который давно закрыл вкладку, и будет занимать соединение пула. Передавайте `r.Context()` (или производный от него) во всё, что ждёт.

### Горутина с контекстом запроса после ответа

```go
func handler(w http.ResponseWriter, r *http.Request) {
	go sendAudit(r.Context(), event) // контекст отменится, как только handler вернётся
	w.WriteHeader(http.StatusAccepted)
}
```

`r.Context()` отменяется сразу после выхода из `ServeHTTP`, и фоновая работа оборвётся на первом же вызове с контекстом. Нужен `context.WithoutCancel(r.Context())` и собственный `WithTimeout` поверх него. И никогда не пишите в `w` из горутины после возврата хендлера.

### Обрыв соединения замечается не всегда

```go
func upload(w http.ResponseWriter, r *http.Request) {
	slowWork(r.Context()) // тело POST ещё не прочитано
}
```

HTTP/1.1-сервер узнаёт о разрыве, только когда читает из соединения в фоне, а фоновое чтение начинается после того, как тело запроса прочитано до конца. Пока тело не дочитано, `r.Context()` может не отмениться при уходе клиента — читайте тело (с `http.MaxBytesReader`) до долгой работы.

## Задание

Напишите хендлер, который загружает запись по `id` из query-строки и уважает контекст запроса и свой лимит времени:

```go
var ErrNotFound = errors.New("not found")

type Fetcher func(ctx context.Context, id string) (string, error)

func Handler(fetch Fetcher, timeout time.Duration) http.Handler
```

Требования:

- `id` берётся из `r.URL.Query().Get("id")`; если он пуст — ответ `400` с телом `missing id`, `fetch` не вызывается;
- `fetch` получает контекст, производный от `r.Context()` (значения запроса в нём видны). При `timeout > 0` у него лимит `timeout`, при `timeout <= 0` собственного лимита нет;
- `fetch` вызывается в отдельной горутине, хендлер ждёт либо её результат, либо завершение контекста. Если контекст завершился раньше, хендлер отвечает сразу, не дожидаясь `fetch`; горутина не должна зависнуть, когда `fetch` всё-таки вернётся (для этого достаточно канала с буфером на одно значение). Пишет в `w` только сам хендлер;
- ответ выбирается по первому подходящему правилу:
  1. `fetch` вернул `err == nil` — `200`, тело — строка результата как есть;
  2. `r.Context()` завершён (клиент ушёл, истёк дедлайн самого запроса) — `503`, тело `request canceled`;
  3. истёк собственный лимит `timeout` — `504`, тело `timeout` (правила проверяются по порядку, поэтому дедлайн самого запроса, раньше которого истёк контекст `fetch`, уже попал в правило 2);
  4. `errors.Is(err, ErrNotFound)` — `404`, тело `not found`;
  5. любая другая ошибка — `500`, тело `internal error`.
- ответы с ошибкой пишутся через `http.Error(w, msg, code)` — тело получает перевод строки в конце.
