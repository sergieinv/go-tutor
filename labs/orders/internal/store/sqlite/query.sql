-- name: CreateProduct :one
INSERT INTO products (name, price_cents)
VALUES (?, ?)
RETURNING id;

-- name: CreateStock :exec
INSERT INTO stock (product_id, available)
VALUES (?, ?);

-- name: GetProduct :one
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id = ?;

-- name: ListProductsByIDs :many
SELECT p.id, p.name, p.price_cents, s.available
FROM products p
JOIN stock s ON s.product_id = p.id
WHERE p.id IN (sqlc.slice('ids'))
ORDER BY p.id;

-- name: ReserveStock :execrows
UPDATE stock
SET available = available - sqlc.arg(quantity)
WHERE product_id = sqlc.arg(product_id) AND available >= sqlc.arg(quantity);

-- name: ReleaseStock :execrows
UPDATE stock
SET available = available + sqlc.arg(quantity)
WHERE product_id = sqlc.arg(product_id);

-- name: CreateOrder :one
INSERT INTO orders (customer_id, status, total_cents, created_at, updated_at)
VALUES (sqlc.arg(customer_id), sqlc.arg(status), sqlc.arg(total_cents), sqlc.arg(created_at), sqlc.arg(created_at))
RETURNING id;

-- name: CreateOrderItem :exec
INSERT INTO order_items (order_id, product_id, quantity, price_cents)
VALUES (?, ?, ?, ?);

-- name: GetOrder :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = ?;

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
