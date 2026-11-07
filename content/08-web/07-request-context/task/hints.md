## Подсказка 1

Сначала проверьте `id`. Затем создайте производный контекст: при `timeout > 0` — `context.WithTimeout(r.Context(), timeout)`, иначе — `context.WithCancel(r.Context())` (`WithTimeout` с нулём истёк бы сразу). Сразу `defer cancel()`.

## Подсказка 2

Запустите `fetch` в горутине, которая отправляет пару «значение, ошибка» в канал с буфером 1 — тогда опоздавшая горутина не зависнет, даже если результат уже никто не ждёт. Ждите в `select` либо этот канал, либо `ctx.Done()`.

## Подсказка 3

Ответ выбирайте после ожидания, проверяя правила по порядку:

```go
switch {
case err == nil:
	io.WriteString(w, value)
case r.Context().Err() != nil:
	http.Error(w, "request canceled", http.StatusServiceUnavailable)
case ctx.Err() != nil:
	http.Error(w, "timeout", http.StatusGatewayTimeout)
case errors.Is(err, ErrNotFound):
	// ...
}
```

Порядок важен: если истёк дедлайн самого запроса, `ctx.Err()` тоже не `nil`, но это `503`.
