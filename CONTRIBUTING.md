# Contributing

## Workflow

1. Create a focused branch and keep unrelated user changes intact.
2. Add or update tests for behavior changes.
3. Add a numbered SQL migration rather than editing a migration already used outside local development.
4. Run `make check` and validate Compose configuration.
5. Update MCP examples and documentation when tool schemas or safety behavior changes.
6. Explain data migration, Milvus reindex, and rollback requirements in the pull request.

## Design constraints

- PostgreSQL is authoritative; do not introduce direct authoritative Milvus writes.
- All derived-index writes must be retryable and idempotent.
- New MCP write tools need accurate annotations and an approval policy example.
- Never log prompt/response bodies, authorization headers, secrets, or full cloud export packages.
- Maintenance paths must remain local-only and fail closed.
- Repository relationship types should remain a controlled vocabulary; add new types through a migration, code validation, tests, and documentation.

## Commands

```bash
make fmt
make check
make build
make mcp-preflight
make vault-test
make gdrive-test
make backup-restore-test
```

Run `make vault-test` and `make gdrive-test` for vault or Drive-client changes.
Run the Docker-backed `make backup-restore-test` for backup/restore workflow or
Compose secret changes. These tests use disposable credentials and a fake Drive
client; never use real OAuth credentials, refresh tokens, vault files, or cloud
folders in automated tests.
