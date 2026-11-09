-- name: CreateProduct :one
INSERT INTO products (name, price_cents)
VALUES ($1, $2)
RETURNING id;

-- Все товары заказа одним запросом: массив целиком передаётся одним
-- параметром ($1::bigint[]), а не списком плейсхолдеров.
-- name: ListProductsByIDs :many
SELECT id, name, price_cents
FROM products
WHERE id = ANY(@ids::bigint[])
ORDER BY id;

-- Номер заказа — из последовательности identity-колонки до вставки.
-- nextval не откатывается вместе с транзакцией: номера уникальны всегда,
-- пропуски — норма.
-- name: NextOrderID :one
SELECT nextval(pg_get_serial_sequence('orders', 'id'))::bigint;

-- name: CreateOrder :exec
INSERT INTO orders (id, customer_id, status, total_cents, created_at, updated_at)
VALUES (@id, @customer_id, @status, @total_cents, @created_at, @created_at);

-- name: CreateOrderItem :exec
INSERT INTO order_items (order_id, product_id, quantity, price_cents)
VALUES ($1, $2, $3, $4);

-- name: GetOrder :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = $1;

-- Блокирует строку заказа до конца транзакции: параллельная отмена
-- подождёт и увидит уже новый статус.
-- name: GetOrderForUpdate :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = $1
FOR UPDATE;

-- Страница истории покупателя по ключу: id < before_id, новые первыми.
-- before_id = 0 — первая страница.
-- name: ListCustomerOrders :many
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE customer_id = @customer_id
  AND (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @row_limit;

-- name: ListOrderItems :many
SELECT product_id, quantity, price_cents
FROM order_items
WHERE order_id = $1
ORDER BY product_id;

-- Позиции страницы заказов одним запросом (без N+1).
-- name: ListItemsByOrderIDs :many
SELECT order_id, product_id, quantity, price_cents
FROM order_items
WHERE order_id = ANY(@order_ids::bigint[])
ORDER BY order_id, product_id;

-- Условие на старый статус — защита от гонки: 0 строк значит, что статус
-- уже сменил кто-то другой.
-- name: UpdateOrderStatus :execrows
UPDATE orders
SET status = @new_status, updated_at = @updated_at
WHERE id = @id AND status = @old_status;

-- name: CreateOutboxEvent :exec
INSERT INTO outbox (event_id, event_type, aggregate_id, payload, trace_context, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- Advisory-блокировка на время транзакции relay: публикует одна реплика,
-- иначе две реплики отправляли бы события вперемешку и ломали порядок
-- по ключу. Не взяли — публикует другая, пропускаем такт.
-- name: TryLockOutbox :one
SELECT pg_try_advisory_xact_lock(@lock_key::bigint);

-- name: ListPendingEvents :many
SELECT id, event_id, event_type, aggregate_id, payload, trace_context, created_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT $1;

-- name: MarkEventsPublished :exec
UPDATE outbox
SET published_at = @published_at
WHERE id = ANY(@ids::bigint[]);

-- Отставание outbox: сколько событий ждут и когда создано самое старое.
-- name: OutboxStats :one
SELECT count(*)::bigint AS pending,
       COALESCE(min(created_at), now())::timestamptz AS oldest
FROM outbox
WHERE published_at IS NULL;
