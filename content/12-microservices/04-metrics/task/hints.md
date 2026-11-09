## Подсказка 1

Три коллектора: `prometheus.NewCounterVec(prometheus.CounterOpts{Name: HandledName, Help: HandledHelp}, []string{"service", "method", "code"})`, `prometheus.NewHistogramVec` с `Buckets: Buckets` и метками `service`, `method`, и `prometheus.NewGauge`. Регистрируйте через `reg.Register(c)`, а не `MustRegister`: ошибку надо вернуть, а не паниковать.

## Подсказка 2

`info.FullMethod` выглядит как `/orders.v1.OrdersService/CreateOrder`: `strings.CutPrefix(full, "/")`, затем `strings.Cut(rest, "/")`. Всё, что не разобралось ровно на две непустые части, — `unknown`/`unknown`. Код: `nil` — `OK`; если `status.FromError(err)` вернул `ok` — его код; иначе `status.FromContextError(err).Code()` (он узнаёт обёрнутые `context.Canceled` и `DeadlineExceeded`, остальное — `Unknown`).

## Подсказка 3

В interceptor: `m.inFlight.Inc()` и сразу `defer m.inFlight.Dec()` — так gauge вернётся и при панике. `start := now()`, вызов `handler(ctx, req)`, затем `Observe(now().Sub(start).Seconds())` у гистограммы и `Inc()` у счётчика с `code.String()`. Ответ и ошибку обработчика верните как есть. Если `now == nil`, используйте `time.Now`.
