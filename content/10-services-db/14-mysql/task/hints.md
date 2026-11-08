## Подсказка 1

`Config` — это `mysql.NewConfig()` плюс правка полей; строку DSN склеивать не нужно, `FormatDSN` экранирует пароль сам. Адрес с портом собирает `net.JoinHostPort` — он же расставит скобки вокруг IPv6. `Classify` — это `errors.As(err, &me)` и `switch me.Number`, а чтобы в цепочке остались обе ошибки, оберните двумя `%w`: `fmt.Errorf("%w: %w", ErrDuplicate, err)`.

## Подсказка 2

`InTx` удобно разбить на две функции. Первая — одна попытка: `BeginTx`, сразу `defer tx.Rollback()` (он вернёт соединение и при ошибке, и при панике, а после `Commit` ничего не делает), `fn`, `Commit`. Вторая — цикл попыток. Первой функции нужно сообщить, началась ли транзакция: ошибку `BeginTx` повторять не нужно.

## Подсказка 3

```go
attempts := max(r.Attempts, 1)
for attempt := 1; ; attempt++ {
	began, err := once(ctx, db, fn)
	if err == nil || !began {
		return err
	}
	err = Classify(err)
	if !errors.Is(err, ErrRetryable) || attempt == attempts {
		return err
	}
	if r.Wait != nil {
		if err := r.Wait(ctx, attempt+1); err != nil {
			return err
		}
	}
}
```
