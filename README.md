# pgclone (Go Edition)

**pgclone** is a Go rewrite of the original Bash utility that prepares a physical PostgreSQL replica using high-speed `rsync` file transfer and live WAL streaming.  It preserves the proven workflow of the shell script while adding type-safe code, rich tests and modern CI/CD.

> The conversational language with the team remains **Russian**.  All code comments, commit messages, branch names and documentation are written in **English** per project policy.

---

## Features

* PostgreSQL 15+ physical replication (no `pg_basebackup` required)
* Parallel `rsync` workers with smart file distribution (ring-hop heuristic)
* Streaming WAL via external `pg_receivewal` (slot is optional)
* Unified progress indicator (TTY bar / plain mode / CI-friendly none)
* Paranoid checksum mode (`--checksum`) for byte-perfect copies
* Graceful shutdown & full cleanup, even on signals
* File locking prevents concurrent runs against the same replica
* 90 %+ unit-test coverage and GitHub Actions CI (lint → unit → integration)

---

## Installation

Prerequisites:

* **Go ≥ 1.23** (see `.tool-versions` / CI matrix)
* Linux AMD64 (other architectures compile but are not CI-tested)
* Standard PostgreSQL client tools in `$PATH` (`psql`, `pg_receivewal`)
* `rsync` ≥ 3.2, `ssh` client

Clone the repository and build the static binary:

```bash
git clone https://github.com/vbp1/pgclone.git
cd pgclone
make build       # produces ./bin/pgclone
```

Run linters and tests locally:

```bash
make lint        # golangci-lint
make test        # go test ./...
```

---

## Quick Start

```bash
export PGPASSWORD=supersecret

./bin/pgclone \
  --pghost primary.example.com \
  --pguser replica \
  --primary-pgdata /var/lib/postgresql/15/main \
  --replica-pgdata /data/replica \
  --ssh-user postgres \
  --ssh-key ~/.ssh/id_ed25519 \
  --slot \            # optional transient slot
  --parallel 8 \       # default = CPU cores
  --progress bar \     # bar|plain|none|auto (default auto)
  --verbose
```

Flags mirror the original Bash script; run `pgclone --help` for the full list.

---

## Directory Layout

```text
cmd/pgclone          – CLI entry-point (cobra)
internal/cli         – flag parsing & config
internal/postgres    – pgx helpers (version check, tablespaces, wait functions)
internal/rsync       – list parser, distributor, parallel runner, stats
internal/wal         – pg_receivewal wrapper
internal/runctx      – per-run temp dir lifecycle
internal/lock        – file lock based on gofrs/flock
internal/util/...    – misc helpers (fs, disk, signalctx, logging)
```

---

## Build & Test Matrix

| Command                | Purpose                          |
|------------------------|----------------------------------|
| `make build`           | Build static binary to `bin/`    |
| `make lint`            | Run `golangci-lint`              |
| `make test`            | Unit tests                       |
| `make docker-test`     | Integration tests via Docker     |

CI workflow (`.github/workflows/ci.yml`) runs the same steps for `linux-amd64` and `linux-arm64`.

---

## License

pgclone is released under the MIT License.  See [LICENSE](./LICENSE) for details. 