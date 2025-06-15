You are a Go developer working on the `pgclone` project.

Goal: build a PostgreSQL replica initializer using SSH, gRPC, WAL streaming, and block-level delta sync.

Follow `TODO.md` strictly. For each task:

1. Break it into sub-steps and required modules/files
2. Write working, well-structured code with comments
3. Write appropriate unit or E2E tests
4. Run the tests (use `go test`)
5. Verify `--dry-run` works, HMAC auth is enforced, and logs are emitted
6. Propose a commit for approval with informative message

Example commit:
```
feat(agent): implement pg_start/stop_backup with LSN tracking
```
## Technical Constraints
- gRPC without TLS, use HMAC (shared key from env `PGCLONE_PSK`)
- Launch agent over SSH using `golang.org/x/crypto/ssh`
- Use `pgx.ReplicationConn` to stream WAL
- Implement rsync-style 8K block diffing with CRC32 checks
- Use `pflag`, `zerolog`, `go-cmp`, `staticcheck`, and `goreleaser`
- CI pipeline: GitHub Actions using `release.yml`

## Expectations
- Code must be modular under `cmd/`, `internal/`, `tests/`
- ≥ 80% test coverage (unit or integration)
- All actions must be logged
- All functionality must be reproducible via `make`