# Implementation Guide

## Scope

This is the canonical technical description of the code in this repository. It covers the local implementation and the contracts that remain stable when the platform is deployed as an enterprise distributed system.

## Components

| Component | Package/binary | Responsibility |
|---|---|---|
| MCP gateway | `cmd/gateway` | Typed agent boundary, authentication, health endpoints, Streamable HTTP/STDIO. |
| Application service | `internal/service` | Validation, capture, retrieval, approval, graph, and fallback policy. |
| Code graph analyzer | `components/codegraph/golang` | Headless Go module/package/type analysis and deterministic snapshots. |
| PostgreSQL adapter | `internal/postgres` | Transactions, workflow state, full-text fallback, graph traversal, outbox. |
| Milvus adapter | `internal/milvus` | Derived vector collection for knowledge, repository relationships, and selected code entities. |
| Ollama adapter | `internal/ollama` | Batched local embeddings through `/api/embed`. |
| Artifact store | `internal/artifacts` | Immutable content-addressed prompt/output blobs. |
| Index worker | `cmd/worker` | Claims outbox events, embeds authoritative records, updates Milvus. |
| Admin CLI | `cmd/admin` | Migrations, collection initialization, dependency checks, decisions, reindex. |

The domain package contains interfaces, so local implementations can be replaced independently without changing MCP contracts.

## Data ownership

```text
Git source repositories     authoritative product source
PostgreSQL                  authoritative workflow, provenance, graph, approvals
Artifact CAS                authoritative immutable prompt/output bytes by SHA-256
Milvus                      disposable semantic projection
OpenClaw/Codex sessions     transient agent context, never canonical knowledge
```

Milvus IDs are PostgreSQL UUIDs. Search first returns vector IDs and scores; the application then hydrates content from PostgreSQL and drops anything no longer approved or present in the active code snapshot. This prevents a stale vector record from overriding workflow or graph state.

## Capture and knowledge promotion

`generation_capture` requires a project, original prompt, and generated response. High-quality captures should also contain:

- Session ID, task type, provider, and model.
- Repository commit or release revision.
- Ordered procedure and important tool actions.
- Validation evidence such as test commands and outcomes.
- Language, tags, summary, and success/partial/failure outcome.

Prompt and response bytes are written to the local content-addressed store before the database transaction. PostgreSQL stores their hashes and locations. It creates a pending knowledge candidate that includes the problem, procedure, response, and validation evidence.

`review_record` stores reviewer/provider/model provenance. A `revise` verdict with `improved_content` replaces the content of a pending candidate and increments its version; it cannot mutate approved knowledge. `knowledge_candidate_decide` is the explicit gate. Approval, its audit review row, and the `knowledge.upsert` outbox event commit in one transaction.

The worker claims outbox rows using `FOR UPDATE SKIP LOCKED`. Failed events retain the error and use bounded incremental backoff. A lock older than five minutes is reclaimable after a crashed worker. Milvus can be dropped and rebuilt with `admin reindex`.

## Regenerating similar outputs locally

The system aims for functionally similar validated outcomes, not token-identical reproduction.

1. Normalize the new task and identify the product/project.
2. Traverse repository relationships when cross-repository impact is possible.
3. Search approved patterns semantically; the PostgreSQL full-text fallback handles identifiers and error strings if vector services are unavailable.
4. Hydrate the pattern's original problem, solution, ordered procedure, validation evidence, and repository revision.
5. Ask the local coding model to adapt the pattern to current source and constraints. Stored output is an exemplar, not text to paste blindly.
6. Inspect current code, apply the change, and repeat the recorded validation plus task-specific checks.
7. Capture the new successful run as another pending candidate. This creates a feedback loop without self-publishing model output.

For exact reproduction, store a deterministic generator/template and its version as source code. A language model plus retrieval cannot guarantee byte-identical output.

## Git repository graph

`software_repositories` stores canonical URLs, names, branches, and observed revisions within a project. `repository_relations` stores directed typed edges:

- `depends_on`
- `provides_api_to`
- `deploys_with`
- `shares_contract`
- `fork_of`
- `upstream_of`
- `successor_of`
- `contains`
- `related_to`

Each edge requires evidence and an accountable approval identity. Evidence should identify something verifiable: a module manifest, API client import, deployment descriptor, submodule declaration, Git fork/upstream record, shared schema, or reviewed architecture decision.

`repository_graph_get` uses a bounded recursive PostgreSQL CTE and accepts a repository UUID, canonical URL, or exact name. `repository_relation_search` embeds the relationship text and searches Milvus. The relation upsert and its indexing event are transactional in PostgreSQL; Milvus catches up asynchronously. Exact topology and integrity decisions must use the SQL graph, never vector similarity.

Both approved knowledge and repository edges share one Milvus collection with a dynamic `document_type` field. Searches always filter by `project_id` and `document_type`, preventing relation vectors from entering answer-pattern results.

## Source-code graph

`code_repository_index` analyzes a checked-out Go repository below `CODEGRAPH_ALLOWED_ROOTS`. The service resolves symlinks before checking the allowlist, verifies the requested Git revision, rejects a dirty worktree unless explicitly permitted, and applies file/entity/relation caps. The analyzer does not use an LLM and does not execute repository programs.

Each successful run contains modules, packages, files, types, interfaces, functions, methods, fields, variables, constants, and tests plus typed `contains`, `defines`, `imports`, `calls`, `references`, `implements`, `embeds`, and `tests` edges. PostgreSQL writes the entire run and advances `code_repository_heads` in one transaction. A failed or partial analysis cannot replace the active graph.

Logical identities use a repository-scoped stable key composed from language, entity kind, and qualified name. `code_entities` reuses its UUID across analysis revisions. Milvus stores selected first-party types, interfaces, functions, methods, and tests—including signatures, source paths, and available documentation—using that UUID as the vector primary key:

```text
semantic query → Milvus entity UUID → active PostgreSQL occurrence → exact SQL graph traversal
```

All graph edges remain in PostgreSQL. Milvus is used to discover a likely starting symbol, never to infer topology. `code_symbol_search` reports whether Milvus or PostgreSQL lexical fallback supplied the result. `code_graph_get` accepts an entity UUID, stable key, qualified name, or exact name and performs a bounded bidirectional traversal of the active snapshot.

Milvus code searches always filter by project and `document_type`; when the caller supplies `repository_id`, that scalar filter is pushed into the vector query rather than applied only after retrieval. PostgreSQL hydration still rechecks the repository and active snapshot.

The reusable analyzer boundary is isolated under `components/codegraph` and MPL-2.0. PostgreSQL, Milvus, MCP, worker, and policy integrations remain in the MIT portion of the platform.

## MCP contract and safety

The service uses the official Go MCP SDK. Input and output schemas are inferred from typed structs. Tool annotations distinguish read-only and additive writes. Codex is configured with `default_tools_approval_mode = "writes"`; approval, repository-relationship, and code-index writes have explicit prompt overrides. `code_repository_index` is registered only when synchronous local analysis is enabled; code search and graph traversal remain available in query-only enterprise gateways.

HTTP endpoints:

| Endpoint | Behavior |
|---|---|
| `POST /mcp` | Stateless MCP Streamable HTTP with JSON responses; bearer token required in HTTP mode. |
| `GET /healthz` | Process liveness only. |
| `GET /readyz` | Bounded dependency checks; returns 503 when degraded. |

The HTTP MCP handler is stateless, limits request bodies to 4 MiB, propagates cancellation, uses origin protection, and applies constant-time token comparison. STDIO mode writes protocol bytes only to stdout; logs go to stderr.

Static bearer authentication is intentionally a local deployment mechanism. Enterprise deployments terminate OAuth/OIDC at a trusted gateway and use workload identity internally.

## Search availability

Knowledge search attempts Ollama embedding and Milvus first. If either fails and `SEARCH_LEXICAL_FALLBACK=true`, it performs approved-only PostgreSQL full-text/substring search and returns `backend: postgres-lexical-fallback`. This preserves local maintenance usability while making degradation visible. Repository relation semantic search has no lexical fallback; callers can use exact graph traversal.

## Configuration

Configuration is environment-only and validated at startup. Important values:

| Variable | Default | Notes |
|---|---|---|
| `MCP_TRANSPORT` | `http` | `http` or `stdio`. |
| `AUTH_MODE` | `token` | `none` is rejected outside local mode. |
| `DATABASE_URL` | local PostgreSQL | Required for all durable operations. |
| `OLLAMA_URL` | `http://127.0.0.1:11434` | Native API base without `/v1`. |
| `OLLAMA_EMBEDDING_MODEL` | `embeddinggemma` | Must match the configured dimension. |
| `EMBEDDING_DIMENSION` | `768` | A dimension change requires a new collection name/reindex. |
| `MILVUS_COLLECTION` | `approved_knowledge_v1` | Version the name when schema/embedding changes. |
| `AUTO_APPROVE_LOCAL` | `false` | Leave false for shared or production use. |
| `SEARCH_LEXICAL_FALLBACK` | `true` | Returns a visible backend marker. |
| `CODEGRAPH_ENABLED` | `true` locally | Enables the synchronous local indexing tool; defaults false in enterprise mode. |
| `CODEGRAPH_ALLOWED_ROOTS` | `.` | OS path-list of roots the analyzer may read; set explicitly for services. |
| `CODEGRAPH_MAX_FILES` | `5000` | Hard cap per analysis request. |
| `CODEGRAPH_MAX_ENTITIES` | `200000` | Hard cap per analysis request. |
| `CODEGRAPH_MAX_RELATIONS` | `1000000` | Hard cap per analysis request. |

## Schema and embedding evolution

PostgreSQL migrations are embedded and transactionally recorded in `schema_migrations`. Never edit an already deployed migration; add a new numbered migration.

Milvus is intentionally versioned by collection name. To change the embedding model or dimension:

1. Choose a new collection name such as `approved_knowledge_v2`.
2. Deploy worker instances configured for the new model and collection.
3. Run `admin milvus-init` and `admin reindex`.
4. Measure retrieval quality and event lag.
5. Move gateways to the new collection.
6. Retain the previous collection for rollback, then remove it under a reviewed retention procedure.

## Known boundaries

- The current local artifact store does not compress blobs; content addressing and permissions are implemented. Enterprise object storage should add encryption, retention, and lifecycle policies.
- The local MCP token represents one trust domain. Per-user authorization belongs at the enterprise gateway.
- Repository relationship discovery is explicit. An automated scanner can propose edges from manifests later, but proposals should still require evidence and approval.
- The local MCP gateway performs analysis synchronously. Enterprise scale requires queued jobs and sandboxed analyzer workers; OpenClaw should orchestrate those workers, not generate graph facts itself.
- Removed symbols can leave stale Milvus rows until collection rebuild; active PostgreSQL hydration prevents them from being returned as facts.
- PostgreSQL integration tests require a running service; unit tests isolate pure application and transport behavior.
