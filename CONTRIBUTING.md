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
docker compose --env-file .env.example -f deploy/compose/compose.yaml config --quiet
```
