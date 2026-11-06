## Подсказка 1

Создайте производный контекст и сразу `defer cancel()` — так при любом выходе из `Search` остальные бэкенды получат отмену. Для `timeout > 0` — `context.WithTimeout(ctx, timeout)`, иначе — `context.WithCancel(ctx)`: `WithTimeout` с нулём истёк бы мгновенно.

## Подсказка 2

Запустите каждый бэкенд в горутине, которая отправляет пару «значение, ошибка» в общий канал. Сделайте канал буферизованным на `len(backends)`: тогда опоздавший бэкенд отправит ответ в буфер и завершится, даже если `Search` уже вернулся.

## Подсказка 3

Ждите ровно `len(backends)` ответов, но в `select` вместе с `ctx.Done()`:

```go
for range backends {
	select {
	case r := <-replies:
		if r.err == nil {
			return r.value, nil
		}
		errs = append(errs, r.err)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
return "", errors.Join(errs...)
```
