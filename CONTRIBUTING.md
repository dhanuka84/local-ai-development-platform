# Contributing

Start with the [developer guide](docs/developer-guide.md) for architecture,
setup, feature development and requirement-level E2E coverage.

Use the [AI-native scope](docs/ai-native-sdlc-expectations.md) and
[prioritized gaps](docs/sdlc-gap-assessment.md) to select an increment. Map its
agent responsibility, structural/semantic KB inputs and outputs, MCP/source
contracts, authority and audit evidence to an expectation and acceptance test.
Describe target capabilities separately from implemented behavior.

## Workflow

1. Create a focused branch and keep unrelated user changes intact.
2. Add meaningful tests for behavior changes, including E2E coverage for each
   affected functional requirement. Keep the checklist and coverage map aligned.
3. Add a numbered SQL migration rather than editing a migration already used outside local development.
4. Run `make check`; run `make agent-ready-functional` for agent-ready behavior
   and the relevant policy/contract/integration checks. Validate affected Compose profiles.
5. Update MCP examples, current guides and diagrams when behavior changes.
   Run `make diagrams` after Mermaid edits and `make docs-check` before handoff.
6. Explain data migration, Milvus reindex, and rollback requirements in the pull request.

## Design constraints

- Follow the [Go readability style guide](docs/go-readability-style-guide.md)
  and the [repository adoption notes](docs/go-readability-adoption.md).
  Cite GOR rule IDs in substantive reviews. Check cancellation, error causes,
  cleanup, ordering and nil/empty contracts before accepting a modernization.
- PostgreSQL is authoritative; do not introduce direct authoritative Milvus writes.
- All derived-index writes must be retryable and idempotent.
- New MCP write tools need accurate annotations and an approval policy example.
- New source tools need bounded queries, source/version/time provenance,
  classification, partial-result handling and allow/deny tests. Validated source
  observations and generated interpretations have different ingestion rules;
  generated KB entries retain their explicit exact-version approval gate.
- Bind agent roles to workload identity, delegated authority and an accountable
  owner. Record reads, writes, decisions and failures through the relevant
  service boundary; a role prompt or model review grants no authority.
- Never log prompt/response bodies, authorization headers, secrets, or full cloud export packages.
- Maintenance paths must remain local-only and fail closed.
- Repository relationship types should remain a controlled vocabulary; add new types through a migration, code validation, tests, and documentation.

## Commands

```bash
make fmt
make check
make build
make agent-ready-functional
make docs-check
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
