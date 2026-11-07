---
title: Фейки, моки и httptest
summary: Маленькие интерфейсы на стороне потребителя, ручные стабы, шпионы и фейковые часы, httptest.NewRecorder для хендлеров и httptest.NewServer для клиентов.
difficulty: 3
minutes: 30
runner:
  race: true
prerequisites:
  - 07-testing/02-table-driven
  - 07-testing/03-helpers-and-fixtures
  - 03-types/03-interfaces
  - 05-concurrency/18-context
docs:
  - title: "Пакет net/http/httptest"
    url: https://pkg.go.dev/net/http/httptest
  - title: "Go Code Review Comments: Interfaces"
    url: https://go.dev/wiki/CodeReviewComments#interfaces
---

Чтобы код можно было тестировать без сети, базы и настоящего времени, зависимости должны приходить снаружи. В C# для этого есть DI-контейнер, интерфейс на каждый сервис и Moq. В Go хватает двух вещей: неявной реализации интерфейсов и функций как значений.

Правило звучит как «accept interfaces, return structs»: функции и конструкторы **принимают** интерфейс, а **возвращают** конкретный тип. Интерфейс объявляет потребитель, и в нём ровно те методы, которые потребителю нужны — часто один. Тип из другого пакета реализует его неявно, ничего о нём не зная, а фейк в тесте — структура на пять строк. Время — тоже зависимость: передайте функцию `now func() time.Time` (в коде — `time.Now`) или интерфейс `Clock`, и тест сам решит, который сейчас час. Для кода с `time.Sleep`, таймерами и тикерами есть `testing/synctest` из урока о параллельных тестах.

Фейки пишут руками, и у них есть общепринятые названия:

- **стаб** (stub) отдаёт заготовленные ответы: «курс USD — 90»;
- **шпион** (spy) запоминает вызовы, чтобы тест их проверил: «источник вызван один раз»;
- **фейк** (fake) — рабочая облегчённая реализация: хранилище в map вместо базы;
- **мок** (mock) заранее знает ожидаемые вызовы и сам проверяет их. Моки генерируют инструменты: gomock (`go.uber.org/mock`), mockery для testify/mock, moq. В песочнице их нет, и в большинстве случаев ручной фейк на маленький интерфейс читается проще.

Для HTTP в стандартной библиотеке есть `net/http/httptest`:

- `httptest.NewRecorder()` — `http.ResponseWriter`, который запоминает статус, заголовки и тело (`rec.Code`, `rec.Header()`, `rec.Body`, `rec.Result()`). Вместе с `httptest.NewRequest(method, target, body)` он позволяет вызвать хендлер как обычную функцию — без сервера, порта и сети;
- `httptest.NewServer(handler)` поднимает настоящий HTTP-сервер на `127.0.0.1` со свободным портом. Клиентский код получает `srv.URL` и `srv.Client()` и не знает, что это тест. `srv.Close()` останавливает сервер и дожидается активных запросов;
- `httptest.NewTLSServer` — то же по HTTPS с самоподписанным сертификатом; `srv.Client()` ему доверяет. `NewUnstartedServer` позволяет донастроить сервер до запуска.

## Как это в C#

- `Mock<IRateSource>` в Moq и `Substitute.For<>` в NSubstitute создают реализацию через рефлексию и прокси. В Go так не делают: фейк — обычная структура в `_test.go`, а генераторы (gomock, mockery) пишут такой же код заранее.
- В .NET интерфейс обычно лежит рядом с реализацией (`IRateSource` + `RateSource`), и класс явно его реализует. В Go интерфейс объявляет потребитель, а реализация о нём не знает — поэтому интерфейсы маленькие и фейки дешёвые.
- `TimeProvider`/`FakeTimeProvider` из .NET 8 ≈ внедряемая функция `now` или интерфейс `Clock`.
- `WebApplicationFactory<T>` поднимает приложение в памяти, а клиент тестируют подменой `HttpMessageHandler`. В Go хендлер — функция, её вызывают с `NewRecorder`, а для клиента поднимают настоящий loopback-сервер `httptest.NewServer`.

## Ловушки

### Большой интерфейс рядом с реализацией

```go
// package storage
type Store interface { // 15 методов «на всякий случай»
	Get(id string) (Order, error)
	Put(o Order) error
	Delete(id string) error
	// ...
}
```

Так привычно после C#, но каждому фейку придётся реализовать все 15 методов, а пакет `storage` начнёт поддерживать интерфейс, который ему самому не нужен. Пусть `storage` возвращает `*DB`, а потребитель объявит `type orderGetter interface{ Get(id string) (Order, error) }` — `*DB` подойдёт к нему без единой правки.

### `httptest.NewRequest` в клиентском коде

```go
req := httptest.NewRequest(http.MethodGet, srv.URL, nil)
_, err := srv.Client().Do(req)
// http: Request.RequestURI can't be set in client requests
```

`NewRequest` строит **входящий** запрос — такой, каким его видит хендлер (с `RequestURI`, `RemoteAddr` `192.0.2.1:1234`, хостом `example.com`). Для клиента запрос создают обычным `http.NewRequestWithContext`. И не забывайте `defer srv.Close()` (или `t.Cleanup(srv.Close)`): незакрытый сервер оставляет горутины и открытый порт до конца прогона.

### Шпион без синхронизации

```go
type spy struct{ calls int }

func (s *spy) Rate(ctx context.Context, cur string) (float64, error) {
	s.calls++ // гонка, если код вызывает источник из нескольких горутин
	return 90, nil
}
```

Фейк — такой же код, как и боевой: если тестируемый код вызывает его конкурентно (хендлер под `httptest.NewServer`, пул воркеров), счётчик нужно защищать `sync.Mutex` или `atomic.Int64`. Иначе тест под `-race` упадёт из-за фейка, а без `-race` будет врать.

## Задание

Напишите тестопригодный клиент API пользователей и кеш поверх него. Скрытые тесты подставят `httptest.NewServer`, свой источник и фейковые часы.

```go
type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

var ErrNotFound = errors.New("users: пользователь не найден")

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client
func (c *Client) User(ctx context.Context, login string) (User, error)

type UserSource interface {
	User(ctx context.Context, login string) (User, error)
}

func NewCache(src UserSource, ttl time.Duration, now func() time.Time) *Cache
func (c *Cache) User(ctx context.Context, login string) (User, error)
```

Требования к клиенту:

- `NewClient` сохраняет `baseURL` без завершающего `/` (если он есть) и переданный `hc`; при `hc == nil` создаёт **новый** `&http.Client{Timeout: 5 * time.Second}` (не `http.DefaultClient`);
- `User` с пустым `login` возвращает ошибку, не отправляя запрос;
- иначе отправляет с контекстом `ctx` запрос `GET {BaseURL}/users/{login}`, где `login` экранирован `url.PathEscape` (`a/b` → `/users/a%2Fb`), с заголовком `Accept: application/json`, и закрывает тело ответа;
- статус 200 — тело разбирается как JSON в `User`; ошибка разбора — ошибка;
- статус 404 — ошибка, для которой `errors.Is(err, ErrNotFound)`; любой другой статус — ошибка, в тексте которой есть числовой код (`500`, `201`), и это не `ErrNotFound`;
- ошибки транспорта и контекста возвращаются так, чтобы `errors.Is(err, context.Canceled)` / `context.DeadlineExceeded` работали (обёртка `%w` или как есть); при любой ошибке возвращается нулевой `User`.

Требования к кешу:

- `Cache.User` возвращает сохранённого пользователя, если с момента его получения прошло меньше `ttl` по часам `now`; ровно через `ttl` и позже — снова спрашивает `src`. Время записи — момент успешного ответа источника;
- кеш раздельный для каждого `login`; контекст вызова передаётся в `src`;
- ошибки источника (включая `ErrNotFound`) возвращаются как есть вместе с нулевым `User` и не кешируются: следующий вызов снова идёт в `src`;
- `Cache` безопасен для одновременного использования из многих горутин (тесты идут с `-race`); вызывать `src` для разных логинов одновременно разрешено, но не обязательно;
- `*Client` удовлетворяет `UserSource`, поэтому `NewCache(NewClient(url, nil), time.Minute, time.Now)` — рабочая конфигурация.
