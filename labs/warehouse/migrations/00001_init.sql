-- +goose Up
-- Схема «Склада». InnoDB — ради транзакций и блокировок строк; utf8mb4 —
-- полноценный UTF-8 (utf8 в MySQL — устаревший трёхбайтовый). Время — в
-- DATETIME(6) в UTC (ParseTime и Loc задаёт store/mysql.Open). CHECK
-- соблюдается начиная с MySQL 8.0.16 — последняя линия защиты от ушедшего
-- в минус остатка, если в коде окажется ошибка.
CREATE TABLE stock (
    product_id BIGINT      NOT NULL PRIMARY KEY,
    available  INT         NOT NULL,
    reserved   INT         NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT stock_available_check CHECK (available >= 0),
    CONSTRAINT stock_reserved_check CHECK (reserved >= 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- Резерв заказа. order_id — ключ идемпотентности Reserve/Release.
-- «Надгробие» (Release без Reserve) — строка в статусе released без позиций.
CREATE TABLE reservations (
    order_id   BIGINT      NOT NULL PRIMARY KEY,
    status     VARCHAR(16) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT reservations_status_check CHECK (status IN ('reserved', 'released'))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- Внешнего ключа на stock нет нарочно: резерв ссылается на строки,
-- которые транзакция только что заблокировала, а проверка FK взяла бы ещё
-- одну разделяемую блокировку на каждую позицию.
CREATE TABLE reservation_items (
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    quantity   INT    NOT NULL,
    PRIMARY KEY (order_id, product_id),
    CONSTRAINT reservation_items_quantity_check CHECK (quantity > 0),
    CONSTRAINT reservation_items_order_fk FOREIGN KEY (order_id) REFERENCES reservations (order_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE reservation_items;
DROP TABLE reservations;
DROP TABLE stock;
