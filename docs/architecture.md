# pgclone Architecture

```mermaid
graph TB
    classDef hidden fill:transparent,stroke-width:0

    %% Local Machine
    subgraph "Local Machine"
        direction TB
        anchorL(( )):::hidden
        CLI["cmd/pgclone (cobra CLI)"]
        Orch["clone.Orchestrator"]
        RunCtx["runctx.RunCtx (temp dir)"]
        FileLock["lock.FileLock"]
        SSHClient["ssh.Client"]
        WALRcv["wal.Receiver (pg_receivewal)"]
        RsyncRunner["rsync.RunParallel"]
        PgPool["postgres.Connect (pgxpool)"]
        Progress["progress printer"]
        anchorL --> CLI
        CLI --> Orch
        Orch --> RunCtx
        Orch --> FileLock
        Orch --> SSHClient
        Orch --> WALRcv
        Orch --> RsyncRunner
        Orch --> PgPool
        Orch --> Progress
        RsyncRunner --> Progress
    end

    %% Remote Machine
    subgraph "Remote Machine"
        direction TB
        anchorR(( )):::hidden
        Rsyncd["rsync --daemon"]
        PG["PostgreSQL Primary 15+"]
        PGDATA["/var/lib/postgresql/data"]
        anchorR --> Rsyncd
    end

    %% Connections
    RsyncRunner --> Rsyncd
    WALRcv --> PG
    PgPool --> PG
    SSHClient -.->|"SSH bootstrap"| Rsyncd
    Rsyncd <--> PGDATA
    PG --> PGDATA

    %% Alignment helpers
    anchorL --> anchorR:::hidden
```

The diagram shows how the CLI orchestrates components:

1. **File lock** prevents concurrent runs on the same replica data directory.
2. **RunCtx** provides a run-scoped temporary directory automatically cleaned up (unless `--keep-run-tmp`).
3. **wal.Receiver** starts `pg_receivewal` to stream WAL ahead of the file copy.
4. **rsync.RunParallel** boots a transient `rsyncd` on the primary host and spawns parallel workers to copy `base/` and tablespaces.
5. A lightweight `pgx` pool is used for control queries (`pg_backup_start/stop`, waiting for replication, etc.). 