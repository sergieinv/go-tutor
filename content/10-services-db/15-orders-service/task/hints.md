## Подсказка 1

Пройдите по разделу «Работа с БД» и разделам «Ошибки», «context и отмена» чек-листа (`/review/`) и для каждой функции спросите. Куда уходит ошибка — наверх или в никуда? Что будет с транзакцией и соединением на каждом раннем `return`? Чей контекст получает запрос к базе? Сколько запросов уходит на один HTTP-запрос и что видит клиент при сбое? Замечаний семь: три в создании заказа, по одному в чтении позиций, в списке заказов, в обработчике `POST /orders` и в переводе ошибок в ответ.

## Подсказка 2

Подозрительные места:

- `context.Background()` в `createOrder`;
- цикл «проверяем остатки» через `s.db` до начала транзакции;
- `BeginTx` без `defer tx.Rollback()` в `create`;
- `tx.ExecContext` для `outbox` без проверки результата;
- `rows` в `loadItems` без `defer rows.Close()`;
- `loadItems` в цикле по заказам в `listOrders`;
- `err.Error()` в ветке `default` функции `fail`.

Проверьте каждое мысленным экспериментом: клиент отменил запрос; две позиции одного товара 3 + 3 при остатке 5; сбой вставки позиции; сбой записи в outbox; битое значение `quantity` в базе; сто заказов в списке; пропавшая таблица.

## Подсказка 3

- `createOrder`: `s.create(r.Context(), req.Items)`.
- `create`: уберите предварительную проверку. После `BeginTx` — `defer tx.Rollback()`. Резерв — `UPDATE products SET available = available - ? WHERE id = ? AND available >= ?`, при `RowsAffected() == 0` — `SELECT id FROM products WHERE id = ?` внутри транзакции: `sql.ErrNoRows` → `errUnknown`, иначе `errOutOfStock`. Ошибку вставки в outbox верните.
- `loadItems`: `defer rows.Close()` сразу после проверки ошибки `QueryContext`.
- `listOrders`: один запрос `SELECT o.id, o.status, i.product_id, i.quantity FROM orders o LEFT JOIN order_items i ON i.order_id = o.id ORDER BY o.id, i.rowid` и сборка заказов в цикле (`sql.NullInt64` для позиций заказа без строк).
- `fail`: в `default` — `log.Printf`/`slog` с подробностями и `{"error": "internal error"}` клиенту.
