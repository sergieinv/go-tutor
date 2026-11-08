-- name: CreateProduct :one
INSERT INTO products (name, price_cents)
VALUES ($1, $2)
RETURNING id;

-- name: CreateStock :exec
INSERT INTO stock (product_id, available)
VALUES ($1, $2);

-- name: GetProduct :one
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id = $1;

-- Все товары заказа одним запросом: массив целиком передаётся одним
-- параметром ($1::bigint[]), а не списком плейсхолдеров.
-- name: ListProductsByIDs :many
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id = ANY(@ids::bigint[])
ORDER BY p.id;

-- Атомарное резервирование: проверка и списание — одна инструкция.
-- 0 затронутых строк — остатка не хватило.
-- name: ReserveStock :execrows
UPDATE stock
SET available = available - @quantity
WHERE product_id = @product_id AND available >= @quantity;

-- name: ReleaseStock :execrows
UPDATE stock
SET available = available + @quantity
WHERE product_id = @product_id;

-- name: CreateOrder :one
INSERT INTO orders (customer_id, status, total_cents, created_at, updated_at)
VALUES (@customer_id, @status, @total_cents, @created_at, @created_at)
RETURNING id;

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

-- name: ListOrderItems :many
SELECT product_id, quantity, price_cents
FROM order_items
WHERE order_id = $1
ORDER BY product_id;

-- Условие на старый статус — защита от гонки: 0 строк значит, что статус
-- уже сменил кто-то другой.
-- name: UpdateOrderStatus :execrows
UPDATE orders
SET status = @new_status, updated_at = @updated_at
WHERE id = @id AND status = @old_status;

-- name: CreateOutboxEvent :exec
INSERT INTO outbox (event_type, aggregate_id, payload, created_at)
VALUES ($1, $2, $3, $4);

-- name: ListPendingEvents :many
SELECT id, event_type, aggregate_id, payload, created_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT $1;
