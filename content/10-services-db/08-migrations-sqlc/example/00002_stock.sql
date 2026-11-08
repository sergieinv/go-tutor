-- +goose Up
ALTER TABLE products ADD COLUMN stock INTEGER NOT NULL DEFAULT 0;
CREATE INDEX products_stock_idx ON products (stock);

-- +goose Down
DROP INDEX products_stock_idx;
ALTER TABLE products DROP COLUMN stock;
