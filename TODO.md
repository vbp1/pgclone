# Development Plan: pgclone

## Stage 1: Initialization and Skeleton
- [ ] CLI: client and agent mode with all arguments and `--dry-run`
- [ ] Config structures (Config)
- [ ] Logging setup (zerolog)
- [ ] Internal API: gRPC between client and agent with HMAC auth
- [ ] Launch agent remotely via SSH with environment and arguments

## Stage 2: PostgreSQL Connectivity
- [ ] Verify PostgreSQL connection from client and agent
- [ ] Implement pg_start_backup / pg_stop_backup with LSN return
- [ ] Stream pg_walreceiver via agent (pgx replication connection)
- [ ] Stream WAL from agent to client over gRPC

## Stage 3: Block-level Delta Sync
- [ ] Detect modified blocks (CRC over 8K chunks)
- [ ] Send only modified blocks via gRPC
- [ ] Client writes blocks into `pgdata`

## Stage 4: Finalization
- [ ] Move WAL from temp to final pg_wal directory
- [ ] Validate data directory: PG_VERSION, postmaster.pid, etc.
- [ ] Ensure clean stop_backup on abnormal exit

## Stage 5: Testing
- [ ] Unit tests for grpcapi, ssh, pgutils, syncer modules
- [ ] E2E test: local SSH and replication client/agent loop
- [ ] Verify `--dry-run` (no file operations, no backups)

## Definition of Done (DoD)
- Test coverage ≥ 80%
- All errors are fail-fast (exit with error)
- Detailed logging: paths, connection status, LSNs, errors
- Pass `make all` (lint, test, build)
- Buildable via `goreleaser`, both local and CI

## --- ADDITIONAL REQUIREMENTS ---
- Authentication between the client and agent is implemented using a PSK (pre-shared key) passed via the `PGCLONE_PSK` environment variable, available to the agent at startup.
- The gRPC connection is unencrypted; security is ensured via HMAC signing of requests using the shared PSK.
- WAL is streamed from the agent to the client in real-time (streaming mode, not batch).
- Block-level delta synchronization is implemented in code (similar to `rsync --inplace`), without calling external binaries.
- The `--dry-run` mode performs the following checks only:
  - SSH connectivity to the master
  - PostgreSQL connectivity for both client and agent
  - Replication stream can be established from the recorded LSN
  - No execution of pg_start_backup/pg_stop_backup, WAL writes, or data file sync
