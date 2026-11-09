## Подсказка 1

Разбейте `CreateOrder` на шаги и для каждой ошибки ответьте на один вопрос: «знаем ли мы точно, что резерва нет?». Точно знаем после отказа склада (`FailedPrecondition`, `InvalidArgument`). Во всех остальных случаях — нет, и нужна компенсация. Код достаёт `status.Code(err)` — он работает и с обёрнутой ошибкой. `ErrorInfo` лежит в `Details()` статуса из `status.FromError(err)` (или `status.Convert(err)`) — обе тоже разворачивают обёрнутую ошибку.

## Подсказка 2

Для компенсации и проверки `Exists` нужен контекст, который хранит значения запроса, но не отменяется вместе с ним: `context.WithTimeout(context.WithoutCancel(ctx), timeout)`. Создавайте свежий такой контекст для каждого вызова — `Release`, `AddPendingRelease`, `Exists`, — иначе медленный `Release` съест время очереди. Несколько ошибок в одну собирает `errors.Join`, а `fmt.Errorf("%w: %w", ErrUnavailable, err)` делает так, что `errors.Is` находит обе.

## Подсказка 3

Скелет:

```go
if err := s.Warehouse.Reserve(ctx, id, sorted); err != nil {
	switch code := status.Code(err); {
	case code == codes.FailedPrecondition && hasReason(err, "OUT_OF_STOCK"):
		return Order{}, fmt.Errorf("%w: %w", ErrOutOfStock, err)
	case code == codes.FailedPrecondition || code == codes.InvalidArgument:
		return Order{}, fmt.Errorf("reserve: %w", err)
	case code == codes.Unavailable || code == codes.DeadlineExceeded:
		err = fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return Order{}, errors.Join(err, s.compensate(ctx, id)) // compensate: Release, при ошибке — в очередь
}
```

После `Save` с ошибкой: если `errors.Is(err, ErrCommitUnknown)` — спросите `Exists`. `true` — вернуть заказ без ошибки, `false` — компенсировать, ошибка `Exists` — вернуть обе ошибки и резерв не трогать. В `RetryReleases` проверяйте `ctx.Err()` перед каждым номером, а неудачный `Release` записывайте в список ошибок и идите дальше.
