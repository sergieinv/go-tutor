## Подсказка 1

`Create` — `r.db.ExecContext(ctx, "INSERT ... VALUES (?, ?, ?)", owner, email, balance)` и `res.LastInsertId()`: `nil`-указатель драйвер запишет как `NULL`. `Get` — `QueryRowContext(...).Scan(&a.ID, &a.Owner, &a.Email, &a.Balance)`: поле `*string` можно сканировать по адресу, `NULL` станет `nil`. `sql.ErrNoRows` превращайте в `fmt.Errorf("...: %w", ErrNotFound)`.

## Подсказка 2

`List`: после `QueryContext` сразу `defer rows.Close()`, в цикле `rows.Next()` — `Scan` и `append`, после цикла — `if err := rows.Err(); err != nil`. Сортировку и фильтр делает SQL: `WHERE balance >= ? ORDER BY id`.

## Подсказка 3

`Transfer` — сначала проверка `amount` и `from == to`, затем:

```go
tx, err := r.db.BeginTx(ctx, nil)
if err != nil {
	return err
}
defer tx.Rollback() // после Commit ничего не делает
// SELECT balance ... WHERE id = from  → ErrNoRows → ErrNotFound
// SELECT 1 ... WHERE id = to          → ErrNoRows → ErrNotFound
// balance < amount                     → ErrInsufficientFunds
// два UPDATE через tx.ExecContext
return tx.Commit()
```

Все запросы внутри — через `tx`, а не `r.db`: иначе они уйдут в другое соединение мимо транзакции (а с пулом из одного соединения — повиснут).
