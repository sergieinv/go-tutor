-- +goose Up
-- Модуль 11: остатки переехали в «Склад» (см. postgres/00003).
DROP TABLE stock;

-- Последовательность номеров заказов. В SQLite нет SEQUENCE: её роль
-- играет таблица с AUTOINCREMENT — он гарантирует, что номер не выдаётся
-- повторно даже после удаления строк (обычный rowid может переиспользовать
-- максимальный). Нумерация продолжается после уже существующих заказов.
CREATE TABLE order_ids (
    id INTEGER PRIMARY KEY AUTOINCREMENT
);
INSERT INTO order_ids (id) SELECT id FROM orders ORDER BY id DESC LIMIT 1;

DROP TABLE outbox;
CREATE TABLE outbox (
    id            INTEGER  PRIMARY KEY,
    event_id      TEXT     NOT NULL UNIQUE,
    event_type    TEXT     NOT NULL,
    aggregate_id  INTEGER  NOT NULL,
    payload       BLOB     NOT NULL,
    trace_context TEXT     NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL,
    published_at  DATETIME
);
CREATE INDEX outbox_pending_idx ON outbox (id) WHERE published_at IS NULL;

CREATE INDEX orders_customer_id_id_idx ON orders (customer_id, id DESC);
DROP INDEX orders_customer_id_idx;

-- +goose Down
CREATE INDEX orders_customer_id_idx ON orders (customer_id);
DROP INDEX orders_customer_id_id_idx;
DROP TABLE outbox;
CREATE TABLE outbox (
    id           INTEGER  PRIMARY KEY,
    event_type   TEXT     NOT NULL,
    aggregate_id INTEGER  NOT NULL,
    payload      TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    published_at DATETIME
);
CREATE INDEX outbox_pending_idx ON outbox (id) WHERE published_at IS NULL;
DROP TABLE order_ids;
CREATE TABLE stock (
    product_id INTEGER NOT NULL PRIMARY KEY REFERENCES products (id),
    available  INTEGER NOT NULL CHECK (available >= 0)
);
