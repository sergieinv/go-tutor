---
title: Повторы и backoff
summary: "Что повторять (идемпотентность, коды Unavailable, ResourceExhausted, Aborted), экспоненциальная пауза с полным джиттером, дедлайн важнее числа попыток, RetryInfo от сервера, бюджет повторов и шторм повторов в цепочке сервисов."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 10-services-db/07-locking
  - 11-grpc/08-status-errors
  - 11-grpc/13-grpc-client
  - 05-concurrency/18-context
docs:
  - title: "gRPC: Retry (retry policy и throttling)"
    url: https://grpc.io/docs/guides/retry/
  - title: "AWS Architecture Blog: Exponential Backoff And Jitter"
    url: https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
  - title: "Google SRE Book: Handling Overload (retry budget)"
    url: https://sre.google/sre-book/handling-overload/
---

В уроке о блокировках повтор уже встречался: конфликт версии в базе, цикл с ограничением попыток и паузой с джиттером. Между сервисами задача та же, но цена ошибки выше. Повтор маскирует короткий сбой соседа: перезапуск пода, обрыв соединения, секундную перегрузку. Но тот же повтор, написанный неаккуратно, удваивает заказы, держит запрос пользователя дольше его дедлайна и добивает соседа, которому и так плохо. Хороший повтор отвечает на четыре вопроса: **можно ли** повторять эту операцию, **какие** ошибки, **сколько ждать** между попытками и **когда остановиться**.

**Можно ли.** Повтор безопасен, если операция идемпотентна: `GetOrder`, `Reserve` с ключом резерва, `UPDATE ... SET status = 'paid' WHERE id = $1`. `CreateOrder` без ключа идемпотентности повторять нельзя: ответ `Unavailable` или `DeadlineExceeded` не значит, что сервер ничего не сделал. Возможно, заказ создан, а потерялся ответ. Лечится это не отказом от повторов, а ключом: клиент генерирует `idempotency-key`, передаёт его в metadata, а сервер по нему возвращает уже созданный заказ. Так устроена задача итогового урока модуля 11.

**Какие ошибки.** Повторять стоит только то, что может пройти со второго раза:

| Код | Повторять? | Почему |
| --- | --- | --- |
| `Unavailable` | да | сосед недоступен или соединение порвалось — канонический «временный» код |
| `ResourceExhausted` | да, с паузой | перегрузка или лимит; часто приходит с подсказкой, когда повторить |
| `Aborted` | да, всю операцию | конфликт параллельных изменений, как `40001` в базе |
| `DeadlineExceeded` | обычно нет | если истёк *ваш* контекст, повторять уже некогда; если таймаут соседа короче вашего — только идемпотентное |
| `InvalidArgument`, `NotFound`, `AlreadyExists`, `PermissionDenied`, `Unauthenticated`, `FailedPrecondition` | нет | ответ не изменится, повтор только задержит ошибку |
| `Internal`, `Unknown` | осторожно | обычно баг на той стороне; повтор возможен для идемпотентных операций, но не в цикле |

`status.Code(err)` разворачивает обёрнутые через `%w` ошибки и возвращает `codes.Unknown` для ошибок не из gRPC — в том числе для «голых» `context.Canceled` и `context.DeadlineExceeded` вашего собственного контекста. Поэтому классификация по коду сама по себе не повторит отмену. Если же контекст истёк или отменён во время RPC, grpc-go сам вернёт статус `DeadlineExceeded` или `Canceled`, и `status.Code` даст его.

**Сколько ждать.** Экспонента с потолком и **полным джиттером**: пауза перед попыткой `n` случайна в `[0, min(base·2^(n−1), max))`. Экспонента даёт соседу время подняться, случайность разводит клиентов по времени: без неё тысяча клиентов, получивших `Unavailable` в одну секунду, вернутся тоже в одну секунду. Тот же расчёт делает встроенный в gRPC повтор из service config (`initialBackoff`, `backoffMultiplier`, `maxBackoff`; урок о клиенте gRPC). Если сервер сам сказал, когда приходить, — его подсказка важнее своей экспоненты. В gRPC-ошибке она приезжает деталью `google.rpc.RetryInfo` с полем `retry_delay` (её кладут через `status.WithDetails`, урок о статусах), а встроенный повтор grpc-go читает свою подсказку — трейлер `grpc-retry-pushback-ms`.

**Когда остановиться.** Число попыток — грубый предохранитель. Главный ограничитель — **дедлайн**: у запроса пользователя есть бюджет времени, и повтор, который начнётся после дедлайна, никому не нужен. Перед паузой сравните `now + пауза` с `ctx.Deadline()`: если пауза переживёт дедлайн, вернуть ошибку сразу честнее, чем ждать секунду и всё равно получить `DeadlineExceeded`. Пауза — только через `select` с `ctx.Done()`, а не `time.Sleep`, иначе отмена клиента её не прервёт.

**Шторм повторов.** Пусть цепочка gateway → orders → warehouse и каждый слой повторяет до трёх раз. Склад лёг — и каждый запрос пользователя превращается в 3·3·3 = 27 запросов к складу. Нагрузка на упавший сервис выросла в 27 раз ровно тогда, когда он пытается подняться. Против этого два приёма:

- **Повторяет один слой.** Обычно тот, что непосредственно над отказавшим. Остальные получают ошибку как есть; если повторы исчерпаны из-за перегрузки, вверх уходит код, который выше не повторяют (например, `Unavailable` превращают в `ResourceExhausted` без `RetryInfo` или в ошибку с флагом «не повторять» — договорённость внутри системы).
- **Бюджет повторов.** Клиент разрешает себе повторы, только пока неудач немного. В gRPC это `retryThrottling` в service config: ведро на `maxTokens` токенов, каждая повторяемая неудача снимает токен, каждый успех добавляет `tokenRatio`, а повторы разрешены, пока токенов больше половины ведра. Когда сосед лежит, ведро быстро пустеет, и клиент делает по одной попытке на запрос: нагрузка остаётся почти исходной. Вариант из книги Google SRE — повторы не больше 10% от числа запросов.

Пример урока показывает всё это на gRPC-сервере склада поверх `bufconn`: повтор после сбоев сети, пауза из `RetryInfo`, отказ без повтора на `InvalidArgument`, остановку по дедлайну и подсчёт шторма с бюджетом и без. Часы в примере фейковые: `sleep` не ждёт, а сдвигает «сейчас», поэтому пример работает мгновенно, а вывод стабилен. Тот же приём — внедрённые `Now` и `Sleep` — делает повторы тестируемыми в задании.

## Как это в C#

- **Polly вместо цикла.** В .NET повторы почти всегда делает Polly v8: `new ResiliencePipelineBuilder().AddRetry(new RetryStrategyOptions { ShouldHandle = ..., BackoffType = DelayBackoffType.Exponential, UseJitter = true, MaxRetryAttempts = 4, Delay = TimeSpan.FromMilliseconds(100) })`. Для `HttpClient` есть готовый набор `Microsoft.Extensions.Http.Resilience` — `AddStandardResilienceHandler()`: общий таймаут (30 с), ограничитель параллельности (1000 запросов), повтор (3 раза от 2 с, экспоненциально с джиттером), circuit breaker и таймаут попытки (10 с). В Go стандартной библиотеки повторов нет, и обычно её не берут: цикл на два десятка строк виден целиком и тестируется внедрёнными часами.
- **Что повторять.** `ShouldHandle` в Polly — тот же предикат `retryable`: `new PredicateBuilder().Handle<RpcException>(e => e.StatusCode is StatusCode.Unavailable or StatusCode.ResourceExhausted)`. Подсказку сервера Polly берёт через `DelayGenerator` (например, из заголовка `Retry-After`) — у нас `RetryInfo`.
- **Встроенный повтор gRPC.** `Grpc.Net.Client` поддерживает тот же service config, что и grpc-go: `new GrpcChannelOptions { ServiceConfig = new ServiceConfig { MethodConfigs = { new MethodConfig { RetryPolicy = new RetryPolicy { ... } } } } }`, с `RetryThrottling` для бюджета. Ограничения те же: повтор прозрачен для кода, но не знает про идемпотентность и вашу бизнес-логику.
- **Отмена.** `Task.Delay(delay, ct)` прерывается токеном — в Go это `select` по таймеру и `ctx.Done()`; `time.Sleep` отмену не видит.

## Под капотом

Сценарий соединяет повторы с circuit breaker из следующего урока. Запрос A получает три `Unavailable` подряд с растущими паузами, и breaker размыкается: четвёртая попытка отказывает мгновенно, без сети. Запрос B, пока breaker открыт, тоже отказывает сразу, а через секунду пробный запрос C замыкает его обратно. Запрос D с коротким дедлайном показывает отказ по дедлайну: пауза не укладывается в остаток времени, и ошибка возвращается сразу. Джиттер в сценарии опущен, чтобы шаги были воспроизводимы.

## Ловушки

### Повтор неидемпотентного вызова

```go
for attempt := 1; attempt <= 3; attempt++ {
	_, err = orders.CreateOrder(ctx, req) // ответ потерялся — заказ уже создан
	if status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded {
		break
	}
}
```

Сервер мог создать заказ и не успеть ответить: обрыв соединения после записи в базу, таймаут на обратном пути. Повтор создаст второй заказ. Передавайте ключ идемпотентности, один на все попытки:

```go
ctx = metadata.AppendToOutgoingContext(ctx, "idempotency-key", key) // key создан до цикла
```

Сервер хранит ключ вместе с результатом и на повтор отдаёт сохранённый ответ.

### Повторы на каждом слое

```go
// orders
err := retry(ctx, func(ctx context.Context) error { return warehouse.Reserve(ctx, r) })
// gateway
err := retry(ctx, func(ctx context.Context) error { return orders.Create(ctx, o) })
```

Каждый слой честно повторяет до трёх раз, а вместе они умножают нагрузку на упавший склад: 3 × 3 = 9 запросов на один заказ, с третьим слоем — 27. Повторяйте в одном месте — над тем, кто отказал, — и наверх отдавайте ошибку, которую не повторяют. Бюджет повторов на каждом клиенте страхует от ошибок в этой договорённости.

### Пауза, которая не видит отмену

```go
time.Sleep(backoff(attempt)) // клиент ушёл, а мы спим ещё 800 мс и идём на новую попытку
```

`time.Sleep` не знает о контексте. Ждите через таймер и `select`:

```go
t := time.NewTimer(d)
defer t.Stop()
select {
case <-t.C:
case <-ctx.Done():
	return ctx.Err()
}
```

## Задание

Напишите политику повторов для вызовов gRPC с внедрёнными часами, бюджетом и уважением к `RetryInfo`.

```go
func Retryable(err error) bool
func RetryDelay(err error) (time.Duration, bool)

type Budget struct{ /* ... */ }

func NewBudget(maxTokens, ratio float64) *Budget
func (b *Budget) OnSuccess()
func (b *Budget) OnFailure()
func (b *Budget) AllowRetry() bool
func (b *Budget) Tokens() float64

type Policy struct {
	MaxAttempts int
	Base, Max   time.Duration
	Rand        func() float64
	Now         func() time.Time
	Sleep       func(ctx context.Context, d time.Duration) error
	Budget      *Budget
}

func (p Policy) Backoff(attempt int) time.Duration
func (p Policy) Do(ctx context.Context, fn func(ctx context.Context) error) error
```

- `Retryable` — `true`, только если код gRPC ошибки (`status.Code`, в том числе у ошибки, обёрнутой через `%w`) — `Unavailable`, `ResourceExhausted` или `Aborted`. Для `nil`, ошибок не из gRPC и всех остальных кодов — `false`.
- `RetryDelay` — пауза из детали `*errdetails.RetryInfo` в статусе ошибки (тоже через `%w`): `(delay, true)`, если такая деталь есть, поле `RetryDelay` задано, корректно (`IsValid`) и не отрицательно. Иначе — `(0, false)`, в том числе для `nil`.
- `Budget` — бюджет повторов, безопасный для одновременного использования из многих горутин. `NewBudget` создаёт полное ведро: токенов `maxTokens` (считайте `maxTokens > 0`, `ratio ≥ 0`). `OnFailure` снимает 1 токен, но не опускает ниже 0; `OnSuccess` добавляет `ratio`, но не выше `maxTokens`. `AllowRetry` — `true`, пока токенов строго больше `maxTokens/2`. `Tokens` возвращает текущее число токенов.
- `Backoff(k)` — полный джиттер: для `k < 1` считать `k = 1`; предел — `min(Base·2^(k−1), Max)`, результат — `Rand()` × предел, где `Rand()` возвращает число из `[0, 1)`. Считайте `Base > 0` и `Max ≥ Base`. На любых `k`, включая огромные, результат не отрицателен и меньше `Max`.
- `Do` вызывает `fn(ctx)` не больше `MaxAttempts` раз (`MaxAttempts < 1` — один раз):
  - перед каждой попыткой: если `ctx` отменён — вернуть `ctx.Err()`, не вызывая `fn`;
  - `fn` вернула `nil` — `Budget.OnSuccess()` и вернуть `nil`;
  - ошибка не `Retryable` — вернуть её сразу, бюджет не трогать;
  - ошибка `Retryable` — `Budget.OnFailure()`; дальше вернуть эту ошибку без паузы, если попытка была последней или `Budget.AllowRetry()` — `false`;
  - пауза — `RetryDelay(err)`, если подсказка есть (как есть, даже больше `Max`), иначе `Backoff(k)`, где `k` — номер неудавшейся попытки (1, 2, …);
  - если у `ctx` есть дедлайн и `Now()` плюс пауза позже него — вернуть ошибку сразу, без паузы;
  - пауза — `Sleep(ctx, d)`; если `Sleep` вернул ошибку — вернуть её как есть.
  - Ошибки `fn` возвращаются как есть (тот же объект, не обёрнутые). `Budget == nil` — без бюджета. `Now == nil` — `time.Now`, `Rand == nil` — `rand.Float64` из `math/rand/v2`, `Sleep == nil` — настоящее ожидание `d`, прерываемое отменой `ctx` с его ошибкой.
