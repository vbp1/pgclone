# TODO: Go Port of `pgclone`

This document outlines the high-level milestones and concrete tasks required to re-implement the `pgclone` Bash utility in Go, while preserving functional parity, adding automated tests, and establishing modern CI/CD workflows.

> All commit messages, code comments, and further documentation MUST be written in **English**. The conversational language with the team remains Russian as per project policy.

---

## Phase 0 – Project Meta

| ID | Task | Notes |
|----|------|-------|
| 0.2 | Agree Go tool-chain version (≥1.22) & update `.tool-versions` / CI matrix | |
| 0.3 | Define minimal supported OSes (Linux only) and architectures (amd64 only) | |

---

## Phase 1 – Repository Bootstrap

| ID | Task | Output |
|----|------|--------|
| 1.1 | ✅ `go mod init github.com/vbp1/pgclone` | `go.mod`, `go.sum` |
| 1.2 | ✅ Directory layout (`/cmd/pgclone`, `/internal/...`) | Standard Go project structure |
| 1.3 | ✅ Add lint, formatter & static-analysis tools (golangci-lint, gofumpt) | `.golangci.yml` |
| 1.4 | ✅ Add Makefile (build, test, lint, docker-test) | `Makefile` |

---

## Phase 2 – CLI Skeleton

| ID | Task | Details |
|----|------|---------|
| 2.1 | ✅ Introduce `spf13/cobra` (or std `flag`) for argument parsing | Mirrors current Bash flags |
| 2.2 | ✅ Generate stub commands & global flags | `cmd/pgclone/root.go` |
| 2.3 | Wire JSON/YAML config file support (`viper`) | Optional config loading |

---

## Phase 3 – Infrastructure & Utilities

| ID | Task | Details |
|----|------|---------|
| 3.1 | ✅ Logging subsystem using `log/slog` with levels & timestamps | stdout/stderr separation |
| 3.2 | ✅ Graceful shutdown: context + `os/signal` | Handles `INT`, `TERM`, `ERR` analogues |
| 3.3 | Disk space helpers (wrap `syscall.Statfs`) | Cross-platform abstraction |
| 3.4 | File/dir utilities (`mkdir-p`, cleanup helpers) | |

---

## Phase 4 – Postgres Interaction Layer

| ID | Task | Description |
|----|------|-------------|
| 4.1 | Establish connection pool via `pgx/v5` | ENV-driven credentials |
| 4.2 | Implement helper(s): version check ≥15, tablespace discovery, `pg_size_pretty` equivalence | |
| 4.3 | Persistent psql session replacement → simple `pgx` query helper | streaming results |

---

## Phase 5 – External Process Management

| ID | Task | Description |
|----|------|-------------|
| 5.1 | Wrapper for executing & supervising external commands (`rsync`, `pg_receivewal`) | `exec.Cmd` with context & logging |
| 5.2 | Implement watchdog goroutine (kill children when parent dies) | Replaces Bash watchdog PIDs |
| 5.3 | Parse `rsync --stats` output into struct for later aggregation | Regex/Scanner-based parser + tests |

---

## Phase 6 – SSH Subsystem

| ID | Task | Description |
|----|------|-------------|
| 6.1 | Evaluate using native OpenSSH binary vs Go SSH library (`x/crypto/ssh`) | Prefer library to avoid binary dependency |
| 6.2 | Implement helper to run remote shell commands, capture stdout/stderr, return exit status | Respects `INSECURE_SSH` flag |
| 6.3 | Implement remote `rsyncd` bootstrap: create dir, upload conf & secrets, launch daemon, relay chosen port | Matches Bash logic |

---

## Phase 7 – Data-sync Engine

| ID | Task | Description |
|----|------|-------------|
| 7.1 | Implement file list acquisition via `rsync --list-only` | Collect & sort by size |
| 7.2 | Re-implement “ring-hop heuristic” file distribution across workers | Pure Go algorithm + unit tests |
| 7.3 | Spawn parallel `rsync` workers using calculated file chunks | stream progress via pipes |
| 7.4 | Progress bar integration (mpb) *or* plain TTY output | `--progress-mode` parity |
| 7.5 | Aggregate per-worker stats into global view | real-time & final summary |

---

## Phase 8 – WAL Streaming Wrapper

| ID | Task | Description |
|----|------|-------------|
| 8.1 | Minimal viable product: keep external `pg_receivewal` | Manage directory & logs |
| 8.2 | Wait for replica to appear in `pg_stat_replication` (poll via pgx) | timeout handling |
| 8.3 | Future: research pure Go replication protocol (optional stretch goal) | |

---

## Phase 9 – Cleanup & Safety Features

| ID | Task | Description |
|----|------|-------------|
| 9.1 | Implement temp dir lifecycle & `--keep-run-tmp` flag | `os.RemoveAll` guards |
| 9.2 | File locking for concurrent runs (`flock` ➜ `github.com/gofrs/flock`) | Hash of `REPLICA_PGDATA` path |
| 9.3 | Comprehensive `defer` chain to teardown processes & temp data on exit | mirrors `cleanup()` Bash |

---

## Phase 10 – Testing Strategy

| Layer | Tools | Coverage |
|-------|-------|----------|
| Unit  | `testing`, `testify/require` | algorithms, parsers, error branches |
| SSH   | `go-testcontainers`, mock SSH server | rsyncd bootstrap flows |
| Integration | `docker-compose`, Postgres primary/replica, real `rsync`, real `pg_receivewal` | happy path + failure scenarios |

Additional tasks:
- Generate test fixtures (small PGDATA) for quick unit runs.
- Add GitHub Actions workflow `ci.yml` (lint, test).

---

## Phase 11 – Documentation

| ID | Task | Output |
|----|------|--------|
| 11.1 | Update `README.md` with Go installation & usage examples | English |
| 11.2 | Add architecture diagram (`docs/architecture.md`) | mermaid diagram |

---

### Acceptance Criteria

1. Functional parity with Bash `pgclone` on supported environments.
2. All unit tests ≥90 % statement coverage; integration suite green in CI.
3. Binary distributed as static Linux AMD64 & ARM64, signed checksums.
4. README quick-start executes successfully on vanilla Ubuntu 22.04.

---

> Keep this document updated during development – mark tasks `✅` once completed and append newly discovered tasks under appropriate phases. 