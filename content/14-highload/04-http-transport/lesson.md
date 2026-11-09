---
title: http.Client и Transport под нагрузкой
summary: "Один клиент на процесс, пул keep-alive соединений Transport: MaxIdleConnsPerHost (по умолчанию 2!), MaxConnsPerHost, IdleConnTimeout; тело ответа дочитать и закрыть, таймауты клиента и dialer, HTTP/2, httptrace для наблюдения переиспользования; серверные таймауты и MaxHeaderBytes."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 08-web/01-http-client
  - 08-web/02-http-server
  - 09-security/10-dos-limits
  - 14-highload/01-load-testing
docs:
  - title: "Пакет net/http: Transport"
    url: https://pkg.go.dev/net/http#Transport
  - title: "Пакет net/http/httptrace"
    url: https://pkg.go.dev/net/http/httptrace
  - title: "The complete guide to Go net/http timeouts (Cloudflare)"
    url: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
---

В уроке про HTTP-клиент мы создавали `http.Client` с таймаутом и дочитывали тело ответа. Под нагрузкой эти мелочи становятся главными: сервис, который делает тысячу исходящих запросов в секунду, либо держит десяток тёплых соединений, либо открывает тысячу новых TCP- (и TLS-) соединений в секунду. Во втором случае растут задержки (рукопожатие — это лишний сетевой круг, а TLS — ещё один-два), CPU уходит на криптографию, а на машине копятся сокеты в `TIME_WAIT`, пока не кончатся локальные порты.

**Кто за что отвечает.** `http.Client` — тонкая обёртка: таймаут на весь запрос, редиректы, cookies. Соединения живут в `http.Transport`: это **пул keep-alive соединений** по ключу «схема + хост + порт (+ прокси)». Запрос берёт простаивающее соединение из пула или открывает новое; после ответа — когда тело прочитано до EOF и закрыто — соединение возвращается в пул. И клиент, и транспорт безопасны для конкурентного использования, поэтому правило одно: **один `Transport` на процесс** (или на группу похожих бэкендов), созданный при старте. Сам `&http.Client{}` дешёв: без поля `Transport` он использует общий `http.DefaultTransport`. Дорого — новый `Transport` на запрос: у него свой пустой пул.

**Настройки пула** (`http.Transport`):

| Поле | В `DefaultTransport` | В `&http.Transport{}` | Что делает |
| --- | --- | --- | --- |
| `MaxIdleConns` | 100 | 0 — без лимита | Сколько простаивающих соединений всего, на все хосты. |
| `MaxIdleConnsPerHost` | 0 → **2** | 0 → **2** | Сколько простаивающих держать на один хост. Лишние после ответа **закрываются**. |
| `MaxConnsPerHost` | 0 — без лимита | 0 — без лимита | Потолок соединений на хост (активных, простаивающих и открывающихся). Сверх него запрос ждёт свободного соединения. |
| `IdleConnTimeout` | 90 с | 0 — **вечно** | Через сколько простаивающее соединение закрывается. |
| `TLSHandshakeTimeout` | 10 с | 0 — без лимита | Лимит на TLS-рукопожатие. |
| `ResponseHeaderTimeout` | 0 | 0 | Сколько ждать заголовков ответа после отправки запроса. |

Главная ловушка — `MaxIdleConnsPerHost = 2`. Сервис ходит в один бэкенд, держит 50 запросов одновременно: в пике открыто 50 соединений, после пика 48 из них закрываются, а следующий пик открывает их заново. Пример это показывает: каждая волна из 20 параллельных запросов открывает 18 новых соединений, пока `MaxIdleConnsPerHost` не поднят до уровня параллельности. Для бэкенда, в который идёт основной трафик, ставят `MaxIdleConnsPerHost` равным ожидаемой параллельности, а `MaxConnsPerHost` — как предохранитель, чтобы всплеск не открыл десять тысяч соединений и не положил соседа (запросы сверх лимита ждут в очереди — это тот же bulkhead из урока про circuit breaker, только на уровне транспорта). Начинают обычно с `http.DefaultTransport.(*http.Transport).Clone()` — так сохраняются разумные таймауты, прокси из окружения и HTTP/2 — и меняют только нужные поля.

**Тело ответа.** Соединение HTTP/1.1 не может обслужить следующий запрос, пока предыдущий ответ не прочитан до конца. Поэтому:

- **не закрытое** тело — утечка: соединение навсегда занято этим ответом, вместе с ним висят две горутины транспорта (`readLoop` и `writeLoop`), а каждый следующий запрос открывает новое соединение;
- **закрытое, но не дочитанное** тело в Go до 1.26 включительно означало разрыв соединения. В Go 1.27 `Close` сам дочитывает остаток в фоне, но только если весь ответ по `ContentLength` не больше 256 КБ (или длина неизвестна и остаток укладывается в 256 КБ) и дочитывание заняло не дольше 50 мс; делает он это асинхронно — следующий запрос, отправленный сразу, может не застать соединение в пуле. Надёжный способ — дочитать самому до `Close`: `io.Copy(io.Discard, io.LimitReader(resp.Body, limit))`;
- `json.NewDecoder(resp.Body).Decode(&v)` читает ровно одно JSON-значение: перевод строки после него, хвост chunked-ответа или «мусор» остаются непрочитанными. После `Decode` — тоже дочитать.

**Таймауты.** У запроса несколько стадий, и у каждой свой предохранитель:

- `Client.Timeout` — весь запрос: соединение, отправка, заголовки **и чтение тела**. Ошибка — `net.Error` с `Timeout() == true`, и `errors.Is(err, context.DeadlineExceeded)` тоже её находит. Для запросов со стримингом тела (скачивание большого файла) он мешает — там используют контекст и таймауты стадий;
- контекст запроса (`NewRequestWithContext`) — дедлайн, который приходит сверху, от входящего запроса; его отмена обрывает исходящий;
- `net.Dialer{Timeout, KeepAlive}` в `Transport.DialContext` — установка TCP-соединения и TCP keep-alive; `TLSHandshakeTimeout`, `ResponseHeaderTimeout`, `ExpectContinueTimeout` — отдельные стадии.

**HTTP/2.** По TLS транспорт из `DefaultTransport` договаривается о HTTP/2 сам (ALPN). Тогда запросы к хосту мультиплексируются потоками в **одно** соединение, и настройки «соединений на хост» почти теряют смысл. Когда потоки, объявленные сервером, исчерпаны, транспорт открывает ещё одно соединение; чтобы запросы вместо этого ждали свободного потока, есть `HTTP2Config.StrictMaxConcurrentRequests` (Go 1.26). Но свой `Transport` с заданными `DialContext` или `TLSClientConfig` HTTP/2 по умолчанию **выключает** — нужно `ForceAttemptHTTP2: true` (в `Clone()` от `DefaultTransport` он уже стоит). Протоколы с Go 1.24 можно перечислить явно: поле `Protocols` у `Transport` и `Server` (`SetHTTP1`, `SetHTTP2`, `SetUnencryptedHTTP2` для h2c внутри кластера), тонкие настройки — `HTTP2 *http.HTTP2Config`.

**`httptrace`** показывает, что происходит внутри одного запроса: `GetConn`, `GotConn` (`Reused`, `WasIdle`, `IdleTime`), `DNSStart`/`DNSDone`, `ConnectStart`/`ConnectDone`, `TLSHandshakeStart`/`Done`, `WroteRequest`, `GotFirstResponseByte`. Колбэки вешаются на контекст запроса: `httptrace.WithClientTrace(ctx, trace)`. Доля `Reused == false` — отличная метрика для дашборда: когда она растёт под нагрузкой, пул настроен плохо или где-то течёт тело.

**Серверная сторона.** Сервер под нагрузкой должен защищаться от медленных и жадных клиентов (подробно — в уроке о DoS): `ReadHeaderTimeout` (минимум, против Slowloris), `ReadTimeout`/`WriteTimeout` (на всё чтение запроса и на запись ответа), `IdleTimeout` (сколько держать простаивающее keep-alive соединение; если ноль — берётся `ReadTimeout`, если и он ноль — вечно), `MaxHeaderBytes` (по умолчанию 1 МБ; сервер добавляет к лимиту ещё 4 КБ на буфер, сверх — ответ 431). Тело запроса ограничивают `http.MaxBytesReader` в обработчике.

## Как это в C#

- **Socket exhaustion.** `using var client = new HttpClient()` на каждый запрос — классическая ошибка .NET: каждый клиент со своим `SocketsHttpHandler` открывает новое соединение, сокеты копятся в `TIME_WAIT`. Лекарство — один долгоживущий `HttpClient` или `IHttpClientFactory`. В Go то же самое случится с `&http.Transport{}` на каждый запрос, а `&http.Client{}` без транспорта безопасен — он берёт общий `DefaultTransport`.
- **`SocketsHttpHandler` ≈ `http.Transport`.** `MaxConnectionsPerServer` ≈ `MaxConnsPerHost` (в .NET по умолчанию без лимита). `PooledConnectionIdleTimeout` ≈ `IdleConnTimeout`. `PooledConnectionLifetime` (чтобы долгоживущий клиент замечал смену DNS) прямого аналога не имеет: в Go периодически вызывают `CloseIdleConnections()` или полагаются на `IdleConnTimeout`. Отдельного «лимита простаивающих на хост» со значением 2 в .NET нет — эта ловушка чисто гошная.
- **Тело ответа.** В .NET `HttpClient` по умолчанию буферизует тело целиком (`HttpCompletionOption.ResponseContentRead`), и вопрос «дочитал ли я» не стоит. В Go тело — всегда поток из соединения, как `ResponseHeadersRead` в .NET: читать и закрывать его — работа вызывающего. Дочитывание при закрытии в .NET тоже есть и настраивается: `SocketsHttpHandler.MaxResponseDrainSize` (по умолчанию 1 МБ) и `ResponseDrainTimeout` (2 с) — у Go те же пределы зашиты константами (256 КБ и 50 мс).
- **Таймауты.** `HttpClient.Timeout` (100 с по умолчанию) ≈ `Client.Timeout` (по умолчанию нет вовсе); `ConnectTimeout` у `SocketsHttpHandler` ≈ `net.Dialer.Timeout`. Наблюдение за соединениями в .NET — встроенные метрики `System.Net.Http` (`http.client.open_connections`, `http.client.request.time_in_queue` в .NET 8+, видны в `dotnet-counters`), в Go — `httptrace` и свои метрики.

## Ловушки

### Новый Transport на каждый запрос

```go
func fetch(url string) (*http.Response, error) {
	c := &http.Client{
		Transport: &http.Transport{}, // свой пустой пул на каждый вызов
		Timeout:   5 * time.Second,
	}
	return c.Get(url)
}
```

Каждый вызов открывает новое соединение, а после ответа оно остаётся простаивать в пуле транспорта, который больше никто не использует: `IdleConnTimeout` у `&http.Transport{}` — ноль, то есть вечно. Течёт по соединению и две горутины на запрос. Транспорт создают один раз при старте и передают туда, где он нужен.

### `MaxIdleConnsPerHost` по умолчанию

```go
tr := &http.Transport{MaxConnsPerHost: 100} // а простаивающих — по-прежнему 2
```

Лимит соединений подняли, лимит простаивающих — нет: под нагрузкой в 100 параллельных запросов после каждого ответа соединения закрываются сверх двух и тут же открываются заново. В метриках это выглядит как рост задержек и CPU на TLS при «нормальном» числе соединений. Ставьте `MaxIdleConnsPerHost` не меньше типичной параллельности к хосту.

### Тело не дочитано после ошибки или `Decode`

```go
resp, err := c.Do(req)
if err != nil {
	return err
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusOK {
	return fmt.Errorf("status %d", resp.StatusCode) // тело ошибки не прочитано
}
return json.NewDecoder(resp.Body).Decode(&v) // хвост после JSON не прочитан
```

`Close` есть, но до EOF никто не дочитал. Пока ответ мал (до 256 КБ), Go 1.27 дочитает остаток сам в фоне, а большая страница ошибки от прокси или длинный хвост — и соединение разорвано. Перед `Close` дочитывайте с лимитом: `io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))` — удобно в `defer`.

## Задание

Напишите клиент для внутреннего JSON-API, который гарантированно переиспользует соединения.

```go
const MaxBody = 1 << 20

var ErrTooLarge = errors.New("response body too large")

type StatusError struct{ Code int }

func (e *StatusError) Error() string

func NewClient(maxConnsPerHost int, timeout time.Duration) *http.Client
func GetJSON(ctx context.Context, c *http.Client, url string, dst any) error
```

- `(*StatusError).Error()` возвращает `fmt.Sprintf("unexpected status %d", e.Code)`.
- `NewClient` возвращает клиент со своим `*http.Transport` (не `http.DefaultTransport` и не общий между вызовами `NewClient`). `maxConnsPerHost < 1` считается как 1. У транспорта `MaxConnsPerHost` и `MaxIdleConnsPerHost` равны `maxConnsPerHost`, `IdleConnTimeout` и `TLSHandshakeTimeout` положительны, `DialContext` задан. `Client.Timeout` равен `timeout`, а при `timeout <= 0` — 30 секунд.
- `GetJSON` делает `GET url` с контекстом `ctx` через клиент `c`. Ошибку `Do` (сеть, таймаут, отмена) возвращает так, чтобы `errors.Is` находил причину (`context.DeadlineExceeded`, `context.Canceled`). Тело ответа закрывается всегда.
- Статус вне 200–299 — ошибка `*StatusError` с кодом ответа (находится через `errors.As`); тело при этом читать не нужно, но перед закрытием его остаток дочитывается (не больше `MaxBody` байт).
- Если `resp.ContentLength > MaxBody` — ошибка `ErrTooLarge` (через `errors.Is`); тело закрывается без чтения. Эта проверка идёт первой, до статуса: ответ 500 с `ContentLength > MaxBody` — тоже `ErrTooLarge` (тесты это сочетание не проверяют, так что допустим и обратный порядок).
- Иначе из тела, ограниченного `MaxBody` байтами, декодируется одно JSON-значение в `dst` (лимит — на всё, что прочитает декодер, включая его упреждающее чтение в буфер). Ошибка декодирования возвращается (не `*StatusError`). Данные после JSON-значения игнорируются. После декодирования — удачного или нет — остаток тела дочитывается (ещё не больше `MaxBody` байт: лимит дочитывания отдельный от лимита декодера) и тело закрывается.
- Итог: последовательные вызовы `GetJSON` одним клиентом к одному серверу используют одно соединение, в том числе когда сервер отвечает ошибками или присылает после JSON сотни килобайт хвоста (но не больше `MaxBody`).
