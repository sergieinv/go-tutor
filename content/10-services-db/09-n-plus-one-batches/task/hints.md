## Подсказка 1

Сначала нормализуйте вход: копия `ids`, `slices.Sort` и `slices.Compact` — дубли уйдут, а порядок по возрастанию заодно даст порядок результата. Пустой список — сразу `return`, без запросов. Дальше `for chunk := range slices.Chunk(ids, MaxIDsPerQuery)` — на каждую пачку два запроса: заказы и позиции.

## Подсказка 2

Текст `IN (?, ?, ?)` собирается из числа плейсхолдеров, а значения идут отдельным `[]any`: `strings.TrimSuffix(strings.Repeat("?, ", n), ", ")`. Позиции читайте одним запросом `WHERE order_id IN (...) ORDER BY id` и раскладывайте по заказам через `map[int64]int` — id заказа → индекс в срезе (указатель `&orders[i]` после всех `append` в `orders` безопасен, пока срез больше не растёт).

## Подсказка 3

Каждый `QueryContext` — `defer rows.Close()` в отдельной функции и `rows.Err()` после цикла: пул в тестах из одного соединения, и незакрытые `Rows` первого запроса заблокируют второй. Для `InsertItems`:

```go
for chunk := range slices.Chunk(items, MaxParams/3) {
	args := make([]any, 0, len(chunk)*3)
	for _, it := range chunk {
		args = append(args, orderID, it.SKU, it.Qty)
	}
	q := `INSERT INTO items (order_id, sku, qty) VALUES ` +
		strings.TrimSuffix(strings.Repeat("(?, ?, ?), ", len(chunk)), ", ")
	// db.ExecContext(ctx, q, args...), ошибку — обёрнутой через %w
}
```
