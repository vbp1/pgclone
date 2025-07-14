#!/usr/bin/env bash
# Comprehensive demo script for Go-based pgclone.
# 1. Builds pgclone binary and Docker image (integration/docker).
# 2. Generates reusable SSH key-pair.
# 3. Starts primary/replica containers (with interactive checks).
# 4. Runs pgclone inside the replica via SSH to the primary.
# 5. Optionally starts replica in standby mode and cleans up.

set -euo pipefail

# --- configuration ---
IMAGE=pgclone-go-demo
NETWORK=pgclone-net
PRIMARY_CONTAINER=pg-primary
REPLICA_CONTAINER=pg-replica

DIR=$(cd "$(dirname "$0")" && pwd)
BINARY="$DIR/pgclone"            # compiled pgclone binary (linux/amd64)
KEY_PRIV="$DIR/test-key"
KEY_PUB="$DIR/test-key.pub"

declare -A skip_container=()

build_docker_image() {
    echo ">>> Removing image '$IMAGE' and dependent containers…"
    docker ps -a --filter "ancestor=$IMAGE" --format '{{.ID}}' | xargs -r docker rm -f
    docker rmi -f "$IMAGE"
    echo ">>> Building Docker image '$IMAGE'"
    cp ./pgclone integration/docker/pgclone
    docker build -t "$IMAGE" integration/docker
    rm -f integration/docker/pgclone
}

# --- build pgclone binary (linux/amd64) ---
echo ">>> Building pgclone binary (linux/amd64)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$BINARY" ./cmd/pgclone

# --- generate SSH key once ---
if [[ ! -f "$KEY_PRIV" || ! -f "$KEY_PUB" ]]; then
  echo ">>> Generating SSH key-pair $KEY_PRIV / $KEY_PUB"
  ssh-keygen -t rsa -b 2048 -N "" -f "$KEY_PRIV" -q
fi

# --- Docker image build / rebuild prompt ---
if docker image inspect "$IMAGE" >/dev/null 2>&1; then
  read -r -p "Docker image '$IMAGE' already exists. Rebuild? [y/N] " rebuild
  if [[ "$rebuild" =~ ^[Yy]$ ]]; then
    build_docker_image
  fi
else
    build_docker_image
fi

# --- ensure network exists ---
docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK"

# --- existing containers check ---
for c in "$PRIMARY_CONTAINER" "$REPLICA_CONTAINER"; do
  if docker ps -a --format '{{.Names}}' | grep -qw "$c"; then
    read -r -p "Container '$c' already exists. Remove and recreate? [y/N] " ans
    if [[ "$ans" =~ ^[Yy]$ ]]; then
      docker rm -f "$c"
    else
      echo ">>> Skipping container '$c'"
      skip_container["$c"]=1
      continue
    fi
  fi
done

# --- start containers ---
echo ">>> Starting containers…"
if [[ -z "${skip_container[$PRIMARY_CONTAINER]:-}" ]]; then
  docker run --init -d \
    --name "$PRIMARY_CONTAINER" \
    --network "$NETWORK" \
    -e POSTGRES_PASSWORD=postgres \
    -e ROLE=primary \
    -v "$KEY_PUB":/tmp/test-key.pub:ro \
    "$IMAGE"

  # append replica public key to authorized_keys inside primary
  docker exec "$PRIMARY_CONTAINER" bash -c "cat /tmp/test-key.pub >> /var/lib/postgresql/.ssh/authorized_keys && chown postgres:postgres /var/lib/postgresql/.ssh/authorized_keys && chmod 600 /var/lib/postgresql/.ssh/authorized_keys"
fi

if [[ -z "${skip_container[$REPLICA_CONTAINER]:-}" ]]; then
  docker run --init -d \
    --name "$REPLICA_CONTAINER" \
    --network "$NETWORK" \
    -e POSTGRES_PASSWORD=postgres \
    -e ROLE=replica \
    -v "$KEY_PRIV":/tmp/id_rsa:ro \
    -v "$BINARY":/usr/bin/pgclone:ro \
    --entrypoint /bin/bash \
    "$IMAGE" \
    -c "sleep infinity"

  # copy private key into postgres home with correct ownership/permissions
  docker exec "$REPLICA_CONTAINER" bash -c "cp /tmp/id_rsa /var/lib/postgresql/.ssh/id_rsa && chown postgres:postgres /var/lib/postgresql/.ssh/id_rsa && chmod 600 /var/lib/postgresql/.ssh/id_rsa"
fi

# --- allow containers to finish init ---
sleep 5

# --- run pgclone ---
echo ">>> Running pgclone inside replica…"
docker exec -u postgres "$REPLICA_CONTAINER" bash -c '
  export PGPASSWORD=postgres
  pgclone \
    --pghost pg-primary \
    --pgport 5432 \
    --pguser postgres \
    --primary-pgdata /var/lib/postgresql/data \
    --replica-pgdata /var/lib/postgresql/data \
    --replica-waldir /var/lib/postgresql/data/pg_wal \
    --temp-waldir /tmp/pg_wal \
    --ssh-key /var/lib/postgresql/.ssh/id_rsa \
    --ssh-user postgres \
    --insecure-ssh \
    --slot \
    --parallel 4 \
    --verbose \
    --progress=plain \
    --progress-interval 1'

echo ">>> Clone finished. Replica PG_VERSION:"
docker exec "$REPLICA_CONTAINER" cat /var/lib/postgresql/data/PG_VERSION

# --- optionally start replica in standby mode ---
read -r -p "Start replica (standby.signal) to verify recovery? [y/N] " start_replica
if [[ "$start_replica" =~ ^[Yy]$ ]]; then
  echo ">>> Starting replica as standby…"
  docker exec -u postgres "$REPLICA_CONTAINER" bash -c "\
    echo \"primary_conninfo = 'host=pg-primary port=5432 user=postgres password=postgres sslmode=prefer application_name=replica1'\" >> /var/lib/postgresql/data/postgresql.auto.conf && \
    touch /var/lib/postgresql/data/standby.signal && \
    /usr/lib/postgresql/15/bin/pg_ctl -D /var/lib/postgresql/data/ start"

  sleep 10
  docker exec -u postgres "$REPLICA_CONTAINER" bash -c '/usr/lib/postgresql/15/bin/pg_ctl -D /var/lib/postgresql/data/ stop'
fi

# --- optional cleanup ---
for c in "$PRIMARY_CONTAINER" "$REPLICA_CONTAINER"; do
  read -r -p "Remove container '$c'? [y/N] " rm_ans
  if [[ "$rm_ans" =~ ^[Yy]$ ]]; then
    docker rm -f "$c"
  else
    echo ">>> Leaving container '$c' running."
  fi
done

echo ">>> Demo completed successfully." 