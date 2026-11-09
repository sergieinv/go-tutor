-- Профиль labs: база и пользователь сервиса «Склад».
--
-- Учебное упрощение: warehouse — владелец схемы (ALL PRIVILEGES на свою
-- базу), потому что сервис сам применяет миграции при старте. В проде —
-- две роли: миграции (DDL) выполняет отдельный пользователь шагом деплоя,
-- а сервис работает под пользователем только с SELECT, INSERT, UPDATE,
-- DELETE (см. labs/warehouse/README.md).
CREATE DATABASE warehouse CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'warehouse'@'%' IDENTIFIED BY 'labs-warehouse';
GRANT ALL PRIVILEGES ON warehouse.* TO 'warehouse'@'%';

-- Программы учеников в сети песочницы знают пароли из репозитория.
-- Подключение оттуда попадает на эту запись — более точный хост, чем '%', —
-- а она заблокирована (как root в 10-runner.sql).
CREATE USER 'warehouse'@'172.29.99.%' IDENTIFIED BY RANDOM PASSWORD ACCOUNT LOCK;
-- Иначе runner (CREATE USER) удалил бы блокировку или сменил пароль сервиса.
GRANT SYSTEM_USER ON *.* TO 'warehouse'@'172.29.99.%', 'warehouse'@'%';
