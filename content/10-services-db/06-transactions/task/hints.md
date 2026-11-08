## Подсказка 1

`WithTx` — пять строк: `BeginTx`, `defer tx.Rollback()`, вызов `fn(tx)`, возврат его ошибки, `tx.Commit()`. `defer` выполняется и при панике, так что `recover` не нужен: транзакция откатится, а паника полетит дальше. Ошибку `Commit` не теряйте — она тоже результат. В `Reserve` все запросы идут через `tx`: обращение к `db` внутри транзакции займёт второе соединение, а в пуле из одного будет ждать вечно.

## Подсказка 2

Списание — одним атомарным запросом, без «прочитал остаток — сравнил — записал»:

```go
res, err := tx.ExecContext(ctx,
	`UPDATE stock SET qty = qty - ? WHERE sku = ? AND qty >= ?`, l.Qty, l.SKU, l.Qty)
```

`RowsAffected() == 0` значит: товара нет или его мало. Различите это вторым запросом `SELECT 1 FROM stock WHERE sku = ?` (через тот же `tx`): `sql.ErrNoRows` — `ErrUnknownSKU`, иначе — `ErrOutOfStock`. Достаточно вернуть ошибку из `fn` — откат сделает `WithTx`.

## Подсказка 3

Комментарий передавайте драйверу как есть: `sql.Null[string]` без `Valid` запишется как `NULL`, и читается в то же поле `Scan(&o.Note)`. `GetOrder`:

```go
err := q.QueryRowContext(ctx, `SELECT note FROM orders WHERE id = ?`, id).Scan(&o.Note)
if errors.Is(err, sql.ErrNoRows) {
	return Order{}, fmt.Errorf("order %d: %w", id, ErrNotFound)
}
// затем строки: SELECT sku, qty FROM reservations WHERE order_id = ? ORDER BY id,
// defer rows.Close(), цикл, rows.Err()
```
