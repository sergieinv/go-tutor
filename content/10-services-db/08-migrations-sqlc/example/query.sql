-- name: CreateProduct :one
INSERT INTO products (name, price, note, stock)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetProduct :one
SELECT * FROM products WHERE id = ?;

-- name: ListInStock :many
SELECT id, name, stock FROM products
WHERE stock >= sqlc.arg(min_stock)
ORDER BY id;

-- name: Restock :execrows
UPDATE products SET stock = stock + sqlc.arg(delta) WHERE id = sqlc.arg(id);
