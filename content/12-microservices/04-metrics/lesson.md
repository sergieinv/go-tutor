---
title: Метрики Prometheus
summary: "client_golang: counter, gauge, histogram (бакеты, _sum и _count), метки и кардинальность, RED для HTTP и gRPC, свой реестр вместо глобального, promhttp и /metrics, проверка через testutil; OTel metrics — обзорно."
difficulty: 4
minutes: 35
runner:
  race: true
prerequisites:
  - 12-microservices/02-structured-logging
  - 08-web/04-middleware
  - 11-grpc/09-interceptors
docs:
  - title: "Prometheus: типы метрик"
    url: https://prometheus.io/docs/concepts/metric_types/
  - title: "Prometheus: практика именования и меток"
    url: https://prometheus.io/docs/practices/naming/
  - title: "client_golang/prometheus"
    url: https://pkg.go.dev/github.com/prometheus/client_golang/prometheus
---

Метрика — число, которое сервис держит в памяти и отдаёт по HTTP на `/metrics` в текстовом формате. Prometheus сам приходит за ним раз в 15–30 секунд (pull-модель), складывает точки во временной ряд и считает по ним графики и алерты. Логи и трассы рассказывают про отдельные запросы, метрики — про все запросы сразу: «сколько в секунду», «какая доля с ошибкой», «каков 99-й перцентиль». Они дешёвые: инкремент счётчика — атомарная операция, а объём данных не зависит от числа запросов.

**Типы.** **Counter** только растёт (запросы, ошибки, созданные заказы); его значение само по себе неинтересно, смотрят на скорость: `rate(http_requests_total[5m])`. При рестарте процесса он обнуляется, и `rate` это понимает. **Gauge** ходит вверх и вниз: запросы в обработке, размер очереди, соединения пула (`db.Stats()` из урока `10-services-db/05-connection-pool`). **Histogram** раскладывает наблюдения по бакетам «не больше `le`» и отдаёт серии `_bucket{le="…"}` (накопительно, последняя — `+Inf`), `_sum` и `_count`. Из бакетов Prometheus оценивает перцентили (`histogram_quantile(0.99, rate(..._bucket[5m]))`), а из `_sum/_count` — среднее; бакеты агрегируются по всем репликам, поэтому гистограмма лучше, чем **Summary**, который считает квантили внутри одного процесса. Бакеты выбирают под свои SLO: если цель — 300 мс, граница 0.3 должна быть среди них.

**RED.** Для каждого сервиса, принимающего запросы, — три вещи: **R**ate (запросов в секунду), **E**rrors (доля ошибок), **D**uration (распределение длительности). Всё это даёт пара «counter с меткой кода + histogram длительности», которую пишет middleware или interceptor — один раз на весь сервер, а не в каждом обработчике. Для ресурсов (пул, очередь) — USE: utilization, saturation, errors.

**Метки и кардинальность.** Каждое уникальное сочетание значений меток — отдельная серия со своей памятью в сервисе и в Prometheus. Кардинальность — произведение: 20 маршрутов × 10 кодов × 3 метода — 600 серий, терпимо. Метка `user_id` или `order_id` — миллионы серий и упавший Prometheus. Поэтому меткой бывает только маленькое закрытое множество: шаблон маршрута (`GET /orders/{id}` — `r.Pattern`, а не путь), код, имя gRPC-метода. Неизвестное сворачивают в одно значение (`unmatched`, `unknown`). Конкретный заказ ищут в логах и трассах.

**Имена.** `snake_case`, префикс подсистемы, единица измерения в конце и в базовых единицах: `_seconds`, `_bytes`; у counter — `_total`. Не `request_duration_ms`, а `http_request_duration_seconds`.

**Реестр.** Глобальный `prometheus.DefaultRegisterer` удобен в `main`, но в тестах мешает: второй `MustRegister` той же метрики паникует, а значения переходят из теста в тест. Поэтому конструктор метрик принимает `prometheus.Registerer`, в `main` передают `prometheus.NewRegistry()` (и добавляют в него `collectors.NewGoCollector()` и `collectors.NewProcessCollector(...)` для метрик рантайма), а отдают через `promhttp.HandlerFor(reg, promhttp.HandlerOpts{})`. В тестах — `testutil.ToFloat64` для одного значения counter или gauge, `testutil.GatherAndCompare` для сравнения с ожидаемой экспозицией целиком, `testutil.CollectAndCount` — число серий.

**Экземпляры.** Prometheus находит реплики через service discovery Kubernetes и добавляет к каждой серии метки `job` и `instance` сам — сервису не нужно класть в метки имя пода. Суммируют по репликам запросом: `sum by (route) (rate(http_requests_total[5m]))`.

**OpenTelemetry metrics.** У OTel есть свой API метрик (`go.opentelemetry.io/otel/metric`: `Int64Counter`, `Float64Histogram`, `Int64UpDownCounter`) и SDK (`sdk/metric`), который может экспортировать в Prometheus или по OTLP. `otelgrpc` из урока `03-tracing` уже пишет `rpc.server.call.duration` через глобальный `MeterProvider`. Если система целиком на OTel — один SDK на трассы и метрики; если в эксплуатации Prometheus — `client_golang` проще и привычнее. В песочнице доступны оба, урок использует `client_golang`.

## Как это в C#

- **prometheus-net** — прямой аналог `client_golang`: `Metrics.CreateCounter`, `CreateHistogram`, `app.UseHttpMetrics()` добавляет RED для всех запросов ASP.NET Core, `app.MapMetrics()` — эндпоинт. В Go middleware и метки пишут сами (десяток строк из примера), зато видно, что именно считается.
- **System.Diagnostics.Metrics** — встроенный в .NET API (`Meter`, `Counter<T>`, `Histogram<T>`), на который подписывается OpenTelemetry .NET; ASP.NET Core и `HttpClient` сами публикуют `http.server.request.duration`. В Go аналог встроенного API — OTel metrics, но это сторонний модуль, и стандартная библиотека метрик не публикует.
- **Глобальное состояние.** `Metrics.CreateCounter` в prometheus-net тоже регистрирует в статическом реестре по умолчанию; для тестов есть `Metrics.NewCustomRegistry()`. В Go правило то же, только реестр передают явно, без DI.
- **Метки маршрута.** ASP.NET Core даёт шаблон из endpoint routing (`{id}` в пути); в Go с 1.23 шаблон `ServeMux` лежит в `r.Pattern` после маршрутизации, поэтому middleware читает его после `next.ServeHTTP`.

## Ловушки

### Путь вместо шаблона в метке

```go
m.requests.WithLabelValues(r.URL.Path, code).Inc() // /orders/o-1, /orders/o-2, …
```

Каждый заказ — новая серия, а сканер, перебирающий `/admin`, `/.env`, `/wp-login.php`, создаёт их сколько угодно: это и утечка памяти в сервисе, и отказ в обслуживании для Prometheus. Метка — `r.Pattern`, пустой шаблон (маршрут не найден) — одно значение `unmatched`.

### Счётчик вместо gauge и наоборот

```go
inFlight := prometheus.NewCounter(...) // «Dec» у counter нет
queue.Set(float64(processed))           // gauge для накопленного — rate() по нему бессмыслен
```

Если величина только накапливается — counter, и ответы «сколько в секунду» дадут `rate`/`increase`. Если это текущий уровень — gauge. Накопленное в gauge ломается на рестартах: `rate` не узнает сброс.

### Длительность без ошибок или ошибки без длительности

```go
if err != nil {
	m.errors.Inc()
	return nil, err // Observe не вызван: быстрые отказы не попали в гистограмму
}
m.duration.Observe(time.Since(start).Seconds())
```

Гистограмма только по успешным запросам врёт: при сбое базы ответы приходят мгновенно с ошибкой, и график длительности «улучшается». Наблюдайте длительность для всех вызовов, а ошибки различайте меткой `code` у счётчика.

## Задание

Напишите gRPC unary interceptor, который считает RED-метрики сервера. Имена, справки (`Help`) и бакеты даны в шаблоне константами `HandledName`, `HandledHelp`, `HandlingName`, `HandlingHelp`, `InFlightName`, `InFlightHelp` и переменной `Buckets`.

```go
func NewMetrics(reg prometheus.Registerer) (*Metrics, error)
func (m *Metrics) Unary(now func() time.Time) grpc.UnaryServerInterceptor
```

Требования:

- `NewMetrics` создаёт и регистрирует в `reg` ровно три метрики: counter `HandledName` с метками `service`, `method`, `code`; histogram `HandlingName` с метками `service`, `method` и бакетами `Buckets`; gauge `InFlightName` без меток. Справка у каждой — соответствующая константа `…Help`. Если регистрация не удалась (например, метрики уже есть в этом реестре), `NewMetrics` возвращает `nil` и ошибку регистрации (`prometheus.AlreadyRegisteredError` для повторной регистрации), не паникует. Откатывать уже зарегистрированные метрики при этом не нужно;
- interceptor вызывает обработчик ровно один раз с теми же `ctx` и `req` и возвращает его ответ и ошибку без изменений;
- `service` и `method` берутся из `info.FullMethod` вида `/пакет.Сервис/Метод`: `/orders.v1.OrdersService/CreateOrder` → `orders.v1.OrdersService` и `CreateOrder`. Если строка не начинается с `/` или после него нет ровно двух непустых частей через `/`, обе метки — `unknown`;
- `code` — строка `codes.Code.String()` того кода, который увидит клиент: `OK` для `nil`; код статуса, если ошибка несёт gRPC-статус (в том числе обёрнутый через `%w`); `Canceled` или `DeadlineExceeded`, если статуса нет, а это (возможно, обёрнутые) `context.Canceled` или `context.DeadlineExceeded`; иначе `Unknown`. Статус важнее: ошибка со статусом, которая заодно оборачивает ошибку контекста, получает код статуса;
- после каждого завершённого вызова counter с метками `service`, `method`, `code` увеличивается на 1, а в histogram с метками `service`, `method` добавляется длительность в секундах: разность `now()` после возврата обработчика и `now()` перед его вызовом. `now == nil` означает `time.Now`;
- gauge равен числу вызовов, которые обрабатываются прямо сейчас: увеличивается до вызова обработчика и уменьшается после, в том числе если обработчик паникует (паника уходит дальше). Вызов, завершившийся паникой, в counter и histogram не попадает;
- метрики пишутся только в переданный реестр; interceptor безопасен для одновременных вызовов.
