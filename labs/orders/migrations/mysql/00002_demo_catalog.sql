-- +goose Up
-- Демо-каталог для ручной проверки (curl из README). Каталогом сервис
-- «Заказы» не управляет: в модуле 11 товары и остатки переедут в «Склад».
-- SQL одинаковый для всех трёх баз: id не задаём явно, а находим по имени.
INSERT INTO products (name, price_cents) VALUES
    ('Клавиатура', 499000),
    ('Мышь', 199000),
    ('Монитор', 2499000);

INSERT INTO stock (product_id, available)
SELECT id, CASE name WHEN 'Клавиатура' THEN 10 WHEN 'Мышь' THEN 25 ELSE 3 END
FROM products
WHERE name IN ('Клавиатура', 'Мышь', 'Монитор');

-- +goose Down
DELETE FROM stock
WHERE product_id IN (SELECT id FROM products WHERE name IN ('Клавиатура', 'Мышь', 'Монитор'));
DELETE FROM products WHERE name IN ('Клавиатура', 'Мышь', 'Монитор');
