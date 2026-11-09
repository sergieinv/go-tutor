#!/bin/sh
# Профиль labs: роль для потоковой реплики postgres-replica (pg_basebackup
# и поток WAL). Из сети песочницы репликация закрыта: пароль лежит в
# репозитории, а поток WAL — это все базы целиком, включая чужие запуски.
set -eu

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres <<'SQL'
CREATE ROLE replicator REPLICATION LOGIN PASSWORD 'labs-replicator';
SQL

sed -i '1i host replication all 172.29.99.0/24 reject' "$PGDATA/pg_hba.conf"
echo 'host replication replicator all scram-sha-256' >> "$PGDATA/pg_hba.conf"
