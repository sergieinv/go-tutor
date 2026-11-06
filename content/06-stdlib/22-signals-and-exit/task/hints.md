## Подсказка 1

Контекст с отменой по сигналам даёт `signal.NotifyContext(context.Background(), sigs...)`; вторым результатом идёт функция `stop` — отложите её вызов, она снимает подписку. Пустой `sigs` замените на `[]os.Signal{os.Interrupt, syscall.SIGTERM}`.

## Подсказка 2

Как отличить штатную остановку: сигнал пришёл, если `ctx.Err() != nil` у вашего контекста, а ошибка `run` — та самая отмена, если `errors.Is(err, context.Canceled)`. Нужны оба условия. Панику ловит `recover()` в отложенной функции — а чтобы из неё поменять возвращаемый код, сделайте результат именованным.

## Подсказка 3

```go
defer func() {
	if r := recover(); r != nil {
		fmt.Fprintf(stderr, "panic: %v\n", r)
		code = 2
	}
}()

err := run(ctx)
switch {
case err == nil:
	return 0
case ctx.Err() != nil && errors.Is(err, context.Canceled):
	return 0
}
fmt.Fprintf(stderr, "error: %v\n", err)
return 1
```
