# pgclone (Go Edition)

**pgclone** is a Go rewrite of the original Bash utility (https://github.com/vbp1/pgclone.sh) that prepares a physical PostgreSQL replica using high-speed `rsync` file transfer and live WAL streaming.  It preserves the proven workflow of the shell script.

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
cmd/pgclone             – CLI entry point (Cobra)

# Core workflow
internal/cli            – flag parsing & global config
internal/clone          – orchestrator that coordinates the whole clone pipeline

# Functional subsystems
internal/postgres       – pgx helpers (version checks, tablespaces, wait helpers)
internal/rsync          – rsync list parser, distributor, parallel workers, stats
internal/wal            – pg_receivewal wrapper
internal/ssh            – SSH helpers (remote execution, key setup)

# Infrastructure
internal/runctx         – per-run temp dir lifecycle
internal/lock           – file locking based on gofrs/flock
internal/process        – subprocess helpers & watchdogs
internal/log            – slog logger setup
internal/debug          – low-overhead dev breakpoints (`debug.StopIf`)

# Misc utilities
internal/util/disk      – disk space helpers
internal/util/fs        – filesystem utilities (mkdir-p, safe remove)
internal/util/signalctx – context cancellation on OS signals

# Auxiliary
integration/            – Docker-Compose integration tests (build tag `integration`)
docs/                   – architecture docs & diagrams
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