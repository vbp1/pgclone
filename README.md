# pgclone

Утилита для копирования PostgreSQL-реплики с мастера через SSH + gRPC, с поддержкой WAL-стриминга и блочной синхронизации данных.

## Клиент (реплика):
```sh
export PGPASSWORD=secret
pgclone \
  --pghost=pg-master.local \
  --pgport=5432 \
  --pguser=replicator \
  --primary-pgdata=/var/lib/postgresql/16/data \
  --replica-pgdata=/var/lib/postgresql/16/data \
  --replica-waldir=/var/lib/postgresql/16/pg_wal \
  --temp-waldir=/tmp/pgclone-wal \
  --ssh-user=postgres \
  --ssh-key=~/.ssh/id_rsa \
  --parallel=4 \
  --agent-port=35432 \
  [--dry-run] \
  [--verbose]
```

## Агент (мастер):
```sh
export PGPASSWORD=secret
export PGCLONE_PSK=sharedkey123
pgclone agent \
  --agent-port=35432 \
  --pghost=localhost \
  --pgport=5432 \
  --pguser=replicator \
  --primary-pgdata=/var/lib/postgresql/16/data
```

## Сборка и тесты
```sh
make           # сборка + тесты + линт
make install   # установка в $GOBIN
make release   # сборка дистрибутива
```
