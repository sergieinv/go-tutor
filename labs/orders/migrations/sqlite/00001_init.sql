-- +goose Up
-- Та же схема, что для PostgreSQL. Отличия диалекта:
-- INTEGER PRIMARY KEY — псевдоним rowid (автонумерация); время — DATETIME
-- (драйвер modernc.org/sqlite пишет его текстом и читает обратно в time.Time);
-- JSON — обычный TEXT. Внешние ключи SQLite проверяет только при
-- PRAGMA foreign_keys = ON — его включает sqlite.Open.
CREATE TABLE products (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    price_cents INTEGER NOT NULL CHECK (price_cents >= 0)
);

CREATE TABLE stock (
    product_id INTEGER NOT NULL PRIMARY KEY REFERENCES products (id),
    available  INTEGER NOT NULL CHECK (available >= 0)
);

CREATE TABLE orders (
    id          INTEGER  PRIMARY KEY,
    customer_id INTEGER  NOT NULL,
    status      TEXT     NOT NULL CHECK (status IN ('new', 'reserved', 'cancelled')),
    total_cents INTEGER  NOT NULL CHECK (total_cents >= 0),
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

CREATE INDEX orders_customer_id_idx ON orders (customer_id);

CREATE TABLE order_items (
    order_id    INTEGER NOT NULL REFERENCES orders (id),
    product_id  INTEGER NOT NULL REFERENCES products (id),
    quantity    INTEGER NOT NULL CHECK (quantity > 0),
    price_cents INTEGER NOT NULL CHECK (price_cents >= 0),
    PRIMARY KEY (order_id, product_id)
);

CREATE TABLE outbox (
    id           INTEGER  PRIMARY KEY,
    event_type   TEXT     NOT NULL,
    aggregate_id INTEGER  NOT NULL,
    payload      TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    published_at DATETIME
);

-- Частичные индексы SQLite поддерживает, как и PostgreSQL.
CREATE INDEX outbox_pending_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE stock;
DROP TABLE products;
