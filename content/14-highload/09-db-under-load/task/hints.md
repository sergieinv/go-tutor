## Подсказка 1

Курсор — это не номер страницы, а позиция в порядке ленты: пара `(created_at, id)` последнего показанного заказа. Следующая страница — «всё, что строго после неё»: `created_at` меньше, или `created_at` такой же и `id` меньше. Одно сравнение по времени потеряет заказы с одинаковым временем на границе страницы. Сначала проверьте `limit`, потом разберите курсор (`base64.RawURLEncoding.DecodeString`, `strings.Cut` по `":"`, `strconv.ParseInt`) — и только потом идите в базу.

## Подсказка 2

Условие удобно записать сравнением кортежей — его понимают и SQLite, и PostgreSQL, и оба спускаются по индексу прямо к позиции:

```sql
SELECT id, customer_id, created_at, total FROM orders
WHERE customer_id = $1 AND (created_at, id) < ($2, $3)
ORDER BY created_at DESC, id DESC
LIMIT $4
```

Для первой страницы условия по курсору нет, а номер плейсхолдера у `LIMIT` — следующий после последнего аргумента.

## Подсказка 3

Чтобы узнать, есть ли следующая страница, без второго запроса, попросите на одну строку больше: `LIMIT limit+1`. Пришло больше `limit` — отрежьте лишнюю и сделайте `Next` из последнего оставшегося заказа:

```go
if len(orders) > limit {
	orders = orders[:limit]
	last := orders[limit-1]
	next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", last.CreatedAt, last.ID)))
}
```

Не забудьте `defer rows.Close()` и `rows.Err()` после цикла.
