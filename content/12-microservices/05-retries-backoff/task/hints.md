## Подсказка 1

Код ошибки даёт `status.Code(err)` — он сам разворачивает обёртки `%w` и для ошибок не из gRPC (включая `context.Canceled`) возвращает `codes.Unknown`. Детали статуса — `status.FromError(err)` и `st.Details()`: это срез `any`, ищите в нём `*errdetails.RetryInfo` приведением типа, а паузу берите из `GetRetryDelay()` — проверьте `nil`, `IsValid()` и знак `AsDuration()`.

## Подсказка 2

`Budget` — структура с `sync.Mutex` и тремя числами: текущие токены, размер ведра и `ratio`. Каждый метод берёт мьютекс. `OnFailure` — `max(0, tokens-1)`, `OnSuccess` — `min(maxTokens, tokens+ratio)`, `AllowRetry` — `tokens > maxTokens/2`.

В `Backoff` не пишите `Base << (k-1)`: на больших `k` сдвиг переполнит `Duration` и даст ноль или отрицательное число. Удваивайте предел в цикле, пока он меньше `Max`, и только потом возьмите `min`.

## Подсказка 3

Скелет `Do`:

```go
for attempt := 1; ; attempt++ {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := fn(ctx)
	// nil → OnSuccess, return nil
	// !Retryable(err) → return err
	// OnFailure; attempt >= attempts или !AllowRetry() → return err
	d, ok := RetryDelay(err)
	if !ok {
		d = p.Backoff(attempt)
	}
	if dl, ok := ctx.Deadline(); ok && now().Add(d).After(dl) {
		return err
	}
	if serr := sleep(ctx, d); serr != nil {
		return serr
	}
}
```

Ожидание по умолчанию — `time.NewTimer(d)` и `select` по `t.C` и `ctx.Done()`.
