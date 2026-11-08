-- +goose Up
-- Та же схема, что для PostgreSQL. Отличия диалекта:
-- AUTO_INCREMENT вместо IDENTITY, DATETIME(6) вместо timestamptz (время
-- храним в UTC, см. ParseTime и Loc в mysql.Open), JSON вместо jsonb,
-- нет частичных индексов. CHECK соблюдается начиная с MySQL 8.0.16.
CREATE TABLE products (
    id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(200) NOT NULL,
    price_cents BIGINT       NOT NULL,
    CONSTRAINT products_price_check CHECK (price_cents >= 0)
);

CREATE TABLE stock (
    product_id BIGINT NOT NULL PRIMARY KEY,
    available  INT    NOT NULL,
    CONSTRAINT stock_available_check CHECK (available >= 0),
    CONSTRAINT stock_product_fk FOREIGN KEY (product_id) REFERENCES products (id)
);

CREATE TABLE orders (
    id          BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    customer_id BIGINT      NOT NULL,
    status      VARCHAR(16) NOT NULL,
    total_cents BIGINT      NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    updated_at  DATETIME(6) NOT NULL,
    CONSTRAINT orders_status_check CHECK (status IN ('new', 'reserved', 'cancelled')),
    CONSTRAINT orders_total_check CHECK (total_cents >= 0),
    INDEX orders_customer_id_idx (customer_id)
);

CREATE TABLE order_items (
    order_id    BIGINT NOT NULL,
    product_id  BIGINT NOT NULL,
    quantity    INT    NOT NULL,
    price_cents BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id),
    CONSTRAINT order_items_quantity_check CHECK (quantity > 0),
    CONSTRAINT order_items_price_check CHECK (price_cents >= 0),
    CONSTRAINT order_items_order_fk FOREIGN KEY (order_id) REFERENCES orders (id),
    CONSTRAINT order_items_product_fk FOREIGN KEY (product_id) REFERENCES products (id)
);

CREATE TABLE outbox (
    id           BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    event_type   VARCHAR(64) NOT NULL,
    aggregate_id BIGINT      NOT NULL,
    payload      JSON        NOT NULL,
    created_at   DATETIME(6) NOT NULL,
    published_at DATETIME(6) NULL,
    -- Частичных индексов в MySQL нет: составной индекс по (published_at, id)
    -- отдаёт неопубликованные (NULL) события по порядку.
    INDEX outbox_pending_idx (published_at, id)
);

-- +goose Down
DROP TABLE outbox;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE stock;
DROP TABLE products;
