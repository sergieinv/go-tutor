-- +goose Up
-- Демо-остатки для товаров каталога «Заказов» (те же id): 1 «Клавиатура»,
-- 2 «Мышь», 3 «Монитор». Монитора мало — на нём удобно получить OUT_OF_STOCK.
INSERT INTO stock (product_id, available, reserved) VALUES
    (1, 10, 0),
    (2, 25, 0),
    (3, 3, 0);

-- +goose Down
DELETE FROM stock WHERE product_id IN (1, 2, 3);
