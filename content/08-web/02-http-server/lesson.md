---
title: HTTP-сервер
summary: http.Handler и HandlerFunc, ResponseWriter (заголовки до WriteHeader, неявный 200, superfluous WriteHeader, http.Error), http.Server с таймаутами вместо голого ListenAndServe и горутина на каждый запрос.
difficulty: 3
minutes: 30
prerequisites:
  - 08-web/01-http-client
  - 03-types/03-interfaces
  - 05-concurrency/12-mutex
docs:
  - title: "Пакет net/http: Server и Handler"
    url: https://pkg.go.dev/net/http#Server
  - title: "Пакет net/http/httptest"
    url: https://pkg.go.dev/net/http/httptest
  - title: "The complete guide to Go net/http timeouts (Cloudflare)"
    url: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
runner:
  race: true
---

Весь HTTP-сервер Go держится на одном интерфейсе:

```go
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}
```

Хендлер получает запрос и пишет ответ в `http.ResponseWriter`. Функцию с такой сигнатурой превращает в хендлер адаптер `http.HandlerFunc`, маршрутизатор `http.ServeMux` — тоже просто `Handler`, который передаёт запрос другим. Сервер — значение `http.Server`: адрес или готовый `net.Listener`, корневой хендлер и таймауты. Никакого хоста приложения: `main` создаёт сервер, вызывает `Serve` и сам решает, когда его остановить.

У `ResponseWriter` строгий порядок: сначала заголовки через `w.Header()`, потом статус `w.WriteHeader(code)`, потом тело `w.Write`. Первый `Write` без `WriteHeader` отправляет статус 200 неявно (и, если `Content-Type` не задан, угадывает его по первым 512 байтам). После отправки статуса изменения `w.Header()` уже ни на что не влияют, а повторный `WriteHeader` игнорируется с сообщением `http: superfluous response.WriteHeader call` в журнале сервера. Для ответа-ошибки есть `http.Error(w, msg, code)`: он ставит `text/plain; charset=utf-8`, `X-Content-Type-Options: nosniff`, статус и пишет `msg` с переводом строки.

Сервер обслуживает каждое соединение в своей горутине, и хендлеры выполняются конкурентно. Любое общее состояние хендлера — счётчики, кеши, карты — защищайте мьютексом или атомиками; `-race` найдёт забытое.

## Как это в C#

- Kestrel + `WebApplication` ≈ `http.Server` + `ServeMux`, но в Go нет хоста, DI и конвейера: `srv.Serve(ln)` — обычный блокирующий вызов в вашем `main`.
- `app.MapGet("/", (HttpContext ctx) => ...)` ≈ `mux.HandleFunc("GET /", func(w, r) {...})`. Вместо `IResult` и `Results.Created(...)` — явные вызовы: заголовки, `WriteHeader`, `Write`. Возвращаемого значения у хендлера нет.
- `KestrelServerLimits` (`RequestHeadersTimeout` — 30 с, `KeepAliveTimeout` — 130 с, `MaxRequestHeadersTotalSize` — 32 КБ) включены по умолчанию. У `http.Server` все таймауты по умолчанию нулевые — без лимита, и только `MaxHeaderBytes` по умолчанию 1 МБ.
- `Response.HasStarted` и исключение при записи заголовков после старта ответа — в Go вместо исключения изменение молча теряется (или пишется строка в журнал сервера).

## Ловушки

### `http.ListenAndServe` без таймаутов

```go
http.ListenAndServe(":8080", mux) // Server{} без ReadHeaderTimeout, ReadTimeout, WriteTimeout
```

Клиент, который присылает заголовки по байту в минуту (Slowloris), держит соединение и горутину сколько угодно, и тысяча таких клиентов исчерпает ресурсы. Создавайте `&http.Server{...}` и задавайте как минимум `ReadHeaderTimeout` (его требует и линтер gosec), а также `ReadTimeout`/`WriteTimeout` и `IdleTimeout` для keep-alive. `WriteTimeout` ограничивает всё время от конца чтения заголовков до конца ответа — для долгих стримов его снимают точечно через `http.NewResponseController(w).SetWriteDeadline`.

### Заголовок после `WriteHeader`

```go
w.WriteHeader(http.StatusCreated)
w.Header().Set("Location", "/items/1") // не попадёт в ответ
```

Статус и заголовки уходят в момент `WriteHeader` (или первого `Write`). Всё, что задано позже, игнорируется без ошибки — `httptest.ResponseRecorder` в тестах ведёт себя так же. Ставьте заголовки первыми.

### Забытый `return` после `http.Error`

```go
if r.Method != http.MethodPost {
	http.Error(w, "только POST", http.StatusMethodNotAllowed)
}
w.WriteHeader(http.StatusCreated) // superfluous: статус уже 405
fmt.Fprintln(w, "создано")         // а это допишется к телу ошибки
```

`http.Error` только пишет ответ, выполнение хендлера продолжается. Клиент получит 405 с телом `только POST\nсоздано\n`, а побочные эффекты ниже — выполнятся. После `http.Error` всегда `return`.

## Задание

Напишите хендлер счётчика:

```go
type Counter struct{ /* ваши поля */ }

func (c *Counter) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

Нулевое значение `Counter` готово к работе и начинает с 0. Путь запроса не важен — только метод:

- `GET` и `HEAD` — статус 200, тело — текущее значение в десятичной записи и `"\n"`;
- `POST` — увеличивает счётчик на значение параметра запроса `by` (`/?by=5`), без параметра — на 1. Значение `by` разбирается `strconv.Atoi` (поэтому `+5` допустимо) и должно быть от 1 до 1000; иначе (не число, 0, отрицательное, больше 1000, пустое `?by=`) — ответ `http.Error(w, "invalid by", 400)`, счётчик не меняется. При успехе — статус 200 и новое значение в теле, как у `GET`;
- у ответов 200 заголовки `Content-Type: text/plain; charset=utf-8` и `Cache-Control: no-store`;
- `DELETE` — сбрасывает счётчик в 0 и отвечает `204 No Content` без тела;
- любой другой метод — `http.Error(w, "method not allowed", 405)` и заголовок `Allow: GET, HEAD, POST, DELETE`;
- хендлер вызывается из многих горутин одновременно: тесты запускаются с `-race`, а сотня параллельных `POST` должна дать ровно сотню.
