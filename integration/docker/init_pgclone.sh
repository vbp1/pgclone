#!/bin/bash
set -euo pipefail

# This script runs inside the official postgres image from /docker-entrypoint-initdb.d/
# It copies pre-built config files into PGDATA before the server starts so that
# replication works out of the box for integration tests.

if [[ "${ROLE:-primary}" != "primary" ]]; then
  # We only need extended conf on master; replica container starts empty.
  exit 0
fi

cp /pg_hba.conf "$PGDATA/pg_hba.conf"
cp /postgresql.conf "$PGDATA/postgresql.conf"
chmod 0600 "$PGDATA/pg_hba.conf" "$PGDATA/postgresql.conf"
chown postgres:postgres "$PGDATA/pg_hba.conf" "$PGDATA/postgresql.conf" 