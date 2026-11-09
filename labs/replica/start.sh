#!/bin/sh
# Потоковая реплика PostgreSQL профиля labs. Данные — в tmpfs, поэтому при
# каждом старте реплика заново снимает копию с основного сервера
# (pg_basebackup -R пишет standby.signal и primary_conninfo) и дальше
# получает WAL потоком (-c fast: не ждать плановой контрольной точки,
# до 5 минут). pg_hba.conf приезжает вместе с копией: правила
# входа у реплики те же, что у основного сервера.
set -eu

if [ ! -s "$PGDATA/PG_VERSION" ]; then
	until pg_isready -h postgres -U replicator -q; do sleep 1; done
	mkdir -p "$PGDATA"
	chown postgres:postgres "$PGDATA"
	chmod 0700 "$PGDATA"
	gosu postgres pg_basebackup -h postgres -U replicator -D "$PGDATA" -R -X stream -c fast
fi
exec docker-entrypoint.sh "$@"
