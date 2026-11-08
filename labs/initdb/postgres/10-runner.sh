#!/bin/sh
# Профиль labs: роль, от которой runner создаёт временные базы запусков.
# Не суперпользователь: программа ученика работает в сети песочницы, и
# пароль из репозитория ей известен, поэтому вход как postgres оттуда
# запрещён (COPY ... FROM PROGRAM дал бы шелл в контейнере базы).
set -eu

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres <<'SQL'
CREATE ROLE runner LOGIN CREATEROLE CREATEDB PASSWORD 'labs-runner';
-- С PostgreSQL 16 создатель роли получает только ADMIN OPTION; SET и
-- INHERIT нужны, чтобы назначать роль владельцем базы и удалять базу.
ALTER ROLE runner SET createrole_self_grant = 'set, inherit';
-- Роли запусков подключаются только к своей базе.
REVOKE CONNECT, TEMPORARY ON DATABASE postgres, orders, template1 FROM PUBLIC;
GRANT CONNECT ON DATABASE postgres TO runner;
SQL

# Суперпользователь не входит из сети песочницы (подсеть задана в docker-compose.yml).
sed -i '1i host all postgres 172.29.99.0/24 reject' "$PGDATA/pg_hba.conf"
