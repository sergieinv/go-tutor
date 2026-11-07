---
title: HTTP-клиент
summary: http.Client с таймаутом вместо DefaultClient, запрос с контекстом и заголовками, закрытие и дочитывание тела, статус не-2xx как обычный ответ, лимит размера и пул соединений Transport.
difficulty: 3
minutes: 30
prerequisites:
  - 05-concurrency/18-context
  - 06-stdlib/05-json
  - 04-errors/03-custom-errors
docs:
  - title: "Пакет net/http: Client"
    url: https://pkg.go.dev/net/http#Client
  - title: "Пакет net/http/httptest"
    url: https://pkg.go.dev/net/http/httptest
  - title: "The complete guide to Go net/http timeouts (Cloudflare)"
    url: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
---

HTTP-клиент в Go — тип `http.Client` из стандартной библиотеки. Короткие функции `http.Get`, `http.Post` используют глобальный `http.DefaultClient`, а у него **нет таймаута**: если сервер принял соединение и замолчал, вызов будет ждать вечно. В рабочем коде создают свой клиент с `Timeout` (лимит на весь запрос, включая чтение тела) и передают контекст запроса через `http.NewRequestWithContext` — тогда запрос отменяется вместе с обработкой, которая его породила.

`client.Do(req)` возвращает ошибку, только если ответа нет: сбой сети, таймаут, отмена контекста, слишком много редиректов (больше 10). Ответ `404` или `500` — это успешный HTTP-обмен: `err == nil`, а статус лежит в `resp.StatusCode`. Тело `resp.Body` — поток из сетевого соединения: его нужно закрыть, а чтобы соединение вернулось в пул и пригодилось следующему запросу, — ещё и дочитать до конца. Размеру ответа доверять нельзя: читайте через `io.LimitReader`.

Соединения хранит `http.Transport` — пул keep-alive соединений, общий для всех запросов клиента. Клиент и транспорт безопасны для конкурентного использования, поэтому создают их один раз и переиспользуют. По умолчанию транспорт держит до 100 простаивающих соединений, но только 2 на один хост (`MaxIdleConnsPerHost`), — для сервиса, который активно ходит в один бэкенд, это значение обычно увеличивают.

## Как это в C#

- `HttpClient` ≈ `http.Client`, `SocketsHttpHandler` ≈ `http.Transport`. Правило то же: клиент живёт долго, а не создаётся на каждый запрос (в .NET для этого есть `IHttpClientFactory`, в Go достаточно одной переменной).
- `HttpClient.Timeout` по умолчанию — 100 секунд, а в Go у `DefaultClient` таймаута нет вовсе. При таймауте .NET бросает `TaskCanceledException`, Go возвращает `*url.Error`, из которого `errors.Is` достаёт `context.DeadlineExceeded`.
- `EnsureSuccessStatusCode()` в Go нет: проверка `resp.StatusCode` — ваша работа. Удобно завести свой тип ошибки со статусом и проверять его через `errors.As`.
- `CancellationToken` в `SendAsync` → контекст в `NewRequestWithContext`. `using var response` → `defer resp.Body.Close()`; дочитывать тело в .NET не нужно, в Go 1.25 — нужно для переиспользования соединения.

## Ловушки

### `http.Get` без таймаута

```go
resp, err := http.Get(url) // DefaultClient: Timeout == 0, ждать можно вечно
```

Зависший бэкенд держит горутину, соединение и всё, что выше по стеку. Создайте клиент `&http.Client{Timeout: 10 * time.Second}` или передайте контекст с дедлайном через `NewRequestWithContext` — лучше и то и другое.

### Статус не проверен, тело не закрыто

```go
resp, err := client.Get(url)
if err != nil {
	return err
}
json.NewDecoder(resp.Body).Decode(&v) // 500 с HTML — ошибка разбора вместо понятной
```

`err == nil` при любом статусе. Проверяйте `resp.StatusCode`, а `defer resp.Body.Close()` пишите сразу после проверки `err`. Незакрытое тело — утечка соединения и горутины транспорта. Закрытое, но не дочитанное тело в Go 1.25 означает, что соединение будет разорвано, а не возвращено в пул: при сотнях запросов в секунду это тысячи лишних TCP- и TLS-рукопожатий. Перед `Close` дочитайте остаток: `io.Copy(io.Discard, resp.Body)` (лучше — с лимитом). Go 1.27 научился сам дочитывать небольшой остаток при `Close`, но рассчитывать на это в 1.25 нельзя.

### Неограниченное чтение

```go
body, _ := io.ReadAll(resp.Body) // 10 ГБ от сломанного сервера — 10 ГБ в памяти
```

Оборачивайте тело в `io.LimitReader(resp.Body, max+1)`: если прочитано больше `max` байт, ответ слишком велик. Ровно `max` без «+1» не отличить от обрезанного ответа. `Content-Length` — подсказка, а не гарантия: его может не быть (chunked), а сервер может прислать больше.

## Задание

Напишите функцию, которая получает JSON по HTTP:

```go
var (
	ErrTooLarge = errors.New("getjson: response too large")
	ErrNotJSON  = errors.New("getjson: response is not JSON")
)

// StatusError — ответ со статусом вне диапазона 200–299.
type StatusError struct{ StatusCode int }

func GetJSON(ctx context.Context, client *http.Client, url string, maxBytes int64, v any) error
```

Типы и переменные ошибок уже объявлены в шаблоне. Требования:

- если `client == nil`, используется новый клиент с `Timeout: 10 * time.Second` (не `http.DefaultClient`);
- запрос — `GET` с контекстом `ctx` и заголовком `Accept: application/json`; ошибки создания и отправки запроса возвращаются обёрнутыми (`%w`), так что `errors.Is(err, context.DeadlineExceeded)` и `errors.Is(err, context.Canceled)` работают;
- проверки ответа идут по порядку, и первая же неудача возвращает ошибку:
  1. статус вне `200–299` — ошибка `*StatusError` с этим статусом (через `errors.As`);
  2. медиатип из `Content-Type` не `application/json` (регистр не важен, параметры вроде `; charset=utf-8` допустимы; заголовка нет или он не разбирается — тоже ошибка) — `ErrNotJSON`;
  3. тело длиннее `maxBytes` байт — `ErrTooLarge` (ровно `maxBytes` — допустимо);
  4. тело — не ровно одно JSON-значение, подходящее к `v` (пробелы вокруг допустимы), — ошибка `json.Unmarshal`, обёрнутая через `%w`;
- при ошибках шагов 1–3 `v` не изменяется;
- тело ответа всегда закрывается, а если оно не длиннее `maxBytes`, перед закрытием дочитывается до конца — и при ошибке статуса или `Content-Type` тоже, чтобы соединение вернулось в пул.
