## Подсказка 1

Пройдите по каждому методу и спросите: кто и когда вернёт соединение в пул? Соединение держат незакрытые `*sql.Rows`, `*sql.Row` до вызова `Scan` и транзакция до `Commit`/`Rollback`. Ещё один вопрос к каждому запросу: доходит ли до него `ctx` — у одного метода он принимается, но не используется.

## Подсказка 2

- `ByTag`: ранний `return` при достижении `limit` оставляет `rows` открытыми. Нужен `defer rows.Close()` сразу после проверки ошибки `QueryContext`; ещё лучше — `LIMIT ?` в самом запросе, тогда цикл дочитывает выборку до конца.
- `Count`: `QueryRow` → `QueryRowContext(ctx, ...)`.
- `Delete`: `QueryRowContext` без `Scan` держит соединение и теряет ошибку — для запроса без результата есть `ExecContext`.
- `Move`: на ошибках функция выходит, не завершив транзакцию.

## Подсказка 3

```go
func (r *Repo) Move(ctx context.Context, id int64, shelf string) error {
	if shelf == "" {
		return ErrBadShelf // проверка до транзакции: соединение не нужно
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // после Commit ничего не делает
	// UPDATE ... → RowsAffected == 0 → return ErrNotFound (откатит defer)
	// INSERT INTO moves ...
	return tx.Commit()
}
```
