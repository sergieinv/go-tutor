-- +goose Up
CREATE TABLE products (
    id    INTEGER PRIMARY KEY,
    name  TEXT    NOT NULL UNIQUE,
    price INTEGER NOT NULL CHECK (price >= 0),
    note  TEXT
);

-- +goose Down
DROP TABLE products;
