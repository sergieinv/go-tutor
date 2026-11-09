-- name: CreateProduct :one
INSERT INTO products (name, price_cents)
VALUES (?, ?)
RETURNING id;

-- name: ListProductsByIDs :many
SELECT id, name, price_cents
FROM products
WHERE id IN (sqlc.slice('ids'))
ORDER BY id;

-- name: NextOrderID :one
INSERT INTO order_ids DEFAULT VALUES
RETURNING id;

-- name: CreateOrder :exec
INSERT INTO orders (id, customer_id, status, total_cents, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(customer_id), sqlc.arg(status), sqlc.arg(total_cents), sqlc.arg(created_at), sqlc.arg(created_at));

-- name: CreateOrderItem :exec
INSERT INTO order_items (order_id, product_id, quantity, price_cents)
VALUES (?, ?, ?, ?);

-- name: GetOrder :one
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE id = ?;

-- name: ListCustomerOrders :many
SELECT id, customer_id, status, total_cents, created_at, updated_at
FROM orders
WHERE customer_id = sqlc.arg(customer_id)
  AND (sqlc.arg(before_id) = 0 OR id < sqlc.arg(before_id))
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListOrderItems :many
SELECT product_id, quantity, price_cents
FROM order_items
WHERE order_id = ?
ORDER BY product_id;

-- name: ListItemsByOrderIDs :many
SELECT order_id, product_id, quantity, price_cents
FROM order_items
WHERE order_id IN (sqlc.slice('order_ids'))
ORDER BY order_id, product_id;

-- name: UpdateOrderStatus :execrows
UPDATE orders
SET status = sqlc.arg(new_status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = sqlc.arg(old_status);

-- name: CreateOutboxEvent :exec
INSERT INTO outbox (event_id, event_type, aggregate_id, payload, trace_context, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListPendingEvents :many
SELECT id, event_id, event_type, aggregate_id, payload, trace_context, created_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT ?;

-- name: MarkEventsPublished :exec
UPDATE outbox
SET published_at = sqlc.arg(published_at)
WHERE id IN (sqlc.slice('ids'));

-- name: CountPendingEvents :one
SELECT count(*) FROM outbox WHERE published_at IS NULL;

-- name: OldestPendingEvent :many
SELECT created_at FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT 1;
