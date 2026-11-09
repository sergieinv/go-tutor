## Подсказка 1

Interceptor — цикл попыток вокруг `invoker(...)`. После каждой неудачи ответьте на три вопроса по порядку: остались ли попытки, можно ли повторять этот код (и сколько ждать), успеем ли до дедлайна. Ошибку из `invoker` превращайте в статус через `status.Convert(err)`: у него есть `Code()` и `Details()`.

## Подсказка 2

- `RetryInfo` ищите среди `st.Details()` проверкой типа `d.(*errdetails.RetryInfo)`; пауза — `ri.GetRetryDelay().AsDuration()`.
- Backoff для n-го повтора: начните с `InitialBackoff` и удваивайте n-1 раз, затем `min` с `MaxBackoff`, если он больше нуля.
- Дедлайн: `dl, ok := ctx.Deadline()`; при `ok` и `delay >= dl.Sub(cfg.Now())` — вернуть последнюю ошибку без сна.
- Ошибка `cfg.Sleep` — это ошибка контекста: `status.FromContextError(err).Err()`.
- `x-attempt`: возьмите исходную metadata один раз до цикла (`metadata.FromOutgoingContext`), на каждой попытке — `md := base.Copy(); md.Set("x-attempt", ...)` и `metadata.NewOutgoingContext(ctx, md)`. `AppendToOutgoingContext` в цикле над одним и тем же `ctx` копит значения.

## Подсказка 3

```go
base, _ := metadata.FromOutgoingContext(ctx)
for attempt := 1; ; attempt++ {
	md := base.Copy()
	md.Set("x-attempt", strconv.Itoa(attempt))
	err := invoker(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
	if err == nil || attempt >= max(cfg.MaxAttempts, 1) {
		return err
	}
	delay, ok := retryDelay(status.Convert(err), attempt, cfg) // код + RetryInfo + backoff
	if !ok {
		return err
	}
	if dl, has := ctx.Deadline(); has && delay >= dl.Sub(cfg.Now()) {
		return err
	}
	if serr := cfg.Sleep(ctx, delay); serr != nil {
		return status.FromContextError(serr).Err()
	}
}
```
