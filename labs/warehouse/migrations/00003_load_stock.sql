-- +goose Up
-- Модуль 14: остаток товара 4 «Кабель USB-C» для нагрузочных прогонов
-- (labs/loadgen): миллион штук — заказы генератора не кончаются. Резервы
-- всех заказов на него идут в одну строку — это и есть горячая строка,
-- которую видно под нагрузкой (блокировка FOR UPDATE на каждом Reserve).
INSERT INTO stock (product_id, available, reserved) VALUES (4, 1000000, 0);

-- +goose Down
DELETE FROM stock WHERE product_id = 4;
