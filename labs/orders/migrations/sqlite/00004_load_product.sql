-- +goose Up
-- Модуль 14: товар для нагрузочных прогонов (labs/loadgen). У «Склада»
-- для него миллион штук (warehouse/migrations/00003_load_stock.sql):
-- заказы генератора не упираются в остатки демо-товаров. На свежем стенде
-- он получает id 4 — тот же, что у остатков на складе.
INSERT INTO products (name, price_cents) VALUES ('Кабель USB-C', 99000);

-- +goose Down
DELETE FROM products WHERE name = 'Кабель USB-C';
