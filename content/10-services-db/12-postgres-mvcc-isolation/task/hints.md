## Подсказка 1

`IsRetryable` — `errors.As(err, &pgErr)` с `var pgErr *pgconn.PgError` (пакет `github.com/jackc/pgx/v5/pgconn`) и сравнение `pgErr.Code` с `"40001"` и `"40P01"`. Для `nil` `errors.As` вернёт `false` сам.

## Подсказка 2

Вынесите одну попытку в отдельную функцию: тогда `defer tx.Rollback(ctx)` сработает в конце каждой попытки, а не всего цикла, — и при ошибке, и при панике. После успешного `Commit` откат ничего не делает (pgx вернёт `pgx.ErrTxClosed`, её можно игнорировать).

```go
func attempt(ctx context.Context, tx pgx.Tx, fn func(context.Context, pgx.Tx) error) error {
	defer tx.Rollback(ctx)
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

## Подсказка 3

Цикл: `maxAttempts = max(maxAttempts, 1)`; на каждой итерации сначала `ctx.Err()`, затем `BeginTx` (ошибку — сразу наружу), затем `attempt`. Неповторяемую ошибку (и `nil`) возвращайте сразу. Если цикл кончился — `fmt.Errorf("%w: %w", ErrRetriesExhausted, lastErr)`: два `%w` сохраняют и признак исчерпания, и `*pgconn.PgError`.
