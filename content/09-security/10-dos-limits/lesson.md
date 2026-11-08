---
title: Лимиты против DoS
summary: Таймауты сервера и Slowloris, MaxBytesReader и MaxHeaderBytes, gzip-бомба и лимит на распакованный размер, почему в Go нет ReDoS и что всё равно ограничивать, лимит одновременных запросов и 503/429.
difficulty: 4
minutes: 35
prerequisites:
  - 08-web/02-http-server
  - 08-web/04-middleware
  - 06-stdlib/04-regexp
  - 05-concurrency/15-rate-limiting
  - 09-security/02-input-validation
docs:
  - title: "Пакет net/http: Server, MaxBytesReader"
    url: https://pkg.go.dev/net/http#Server
  - title: "Пакет regexp: гарантия линейного времени"
    url: https://pkg.go.dev/regexp
  - title: "Пакет golang.org/x/sync/semaphore"
    url: https://pkg.go.dev/golang.org/x/sync/semaphore
runner:
  race: true
---

Отказ в обслуживании — это исчерпание ресурса: горутин и сокетов, памяти, CPU, соединений с базой. Атакующему не нужен ботнет, если сервер сам соглашается ждать сколько угодно и читать сколько пришлют. Kestrel из коробки ставит таймаут на заголовки, лимит тела в 30 МБ и лимит размера заголовков. `http.Server` по умолчанию не ставит ни одного таймаута, тело не ограничивает, а заголовки ограничивает только мегабайтом (`DefaultMaxHeaderBytes`). Каждый лимит — ваша строка кода.

Лимиты ставят по слоям. **Соединение**: `ReadHeaderTimeout` закрывает соединение медленного клиента (атака Slowloris — заголовки по байту в минуту), `ReadTimeout` и `WriteTimeout` ограничивают чтение запроса и запись ответа, `IdleTimeout` — простой keep-alive между запросами, `MaxHeaderBytes` — размер заголовков (сверх него сервер отвечает `431`). **Тело**: `http.MaxBytesReader` — чтение сверх лимита возвращает `*http.MaxBytesError`, и сервер закроет соединение после ответа. Сжатое тело ограничивают **дважды**: сжатый размер и распакованный, — 10 МиБ нулей gzip сжимает примерно в 1000 раз. **Обработка**: семафор на число одновременных запросов; когда мест нет, честнее сразу ответить `503 Service Unavailable` с `Retry-After`, чем копить очередь, которая съест память. `429 Too Many Requests` — ответ на превышение квоты конкретного клиента (rate limiting из урока 05-concurrency/15).

Отдельно о регулярных выражениях. ReDoS — катастрофический возврат в движках с backtracking: шаблон вроде `^(a+)+$` на строке из нескольких десятков `a` и `!` перебирает экспоненциальное число разбиений, и каждая лишняя `a` удваивает время. Классический пример — `Regex` в .NET без `matchTimeout`. Пакет `regexp` в Go построен на RE2 и гарантирует время, линейное от длины входа; обратных ссылок и lookaround в нём нет именно поэтому. Но «линейное» — не «бесплатное»: время пропорционально длине входа, умноженной на размер шаблона. Длину входа ограничивайте всё равно, а шаблоны от пользователя — по длине до компиляции (`regexp` сам отвергает повторы больше 1000, вроде `a{1001}`, и слишком большие программы).

## Как это в C#

- `KestrelServerLimits` — `RequestHeadersTimeout` (30 с), `MaxRequestBodySize` (около 30 МБ), `MaxRequestHeadersTotalSize` (32 КБ) — включены по умолчанию. В Go — поля `http.Server` с нулевыми значениями и `http.MaxBytesReader` в каждом хендлере или middleware. Аналога `MaxConcurrentConnections` (в Kestrel он по умолчанию тоже выключен) в `net/http` нет: число соединений ограничивают обёрткой над `net.Listener`, а число обработок — семафором.
- `[RequestSizeLimit]` на action ≈ `http.MaxBytesReader` в конкретном хендлере, `[DisableRequestSizeLimit]` — его отсутствие, только в Go отсутствие — по умолчанию.
- `AddRateLimiter` с `ConcurrencyLimiter` и `RejectionStatusCode = 503` — в Go это middleware из десятка строк на буферизованном канале или `semaphore.Weighted.TryAcquire` из `golang.org/x/sync`.
- `RequestDecompression` middleware в ASP.NET Core сам ограничивает распакованное тело тем же `MaxRequestBodySize`. В Go сервер `Content-Encoding` запроса не трогает: распаковываете вы — и лимит ставите тоже вы.
- `Regex` с `RegexOptions.None` — backtracking, против ReDoS нужен `matchTimeout` или `RegexOptions.NonBacktracking` (.NET 7+). Go-`regexp` всегда работает как `NonBacktracking`.

## Ловушки

### `io.LimitReader(r, max)` молча обрезает

```go
// УЯЗВИМО: 10 МиБ превращаются в «валидный» 1 МиБ — и обрезанные
// данные уходят дальше, будто так и было.
data, err := io.ReadAll(io.LimitReader(zr, 1<<20)) // err == nil
```

`LimitReader` после лимита возвращает `io.EOF`, а не ошибку. Читайте `max+1` и проверяйте длину (`len(data) > max` — ошибка) или оберните распаковщик в `http.MaxBytesReader(w, zr, max)`: он сам вернёт `*http.MaxBytesError`, а ровно `max` байт ещё пропустит.

### `WriteTimeout` убивает долгие ответы

```go
srv := &http.Server{WriteTimeout: 10 * time.Second}
// хендлер выгрузки большого отчёта или SSE-поток через 10 с оборвётся
```

`WriteTimeout` отсчитывается от конца чтения заголовков запроса до конца записи ответа. Не отключайте его глобально ради одного хендлера: продлите дедлайн точечно — `http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Hour))` (то же для загрузки: `SetReadDeadline`).

### HTTP-клиент тоже распаковывает

```go
resp, err := client.Get(partnerURL)
// УЯЗВИМО: Transport сам распаковал gzip-ответ — ReadAll прочитает
// гигабайты, если партнёр (или тот, кто его взломал) их пришлёт.
body, err := io.ReadAll(resp.Body)
```

Если `Transport` сам добавил `Accept-Encoding: gzip`, он прозрачно распаковывает ответ. Ограничивайте тело ответа так же, как тело запроса: `io.ReadAll(io.LimitReader(resp.Body, max+1))` с проверкой длины.

## Задание

Напишите middleware с лимитами и конструктор сервера.

```go
type Limits struct {
	HeaderTimeout   time.Duration // ReadHeaderTimeout
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
	MaxBodyBytes    int64 // тело запроса как пришло по сети
	MaxDecompressed int64 // тело после распаковки gzip
	MaxInFlight     int   // одновременных запросов в next
}

func Protect(next http.Handler, l Limits) http.Handler
func NewServer(h http.Handler, l Limits) *http.Server
```

Все поля `Limits` в тестах положительны. `Protect` обрабатывает каждый запрос так:

1. **Параллелизм.** Если `next` уже обрабатывает `MaxInFlight` запросов, ответ — `503` с заголовком `Retry-After: 1`, сразу, без ожидания; `next` не вызывается. Место освобождается, когда запрос завершился — в том числе отказом на шагах 2–3.
2. **Тело.** Чтение `r.Body` больше `MaxBodyBytes` байт возвращает ошибку `*http.MaxBytesError` (проверяется через `errors.As`); ровно `MaxBodyBytes` — без ошибки.
3. **Распаковка** по заголовку `Content-Encoding`:
   - нет заголовка — тело передаётся как есть;
   - `gzip` — `next` получает распакованное тело, заголовок `Content-Encoding` удалён, `r.ContentLength == -1`. Чтение распакованного тела больше `MaxDecompressed` байт возвращает ошибку `*http.MaxBytesError`, при этом `next` успевает прочитать не больше `MaxDecompressed` байт; ровно `MaxDecompressed` — без ошибки. Лимит `MaxBodyBytes` действует на сжатые байты. Если тело не начинается с корректного заголовка gzip — ответ `400`, `next` не вызывается;
   - любое другое значение, в том числе `GZIP`, список вроде `gzip, identity` и пустая строка, — `415`, `next` не вызывается.

`NewServer` возвращает `*http.Server`, у которого `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` и `MaxHeaderBytes` взяты из `l`, а `Handler` — `Protect(h, l)`. Адрес не задаётся: тесты запускают сервер через `srv.Serve(ln)` на `127.0.0.1` и атакуют его медленным клиентом и огромными заголовками.
