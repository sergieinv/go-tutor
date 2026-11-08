## Подсказка 1

Оптимистичная блокировка — это условие на версию в самом `UPDATE`: `UPDATE stock SET qty = ?, version = version + 1 WHERE id = ? AND version = ?`. Если между вашим чтением и записью строку изменил кто-то другой, версия в базе уже другая, и запрос не затронет ни одной строки: `RowsAffected() == 0` → `ErrConflict`. `TryReserve` = `Get` → проверки → уменьшить `Qty` → `Save`.

## Подсказка 2

`Do` — цикл `for attempt := 1; attempt <= attempts; attempt++`, где `attempts := max(r.Attempts, 1)`. В начале итерации — `ctx.Err()`. Результат `fn(ctx)`: всё, что не `errors.Is(err, ErrConflict)` (включая `nil`), возвращается сразу. После последней попытки не спите — сразу возвращайте конфликт. Пауза — `r.Backoff(attempt)` (или 0, если `Backoff` не задан), а ошибку `Sleep` возвращайте как есть.

## Подсказка 3

Ожидание по умолчанию, прерываемое контекстом:

```go
t := time.NewTimer(d)
defer t.Stop()
select {
case <-t.C:
	return nil
case <-ctx.Done():
	return ctx.Err()
}
```

`ExpBackoff`: предел начинается с `base` и удваивается `attempt-1` раз, но не дальше `maxDelay` — останавливайте удвоение, как только предел дошёл до максимума, иначе на больших `attempt` `Duration` переполнится. Результат — `time.Duration(rnd() * float64(min(limit, maxDelay)))`.
