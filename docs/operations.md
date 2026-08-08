# Operations Runbook

## Normal checks

```bash
make doctor
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
docker compose --env-file .env -f deploy/compose/compose.yaml ps
make logs
```

`healthz` proves that the gateway process can answer. `readyz` checks PostgreSQL, Ollama, and Milvus with a three-second request budget.

## Start and stop

```bash
make up-gpu   # GBX100/GB10 with NVIDIA Container Toolkit
make up       # CPU-only fallback
make down     # retains named volumes
```

Do not use `docker compose down -v` unless deletion of all local database, vector, model, and artifact data is explicitly intended and independently backed up.

## Candidate approval

Agents can use MCP with an approval prompt. Operators can also use the local CLI:

```bash
go run ./cmd/admin approve <candidate-uuid> <actor>
go run ./cmd/admin reject <candidate-uuid> <actor>
```

Approval queues indexing. Search may not return the item until the worker completes the event.

## Rebuild Milvus

Milvus is derived. After restoring or replacing it:

```bash
go run ./cmd/admin milvus-init
go run ./cmd/admin reindex
```

Monitor worker logs until the outbox drains. Reindex includes approved knowledge, Git-repository relationships, and selected first-party code entities from every active repository snapshot.

## Analyze a repository

Set `CODEGRAPH_HOST_ROOT` in `.env` to the host repository or parent directory exposed read-only to the Compose gateway, then restart the gateway. Invoke `code_repository_index` through Codex/OpenClaw with `repository_path=/workspace` (or a child path) and an explicit write approval. Supply the expected commit SHA whenever possible and leave `allow_dirty=false` for reproducible snapshots.

For native STDIO operation, set `CODEGRAPH_ALLOWED_ROOTS` to an OS path-list of permitted roots. Git and the matching Go toolchain must be installed. If dependency disclosure is prohibited, pre-populate the Go module cache or vendor dependencies and run the analyzer environment with `GOPROXY=off`.

## Backup

Back up independently:

1. Git repositories and reviewed configuration.
2. PostgreSQL with `pg_dump` plus regular tested restore procedures.
3. Artifact storage, preserving paths and hashes.
4. OpenClaw/Codex configuration and credential stores using an encrypted secret backup.

Milvus backup is optional when PostgreSQL and the embedding model/version are preserved; rebuilding may be slower than restoring at enterprise scale. Ollama model files are also replaceable but expensive to download.

## Recovery order

1. Restore PostgreSQL and artifacts.
2. Apply any newer migrations.
3. Start Ollama and verify the exact embedding model.
4. Start Milvus and initialize the configured collection.
5. Reindex and start workers.
6. Start the MCP gateway and verify readiness.
7. Reconnect Codex/OpenClaw clients.

## Common failures

| Symptom | Likely cause | Action |
|---|---|---|
| `AUTH_TOKEN is required` | HTTP mode without a token | Set a long random token in `.env`; export the matching client variable. |
| Search reports lexical fallback | Ollama or Milvus unavailable | Run `admin doctor`; inspect service logs; local exact search remains usable. |
| Embedding dimension error | Model and `EMBEDDING_DIMENSION` mismatch | Create a versioned collection with the detected dimension and reindex. |
| Approved item is absent | Outbox lag or worker failure | Inspect worker logs and `outbox_events.last_error`; do not write Milvus manually. |
| OpenClaw emits raw tool JSON | Ollama configured with `/v1` | Use native `baseUrl: http://host:11434` and `api: ollama`. |
| Codex cannot authenticate | Token env not exported to Codex process | Export `HYBRID_AI_MCP_TOKEN`; do not put the token in TOML. |
| Repository graph is empty | Root does not exactly match UUID/URL/name | Use the canonical URL returned by relation upsert. |
| Code analysis path is rejected | Path is outside the resolved allowlist or its bind mount | Update `CODEGRAPH_HOST_ROOT`/`CODEGRAPH_ALLOWED_ROOTS`; never expose a broad filesystem root. |
| Code analysis reports package errors | Required Go version or modules are unavailable | Use the repository's supported Go toolchain and populate its module cache/vendor tree. |
| Symbol search uses lexical fallback | Symbol vectors are queued or Ollama/Milvus is down | Inspect `code_entity.upsert` events and worker logs; SQL graph traversal remains authoritative. |

## Upgrade policy

Pin Go modules and images. Upgrade one boundary at a time in a test environment:

1. Back up state and record current versions.
2. Run unit, race, vet, and MCP discovery tests.
3. Verify migration and rollback behavior.
4. Re-run a retrieval evaluation set.
5. Deploy locally/canary, then promote.

Milvus Standalone-to-major-version upgrades require the vendor procedure and a tested backup. A collection rebuild is often safer because the index is derived.
