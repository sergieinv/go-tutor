-- +goose Up

-- processed_events — журнал обработанных событий для идемпотентности.
-- Доставка из Kafka — at-least-once: то же событие (тот же event_id) может
-- прийти повторно после сбоя до коммита офсета или ребалансировки.
-- Первичный ключ по event_id и INSERT ... ON CONFLICT DO NOTHING в той же
-- транзакции, что и запись уведомления, превращают повтор в пустую операцию.
CREATE TABLE processed_events (
    event_id     TEXT    PRIMARY KEY,
    processed_at INTEGER NOT NULL -- unix-время в миллисекундах (UTC)
) WITHOUT ROWID;

-- notifications — результат обработки: «отправленные» покупателю
-- уведомления. Источник истины: журнал «notification sent» пишется только
-- после коммита этой записи.
CREATE TABLE notifications (
    id          INTEGER PRIMARY KEY,
    event_id    TEXT    NOT NULL UNIQUE REFERENCES processed_events (event_id),
    order_id    INTEGER NOT NULL,
    customer_id INTEGER NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('order_created', 'order_cancelled')),
    text        TEXT    NOT NULL,
    created_at  INTEGER NOT NULL -- unix-время в миллисекундах (UTC)
);

CREATE INDEX notifications_order_id ON notifications (order_id);

-- +goose Down
DROP TABLE notifications;
DROP TABLE processed_events;
