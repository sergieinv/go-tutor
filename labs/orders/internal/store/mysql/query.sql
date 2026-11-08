-- RETURNING в MySQL нет: id новой строки берём из LastInsertId
-- (аннотация :execlastid).
-- name: CreateProduct :execlastid
INSERT INTO products (name, price_cents)
VALUES (?, ?);

-- name: CreateStock :exec
INSERT INTO stock (product_id, available)
VALUES (?, ?);

-- name: GetProduct :one
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id = ?;

-- Массивов-параметров в MySQL нет: sqlc.slice разворачивается в IN (?, ?, …)
-- по длине среза во время выполнения.
-- name: ListProductsByIDs :many
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id IN (sqlc.slice('ids'))
ORDER BY p.id;

-- Атомарное резервирование: проверка и списание — одна инструкция.
-- 0 затронутых строк — остатка не хватило.
-- name: ReserveStock :execrows
UPDATE stock
SET available = available - sqlc.arg(quantity)
WHERE product_id = sqlc.arg(product_id) AND available >= sqlc.arg(quantity);

-- name: ReleaseStock :execrows
UPDATE stock
SET available = available + sqlc.arg(quantity)
WHERE product_id = sqlc.arg(product_id);

-- name: CreateOrder :execlastid
INSERT INTO orders (customer_id, status, total_cents, created_at, updated_at)
VALUES (?, ?, ?, ?, ?);

-- name: CreateOrderItem :exec
INSERT INTO order_items (order_id, product_id, quantity, price_cents)
VALUES (?, ?, ?, ?);

-- name: GetOrder :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = ?;

-- Блокирует строку заказа до конца транзакции. В InnoDB это ещё и
-- «текущее чтение»: оно видит последнюю закоммиченную версию строки,
-- а не снимок REPEATABLE READ.
-- name: GetOrderForUpdate :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = ?
FOR UPDATE;

-- name: ListOrderItems :many
SELECT product_id, quantity, price_cents
FROM order_items
WHERE order_id = ?
ORDER BY product_id;

-- name: UpdateOrderStatus :execrows
UPDATE orders
SET status = sqlc.arg(new_status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = sqlc.arg(old_status);

-- name: CreateOutboxEvent :exec
INSERT INTO outbox (event_type, aggregate_id, payload, created_at)
VALUES (?, ?, ?, ?);

-- name: ListPendingEvents :many
SELECT id, event_type, aggregate_id, payload, created_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT ?;
