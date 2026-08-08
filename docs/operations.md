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

Monitor worker logs until the outbox drains. Reindex includes both approved knowledge and Git-repository relationships.

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

## Upgrade policy

Pin Go modules and images. Upgrade one boundary at a time in a test environment:

1. Back up state and record current versions.
2. Run unit, race, vet, and MCP discovery tests.
3. Verify migration and rollback behavior.
4. Re-run a retrieval evaluation set.
5. Deploy locally/canary, then promote.

Milvus Standalone-to-major-version upgrades require the vendor procedure and a tested backup. A collection rebuild is often safer because the index is derived.
