# Implementation Guide

## Scope

This guide explains how the code fits together. It covers the local system and
the interfaces that stay the same when the system grows into an enterprise
deployment. See the [plain-English glossary](glossary.md) for terms such as
artifact, principal, projection, and outbox.

In simple terms, OpenClaw coordinates work, the Go MCP gateway checks and saves
requests, PostgreSQL holds the official records, Milvus helps find similar
approved records, and Ollama runs local models and creates embeddings.

## Components

| Component | Package/binary | Responsibility |
|---|---|---|
| MCP gateway | `cmd/gateway` | Authenticates clients and exposes typed tools over HTTP or STDIO. |
| Application service | `internal/service` | Checks requests and handles capture, search, approval, and graph operations. |
| Code graph analyzer | `components/codegraph/golang` | Reads Go code and creates repeatable snapshots of symbols and links. |
| PostgreSQL adapter | `internal/postgres` | Saves official state, runs safe transactions, traverses graphs, and manages the outbox. |
| Milvus adapter | `internal/milvus` | Searches approved knowledge, repository links, and selected code symbols by meaning. |
| Ollama adapter | `internal/ollama` | Creates local embeddings in batches through `/api/embed`. |
| Artifact store | `internal/artifacts` | Saves exact prompts, outputs, reviews, and context manifests by SHA-256 hash. |
| Index worker | `cmd/worker` | Reads approved outbox events, creates embeddings, and updates Milvus. |
| Admin CLI | `cmd/admin` | Runs setup, health checks, candidate decisions, and full reindexing. |
| Work-packet verifier | `cmd/workpacket`, `components/workpacket` | Checks task policy and validates a patch in a disposable clone. |
| Authorization | `internal/authorization`, `policies/cerbos` | Asks Cerbos whether an authenticated identity may perform an action. |
| Workflow controller | `automation/openclaw-plugin` | Mirrors managed OpenClaw tasks through MCP without direct database access. |

The domain package contains interfaces, so local implementations can be replaced independently without changing MCP contracts.

The work-packet verifier is separate from the MCP knowledge service. OpenClaw
still controls execution. The verifier only checks policy, patches, and test
commands. It does not call a model, choose a provider, approve knowledge, or
write to PostgreSQL or Milvus.

## Bounded local execution and remote review

For delegated patch work, OpenClaw creates a `hybrid-ai/work-packet/v1`
document before asking the local Ollama worker to generate a patch. Evaluate it
before execution:

```bash
make workpacket-evaluate PACKET=/path/to/work-packet.json
```

After the local worker returns a unified Git patch, validate the patch in a
disposable clone:

```bash
make workpacket-verify \
  PACKET=/path/to/work-packet.json \
  PATCH=/path/to/candidate.patch
```

Checks are stored as exact command-and-argument lists and are not passed to a
shell. The verifier uses a small environment with no cloud API keys. It applies
the patch to the declared Git revision, checks file and size limits, rejects
binary patches, runs time-limited checks, and detects changes made by those
checks. It does not modify the original checkout.

This is local safety isolation, not a hostile-code sandbox. Run the verifier in
an egress-denied container/VM with CPU, memory, process, and filesystem limits
when repositories or validation commands are not fully trusted.

For development packets that request remote review, OpenClaw sends only a
sanitized context package after local verification. `review_record` captures
Codex/Kimi findings and stores the exact response plus context manifest as
immutable artifacts. Accepted recommendations are reproduced locally and
captured as a pending candidate. Only an accountable approval queues embedding
into Milvus. Maintenance packets cannot request cloud review.

See the [remote-review learning design](remote-review-learning.md), the
[capability evaluation](cost-routing-evaluation.md), the
[example packet](../examples/openclaw/work-packet.example.json), and
[ADR-0006](adr/0006-bounded-local-execution-and-cloud-review.md).

## Data ownership

```text
Git source repositories     authoritative product source
PostgreSQL                  authoritative workflow, provenance, graph, approvals
Artifact CAS                authoritative immutable prompt/output/review bytes by SHA-256
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

`review_record` stores reviewer/provider/model provenance. Its `raw_output`
(falling back to `comments`) and JSON `context_manifest` are stored as
content-addressed artifacts, while PostgreSQL stores their SHA-256 references.
A `revise` verdict requires `improved_content` and fresh local
`validation_evidence`; it replaces both fields on a pending candidate and
increments its version. It cannot mutate approved knowledge.
Raw review artifacts are never sent to Milvus. `knowledge_candidate_decide` is
the explicit gate. Approval, its audit review row, and the `knowledge.upsert`
outbox event commit in one transaction.

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

`code_repository_index` analyzes checked-out Go, Java, Kotlin, TypeScript, JavaScript, and Python repositories below `CODEGRAPH_ALLOWED_ROOTS`. A language router runs every applicable provider for a mixed-language repository. The service resolves symlinks before checking the allowlist, verifies the requested Git branch and revision, rejects a dirty worktree unless explicitly permitted, and applies file/entity/relation caps. The analyzers do not use an LLM or execute the repository's application entry points.

Go uses compiler APIs directly. The other providers import the official SCIP protocol: `scip-java` covers Maven/Gradle Java and Gradle Kotlin, `scip-typescript` covers TypeScript and JavaScript, and `scip-python` covers Python. They run on a disposable copy so generated files, downloaded dependencies, and build output cannot modify the allowlisted checkout. Maven and Gradle may execute build plugins inside the analyzer container; TypeScript dependency installation disables lifecycle scripts, and Python indexing does not install repository packages. Kotlin projects built only with Maven are not supported by `scip-java`. Maven indexing activates a root `deploy` profile when present to cover modules outside the default reactor, but executes the `install` lifecycle rather than publishing artifacts and skips release signing, source archives, and Javadoc generation. A dedicated named volume caches downloaded dependencies; successful Maven analysis also installs the disposable build's artifacts there so dependent sibling repositories can be indexed afterward.

Each successful run records the repository name, checked-out branch, and exact commit (or explicit dirty-worktree fingerprint). It contains modules, packages, files, types, interfaces, functions, methods, fields, variables, constants, and tests plus typed `contains`, `defines`, `imports`, `calls`, `references`, `implements`, `embeds`, and `tests` edges. PostgreSQL writes the entire run and advances `code_repository_heads` in one transaction. A failed or partial analysis cannot replace the active graph. Search and traversal results repeat the repository, branch, and revision mapping so similarly named symbols from sibling repositories remain attributable.

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

The HTTP MCP handler is stateless, limits request bodies to 4 MiB, propagates cancellation, and uses origin protection. Bearer tokens are SHA-256 hashed and resolved to active PostgreSQL principals; plaintext tokens are not stored. STDIO mode writes protocol bytes only to stdout; logs go to stderr.

Static bearer authentication is intentionally a local deployment mechanism. The gateway now sends the authenticated principal and PostgreSQL-hydrated resource context to an internal Cerbos PDP and fails protected actions closed. Enterprise deployments terminate OAuth/OIDC at a trusted gateway and use workload identity internally; see [ADR-0008](adr/0008-cerbos-contextual-authorization.md).

## Search availability

Knowledge search attempts Ollama embedding and Milvus first. If either fails and `SEARCH_LEXICAL_FALLBACK=true`, it performs approved-only PostgreSQL full-text/substring search and returns `backend: postgres-lexical-fallback`. This preserves local maintenance usability while making degradation visible. Repository relation semantic search has no lexical fallback; callers can use exact graph traversal.

## Configuration

Configuration is environment-only and validated at startup. Important values:

| Variable | Default | Notes |
|---|---|---|
| `MCP_TRANSPORT` | `http` | `http` or `stdio`. |
| `AUTH_MODE` | `token` | `none` is rejected outside local mode. |
| `AUTH_TOKEN` | none | Local human credential; defaults to Development, QA, Product Owner, and Operations roles. |
| `CONTROLLER_AUTH_TOKEN` | none | Separate non-human OpenClaw controller credential; must differ from `AUTH_TOKEN`. |
| `AUTHORIZATION_MODE` | `none` locally, `cerbos` otherwise | Compose sets `cerbos`; `none` is rejected outside local mode. |
| `CERBOS_ADDRESS` | `127.0.0.1:3593` | Internal PDP address; never publish it to an untrusted network. |
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
| `CODEGRAPH_JVM_INDEXER_COMMAND` | `/usr/local/bin/hybrid-index-jvm` | Disposable-copy Java/Kotlin SCIP adapter. |
| `CODEGRAPH_TYPESCRIPT_INDEXER_COMMAND` | `/usr/local/bin/hybrid-index-typescript` | Disposable-copy TypeScript/JavaScript SCIP adapter. |
| `CODEGRAPH_PYTHON_INDEXER_COMMAND` | `/usr/local/bin/hybrid-index-python` | Disposable-copy Python SCIP adapter. |

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
- Local bearer principals and Cerbos authorization are implemented. Enterprise
  identity federation, token lifecycle administration, and workload identity
  still belong at the OIDC-aware gateway.
- Repository relationship discovery is explicit. An automated scanner can propose edges from manifests later, but proposals should still require evidence and approval.
- The local MCP gateway performs analysis synchronously. Enterprise scale requires queued jobs and sandboxed analyzer workers; OpenClaw should orchestrate those workers, not generate graph facts itself.
- Removed symbols can leave stale Milvus rows until collection rebuild; active PostgreSQL hydration prevents them from being returned as facts.
- PostgreSQL integration tests require a running service; unit tests isolate pure application and transport behavior.
