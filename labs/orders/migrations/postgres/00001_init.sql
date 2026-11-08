-- +goose Up
-- Каталог товаров. Цена — в копейках (bigint): деньги не храним во float.
CREATE TABLE products (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text   NOT NULL,
    price_cents bigint NOT NULL CHECK (price_cents >= 0)
);

-- Остатки — отдельной таблицей: резервирование меняет только её, и строка
-- товара не блокируется на запись, пока идут заказы.
-- CHECK — вторая линия обороны: даже ошибочный запрос не уведёт остаток в минус.
CREATE TABLE stock (
    product_id bigint  PRIMARY KEY REFERENCES products (id),
    available  integer NOT NULL CHECK (available >= 0)
);

CREATE TABLE orders (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id bigint      NOT NULL,
    status      text        NOT NULL CHECK (status IN ('new', 'reserved', 'cancelled')),
    total_cents bigint      NOT NULL CHECK (total_cents >= 0),
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- История заказов покупателя (и ключ шардирования в модуле 14).
CREATE INDEX orders_customer_id_idx ON orders (customer_id);

-- Цена позиции фиксируется на момент заказа: каталог может измениться.
CREATE TABLE order_items (
    order_id    bigint  NOT NULL REFERENCES orders (id),
    product_id  bigint  NOT NULL REFERENCES products (id),
    quantity    integer NOT NULL CHECK (quantity > 0),
    price_cents bigint  NOT NULL CHECK (price_cents >= 0),
    PRIMARY KEY (order_id, product_id)
);

-- Transactional outbox: событие пишется в той же транзакции, что и заказ.
-- Публикатор (модуль 12) читает строки с published_at IS NULL и отправляет их в Kafka.
CREATE TABLE outbox (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type   text        NOT NULL,
    aggregate_id bigint      NOT NULL,
    payload      jsonb       NOT NULL,
    created_at   timestamptz NOT NULL,
    published_at timestamptz
);

-- Частичный индекс: в нём только неопубликованные события, поэтому он
-- не растёт вместе с историей.
CREATE INDEX outbox_pending_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE stock;
DROP TABLE products;
