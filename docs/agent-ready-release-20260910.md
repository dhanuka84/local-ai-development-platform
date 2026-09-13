# Agent-ready local release preparation — 2026-09-10

Historical release-preparation receipt. The runtime observations below are from
September 10, not a new live deployment check. Use the
[current scope](agent-ready-gap-checklist.md) and [operations runbook](operations.md)
for subsequent work; live cutover remains deferred.

The implementation and real two-task pilot are validated. The live platform
remains on migration 7. Its coordinated rollout is prepared; the required vault
materialization step could not obtain a passphrase noninteractively. Existing
live gateway, worker and data services remain running. No live candidate was
approved, and no encrypted vault or live credential was replaced.

## Recovery evidence

A local `pg_dump --format=custom --no-owner --no-acl` captured the live database.
The dump and artifact archive are in a private, ignored directory:
`.local/gap-work-evidence/release-recovery-20260910` in the implementation worktree.
They contain sensitive local state and were not sent to a model or cloud store.

| Check | Result |
|---|---|
| PostgreSQL dump SHA-256 | `5f28950e67ed71d2ff8108832f86a420d7dd37818f2c32e9de611dfefbb9823e` |
| CAS archive SHA-256 | `d01fd7c7e1595002038f6f5f5d7440f4672bba01be4004c6d66c87aa727d16c9` |
| Database restore | Passed in a disposable container with no network connectivity |
| Restore engine | Same immutable PostgreSQL/AGE image ID as live, `sha256:429e38f1a4c21dbb65ee8e223dc258a35c8952798b22dc1d11fde25b9609fd4f` |
| Upgrade | Migration 7 → 18 passed using the rebuilt admin CLI |
| Candidate preservation | Five pending, zero approved; identical content/status digest before and after |
| Code preservation | 130,516 code entities before and after |
| Artifact verification | All 14 referenced objects present; every archived CAS object's content matched its filename digest |

The database snapshot was taken before archiving immutable CAS files. Additional
unreferenced live objects may be included. The archive was checked against the
restored database's exact artifact references. It is recovery evidence for this
local release, not a production retention or HA certification.

The first restore attempt correctly failed on a plain PostgreSQL image because
the source uses AGE 1.6.0. The AGE image also pre-initializes a graph schema in its
default database, so the successful rehearsal used a new database created from
`template0` before restoring the archive. Never drop/recreate the live database
to follow this rehearsal. It was performed only in the named disposable restore
container.

## Compatible artifacts

Locally built release tags:

- `hybrid-ai-platform-gateway:agent-ready-20260910` — `gateway-analyzer` target.
- `hybrid-ai-platform-worker:agent-ready-20260910` — worker with Git source checks.
- `hybrid-ai-platform-admin:agent-ready-20260910` — migrations and operator CLI.
- `hybrid-ai-gateway-source-verifier:agent-ready-20260910` — Kubernetes source
  verification without synchronous analyzer toolchains.

Exact image IDs, repository revision and validation log hashes are retained in
`.local/gap-work-evidence/release-manifest-20260910.json` in the implementation
worktree. The Compose base, telemetry overlay and source snapshot
overlay are source-controlled. Render the actual deployment configuration after
vault materialization; compare image IDs, allowed roots, policies and mounted
volumes before replacing containers. Application image builds do not require
decrypting the live vault.

## Cutover procedure after vault unlock

1. In the primary checkout, run `make vault-materialize` interactively, or use
   the existing supported private `VAULT_PASSPHRASE_FILE` mechanism. Do not put
   the passphrase in a prompt or shell argument. Then run `make mcp-preflight`.
2. Render `deploy/compose/compose.yaml` plus `compose.telemetry.yaml`, with the
   release image tags above or their recorded immutable IDs. Keep the current
   data volumes and actual PostgreSQL/AGE image. The PostgreSQL-only fallback
   is not the profile observed in this deployment.
3. Stop gateway and worker to drain protected writes, then take a fresh local
   database/CAS backup. The rehearsal snapshot is not a substitute for a
   cutover snapshot if data has changed.
4. Run the matching admin image's `migrate` command against the existing
   database. Start the matching gateway, worker and private collector together.
   Keep policy files from this release. Do not recreate unrelated data/model
   containers as part of the application cutover.
5. Verify `/healthz`, `/readyz`, authenticated MCP tool discovery, approved-only
   knowledge reads, project isolation, policy denials, worker source/projection
   receipts and local collector export. Confirm the original live candidates
   remain pending and inventory outbox errors before accepting the rollout.
6. If a protected-write check fails, stop protected writes and repair using a
   compatible release. Preserve migrated evidence. Restoring a permissive old
   approval path or deleting new evidence is not a rollback procedure.

`make vault-materialize` was attempted with closed stdin and stopped at the
passphrase prompt. The CLI now returns an actionable error for this case rather
than an EOF traceback. The encrypted vault and its generation checks remain
intact; this work does not substitute secrets extracted from legacy containers
for a validated vault generation.

## Validation and pending capture

The final validation passed `make check`, `make contracts-check`, all 61 Cerbos
policy tests, `make vault-test`, and the complete disposable
PostgreSQL/AGE/Milvus/collector acceptance. The acceptance also exercises
delegated credential boundaries and candidate text quality.
The Kubernetes overlay renders offline; its image was checked as UID 65532 with
a read-only source mount. Cluster-specific deployment checks remain outstanding.

An optional `generation_capture` can record the validated procedure as pending
knowledge: inspect authoritative context; reproduce in isolation; preserve exact
model output and failures; implement bounded repairs; execute scope and negative
tests; apply the user's exact version-bound decision; verify publication and
reuse; rehearse backup restoration and migration. Attach the pilot validation
IDs, complete report, test logs and release-manifest revision. Actual pilot and
advisory review provider/model were local `ollama` / `qwen3.6:35b`. Implementation
assistance was Codex; its exact runtime API model identifier was not exposed.
This proposal is not approval, and no new reusable outcome has been embedded.
