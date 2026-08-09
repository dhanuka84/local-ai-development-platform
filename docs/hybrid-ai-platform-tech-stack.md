# Recommended Technology Stack for the Hybrid AI Engineering Platform

**Status:** Historical design draft; superseded by the implemented Go/PostgreSQL/Milvus stack  
**Last updated:** 2026-08-08  
**Target:** ASUS Ascent GX10 / NVIDIA GB10, 128 GB unified memory  
**Companion architecture:** [Hybrid OpenClaw, Ollama, Kimi, Codex, and MCP Architecture](./hybrid-openclaw-ollama-kimi-architecture.md)

> This is an archived alternative, retained only for decision history. Do not
> use its Node.js, pgvector, model, version, command, or deployment choices for
> this repository. The executable system uses Go, PostgreSQL, and Milvus as
> described by [ADR-0001](./adr/0001-go-for-the-mcp-data-plane.md),
> [ADR-0002](./adr/0002-postgresql-and-milvus.md), the
> [implementation guide](./implementation-guide.md), and the
> [remote-review learning contract](./remote-review-learning.md).

## 1. Historical recommendation

Use a local-first, single-host production architecture built from:

- NVIDIA DGX OS on the GX10.
- Ollama for local generation and embeddings.
- OpenClaw as the local agent control plane.
- Codex and Kimi as explicit, policy-controlled cloud review lanes.
- Node.js 24 LTS and strict TypeScript for the engineering MCP service and workers.
- The current production-stable MCP TypeScript SDK, pinned in the lockfile.
- PostgreSQL 18 with pgvector for workflow state, metadata, full-text retrieval, and vector retrieval.
- Git-versioned Markdown for canonical knowledge and solution patterns.
- A local SHA-256 content-addressed artifact store for bounded run artifacts.
- Codex lifecycle hooks plus a protected local spool for asynchronous run capture.
- Native `systemd` services and OS accounts rather than Kubernetes.
- Prometheus-compatible metrics, structured journald logs, and Grafana for operations.
- SOPS with age, systemd credentials, Gitleaks, and OS/network isolation for security.

This was the draft recommendation before the Go/PostgreSQL/Milvus decision. It
is not the current production stack. SQLite remains suitable for a single-user
prototype, but this repository starts with PostgreSQL because multiple MCP
clients, review transitions, graph integrity, and outbox publication require a
shared transactional authority.

## 2. Stack summary

| Layer | Primary choice | Purpose |
|---|---|---|
| Hardware | ASUS Ascent GX10, GB10, 128 GB unified memory | Local generation, embeddings, orchestration, and supporting services. |
| Base OS | NVIDIA DGX OS supplied for the GB10 platform | Supported ARM64, driver, CUDA, firmware, and platform baseline. |
| Service manager | `systemd` | Process supervision, sandboxing, credentials, restart policy, and resource limits. |
| Local model runtime | Ollama | Local chat, tool-capable coding inference, and embeddings. |
| Agent orchestrator | OpenClaw Gateway | Agent routing, policies, tools, sessions, model selection, and cloud-review delegation. |
| Local coding model | `qwen3-coder-next` | Development, maintenance, and local solution replay. |
| Local utility model | `qwen3.5:9b` | Classification, extraction, summarization, and low-latency bounded tasks. |
| Embedding model | `qwen3-embedding:4b` | Code and document retrieval for approved knowledge and solution patterns. |
| Cloud architecture reviewer | Kimi K3 through Moonshot | Explicit long-horizon architecture and optimization review. |
| Cloud code reviewer | Codex with an approved ChatGPT-backed review model | Diff-focused independent review and high-quality generation examples. |
| MCP implementation | Node.js 24 LTS, TypeScript, stable official MCP SDK | Typed local interface for review packages, knowledge, capture, and replay. |
| MCP transport | Streamable HTTP on `127.0.0.1:7788/mcp` | One shared local service for OpenClaw, Codex, and workers. |
| Runtime validation | Zod compatible with the pinned MCP SDK | Input, output, configuration, and state-transition validation. |
| Workflow database | PostgreSQL 18 | Transactions, identities, capabilities, review state, capture state, and audit metadata. |
| Vector extension | pgvector | Local cosine-vector search with an HNSW index. |
| Lexical retrieval | PostgreSQL full-text search with GIN indexes | Exact terms, identifiers, error strings, and acronym retrieval. |
| Canonical knowledge | Markdown, YAML frontmatter, Git | Human review, history, provenance, rollback, and portability. |
| Artifact storage | Local SHA-256 content-addressed store with Zstandard compression | Diffs, bounded outputs, manifests, and validation artifacts. |
| Capture integration | Codex hooks plus local append-only spool and worker | Non-blocking capture of prompts, bounded tool summaries, and final outputs. |
| Queue | PostgreSQL outbox/job table | Reliable asynchronous work without Redis, RabbitMQ, or Kafka. |
| Secret management | systemd credentials; SOPS with age for encrypted configuration | Keep tokens out of repositories, process arguments, and global environments. |
| Secret scanning | Gitleaks plus organization-specific rules | Block secrets before review packaging, capture, and KB promotion. |
| Logging | Pino JSON logs to journald | Structured local logs with system-level retention and permissions. |
| Metrics | `prom-client` and Prometheus | MCP, retrieval, capture, model, and workflow metrics. |
| Dashboards | Grafana | Retrieval quality, local-model health, review cost, and pipeline status. |
| Tracing | OpenTelemetry, initially disabled or sampled | Cross-component debugging when metrics and logs are insufficient. |
| Unit tests | Vitest | Fast TypeScript tests and state-machine coverage. |
| Database tests | Ephemeral PostgreSQL with pgvector | Migration, transaction, RLS, FTS, and vector-query validation. |
| Protocol tests | MCP Inspector plus contract fixtures | Tool schemas, authentication, transport, timeouts, and error behavior. |
| E2E tests | Opt-in OpenClaw, Codex-hook, and Ollama integration suites | Prove real client and model behavior without making every CI job expensive. |
| Host automation | Ansible | Reproducible packages, users, directories, firewall, and systemd units. |
| CI | Existing Git forge with a self-hosted ARM64 runner | Build, test, scan, package, and deploy without exporting sensitive repos. |
| Backup | Git remote, `pg_dump`, and encrypted Restic snapshots | Recover canonical knowledge, workflow state, configuration, and artifacts. |

## 3. Host and operating system

Use the vendor-supported NVIDIA DGX OS image for the GX10 rather than replacing its kernel, GPU driver, or CUDA stack with a generic distribution. The platform is ARM64, so every native package, container image, Node dependency, and CI runner must be verified for `linux/arm64`.

For the 4 TB GX10 SKU, use the following storage quota plan. ASUS also offers smaller storage variants; those should use an external encrypted artifact/backup volume and proportionally smaller local caches.

```text
500 GB   OS, packages, service state, and operational headroom
500 GB   Ollama models and model staging
500 GB   repositories, build caches, and local development worktrees
250 GB   PostgreSQL, indexes, and temporary reindex space
500 GB   content-addressed artifacts and capture spool within quotas
250 GB   logs, metrics, and temporary exports
1.5 TB   unallocated growth and filesystem safety margin
```

These are quota boundaries, not preallocated partitions. Backups should use separate encrypted storage or a trusted NAS. Do not treat unused internal capacity as a backup.

Create dedicated service identities:

```text
ollama
openclaw-development
openclaw-maintenance
engineering-mcp
engineering-capture
engineering-publisher
postgres
monitoring
```

Hard-compliance maintenance should run in a separate process or OS account with outbound network denied. The MCP and capture services should also have no internet egress.

## 4. Model and inference stack

### 4.1 Ollama

Run Ollama as a native host service bound to loopback. Do not expose port `11434` publicly.

Recommended model set:

| Model | Purpose | Operating policy |
|---|---|---|
| `qwen3-coder-next` | Primary local coding and maintenance | Start with 64K context; one main generation request at a time. |
| `qwen3.5:9b` | Classification and utility tasks | 16K-32K context; short keep-alive. |
| `qwen3-embedding:4b` | KB and solution-pattern embeddings | Batch during indexing; low concurrency during interactive use. |

`qwen3-coder-next` is approximately a 52 GB Q4 artifact with 79.7B total parameters and roughly 3B active parameters per token. It fits the target but still needs memory headroom for KV cache, the OS, OpenClaw, and supporting services.

Do not run Kimi K3 weights locally on this machine. Use either the direct Moonshot route or the deliberate Ollama Cloud alternative, but classify both as cloud execution.

### 4.2 Model routing

```text
routine development       -> local qwen3-coder-next
maintenance               -> local qwen3-coder-next, no fallback
classification/extraction -> local qwen3.5:9b
embeddings                -> local qwen3-embedding:4b
architecture escalation   -> Kimi K3, explicit sanitized package
diff/code escalation      -> Codex, explicit sanitized package
```

Cloud review is a workflow decision, never a model fallback. Local failure must remain visible.

## 5. OpenClaw and Codex

### 5.1 OpenClaw

Run a pinned OpenClaw release under `systemd`. Use separate agent directories and authentication stores for development, Kimi review, and maintenance.

Required controls:

- Local primary models and empty fallbacks for development and maintenance.
- No cloud credentials or MCP bundle access for maintenance.
- No shell, process, write, or MCP access for the Kimi reviewer.
- Explicit `allowAgents` from development to the Kimi reviewer only.
- MCP tool filters and server-side authorization for development tools.
- Cerbos PDP as the internal contextual authorization decision point; the Go
  gateway authenticates callers, supplies trusted PostgreSQL context, and
  enforces fail-closed decisions.
- Configuration backup and validation before every upgrade.

### 5.2 Codex

Use a local Codex CLI, IDE extension, or app attached to the repository. Configure the engineering MCP server in project-scoped `.codex/config.toml` so unrelated repositories do not inherit it.

Use Codex lifecycle hooks for capture:

- `UserPromptSubmit`: enqueue the bounded input event.
- `PostToolUse`: enqueue allowlisted tool and validation summaries.
- `Stop`: enqueue the latest assistant output and close the turn envelope.

The hook must return quickly. A worker performs redaction, artifact hashing, database writes, embedding, and pattern extraction. Do not parse the transcript file as a stable API.

## 6. Engineering MCP service

### 6.1 Language and runtime

Use Node.js 24 LTS with strict TypeScript. Avoid Node.js Current releases on the production host. Pin Node, package-manager, SDK, and dependency versions.

Recommended package profile:

```text
MCP protocol       official production-stable TypeScript SDK
HTTP               Node native HTTP adapter and Streamable HTTP transport
schemas            Zod version compatible with the selected MCP SDK
PostgreSQL         pg
migrations         node-pg-migrate or reviewed SQL migration runner
logging            pino
metrics            prom-client
tracing            @opentelemetry/api and Node SDK when enabled
tests              vitest
Markdown parsing   unified + remark
code chunking      tree-sitter adapters for supported languages, added gradually
```

Use native ESM, `tsc` for production compilation, and `pnpm` with a committed lockfile. Do not transpile or bundle server dependencies unless deployment testing proves it is necessary.

The official MCP TypeScript repository was transitioning toward a new major SDK during 2026. For production, use the version explicitly marked stable and recommended by the project, pin it exactly, and run client compatibility tests before any major upgrade. Do not track the repository's `main` branch.

### 6.2 Interfaces

Expose only:

```text
POST /mcp      MCP Streamable HTTP
GET  /healthz  process liveness; no dependency or secret details
GET  /readyz   database, schema, canonical revision, and embedding readiness
GET  /metrics  loopback-only Prometheus metrics
```

Bind to `127.0.0.1:7788`. Use distinct scoped bearer tokens for OpenClaw, Codex, the capture worker, maintenance if enabled, and administrative tooling. Store only strong token hashes in the service database.

No general REST administration API is needed. Approval, promotion, and reindex operations should remain local CLIs under separate OS identities.

### 6.3 Process split

Run separate units:

```text
engineering-mcp.service          read approved KB, serve MCP, write workflow state
engineering-capture.service      drain hook spool and assemble run envelopes
engineering-indexer.service      update derived lexical/vector indexes
engineering-publisher@.service   one-shot, human-authorized Git worktree publisher
```

The online MCP service must have read-only access to canonical knowledge. Only the publisher can write a dedicated knowledge worktree.

## 7. Data, knowledge, and retrieval

### 7.1 Canonical knowledge

Use a dedicated Git repository containing Markdown with validated YAML frontmatter:

```text
knowledge/
├── shared/
│   ├── architecture/
│   ├── coding-standards/
│   ├── review-learnings/
│   └── solution-patterns/
├── development/
└── maintenance/
```

Git is the source of truth. PostgreSQL is not the authoring system and must be rebuildable from a known Git revision.

### 7.2 Workflow state

Use PostgreSQL 18 for:

- Review packages, capabilities, manifests, findings, and dispositions.
- Codex generation-run envelopes and validation outcomes.
- Solution-pattern candidates, states, owners, and quality metrics.
- Client identities, token hashes, scopes, and expiry.
- Append-only audit metadata.
- Background-job outbox and idempotency keys.
- Derived chunk metadata, full-text vectors, and embedding references.

Use explicit SQL constraints for state transitions where possible and transactions for every multi-record transition. Application checks supplement database constraints; they do not replace them.

### 7.3 Vector and lexical search

Install pgvector in PostgreSQL. The chosen embedding model can exceed the normal 2,000-dimension HNSW limit for `vector`; pgvector supports HNSW indexing for `halfvec` up to 4,000 dimensions. Detect the actual Ollama embedding length at index initialization and create a matching `halfvec(N)` column and cosine HNSW index.

Use PostgreSQL full-text search in parallel:

```text
metadata and classification filters
  -> GIN full-text candidates for identifiers and exact terms
  -> pgvector cosine candidates for semantic similarity
  -> reciprocal-rank fusion
  -> freshness, validation-success, and scope adjustment
  -> top 2-4 diverse results
```

Do not call PostgreSQL text ranking BM25. Use its native full-text ranking and measure it against the project's evaluation set. Add a local reranker only if evaluation shows a material gain.

Recommended chunking:

- Markdown: heading-aware chunks through a Markdown AST.
- ADRs and patterns: preserve YAML metadata and section roles.
- Code: symbol-aware chunks for explicitly supported languages.
- Runbooks: keep ordered procedures and warnings together.
- Solution patterns: separately embed intent/applicability and recipe/output contract.

### 7.4 Content-addressed artifacts

Store bounded diffs, generated files, manifests, validation reports, and approved exemplar fragments outside PostgreSQL:

```text
/var/lib/engineering-mcp/objects/sha256/ab/<full-hash>
```

Metadata records contain the hash, type, size, compression, classification, owner, creation time, and retention. Use atomic writes and verify the digest after reading. Compress text artifacts with Zstandard.

Raw command output, environment dumps, repository archives, secrets, and production payloads are prohibited.

## 8. Capture and replay pipeline

### 8.1 Capture path

```text
Codex hook
  -> protected append-only spool
  -> capture worker
  -> redaction and classification
  -> PostgreSQL run envelope
  -> content-addressed artifacts
  -> validation and disposition
  -> pattern candidate
```

The spool should use `0700` directory and `0600` file permissions, quotas, atomic rename, event IDs, and short retention. It must remain useful when PostgreSQL is briefly unavailable without blocking Codex.

### 8.2 Replay path

```text
new local input
  -> task normalization
  -> hard metadata filters
  -> hybrid pattern retrieval
  -> bounded replay plan
  -> qwen3-coder-next
  -> deterministic validation
  -> outcome and pattern feedback
```

Similarity is a retrieval signal, not the acceptance test. Validate code with tests, contracts, security checks, and requested behavior. Validate documents with schemas, required sections, citations, and an owner-defined rubric.

## 9. Security stack

### 9.1 Secrets

- Use systemd credentials for runtime token and password delivery.
- Use SOPS with age for encrypted configuration that must live in Git.
- Never store API keys in `openclaw.json`, `.codex/config.toml`, shell history, MCP arguments, or knowledge documents.
- Scan package inputs, captured events, artifacts, and promoted Markdown with Gitleaks and custom restricted-data rules.
- Keep Codex/OpenAI and Moonshot credentials outside the MCP service.

### 9.2 Isolation

- Dedicated OS accounts and directories for each trust role.
- Loopback-only listeners for Ollama, OpenClaw, MCP, PostgreSQL, and metrics.
- `nftables` or systemd network restrictions denying egress for MCP and maintenance.
- Read-only repository and canonical-KB access for MCP.
- Dedicated writable Git worktree for the publisher.
- `NoNewPrivileges`, restricted capabilities, protected system paths, and private temporary directories in systemd units.
- No cloud fallbacks or cloud credentials in maintenance.

### 9.3 Supply chain

- Commit `pnpm-lock.yaml` and verify lockfile changes in review.
- Pin deployment artifacts and container digests where containers are used for tests.
- Generate an SBOM with Syft and scan dependencies/images with Grype, Trivy, or OSV-Scanner according to organizational standards.
- Use Renovate or Dependabot for reviewed dependency updates.
- Run `npm` lifecycle scripts only from reviewed dependencies and builds.

## 10. Observability

Start with:

- Pino JSON logs written to stdout and captured by journald.
- Prometheus-compatible metrics exposed only on loopback.
- Grafana dashboards reading Prometheus.
- PostgreSQL slow-query logging for retrieval and workflow queries.
- Ollama and OpenClaw health probes.

Core metrics:

```text
mcp_requests_total{client,tool,status}
mcp_request_duration_seconds{tool}
review_packages_total{route,classification,status}
capture_events_total{event,status}
capture_worker_lag_seconds
solution_pattern_retrieval_total{result}
solution_pattern_replay_total{validation_status}
retrieval_duration_seconds{stage}
embedding_requests_total{status}
cloud_review_tokens_total{provider}
cloud_review_cost_estimate{provider}
ollama_request_duration_seconds{model}
```

Add OpenTelemetry traces only when a concrete cross-service debugging need appears. Trace metadata must never include raw prompts, secrets, source, or model output by default.

## 11. Testing and quality stack

### 11.1 Required test layers

| Layer | Tools | Minimum coverage |
|---|---|---|
| Unit | Vitest | Schemas, authorization, sanitization, state machines, ranking, and redaction. |
| Property/fuzz | fast-check or equivalent | Path handling, capability scopes, parser bounds, idempotency, and state transitions. |
| Database | PostgreSQL 18 + pgvector test instance | Migrations, constraints, transactions, FTS, HNSW, filters, and backup restore. |
| MCP contract | MCP Inspector and recorded fixtures | Initialization, tools/list, tool calls, auth failures, timeouts, and result limits. |
| Security | Gitleaks fixtures and adversarial package corpus | Secret leakage, symlink escape, oversized input, prompt injection, and scope bypass. |
| Retrieval | Versioned query/source relevance set | Recall, precision, exclusion accuracy, citation correctness, and leakage. |
| Replay | Versioned task and acceptance set | Local task success with patterns versus no-pattern baseline. |
| Integration | Real Ollama and OpenClaw, opt-in | Model routing, fail-closed maintenance, indexing, and MCP projection. |
| Codex capture | Synthetic hook events plus local Codex smoke test | Prompt/tool/output assembly without transcript parsing. |

Coverage percentage alone is not an acceptance gate. State transitions, authorization decisions, negative data-boundary cases, and recovery paths require explicit tests.

### 11.2 CI gates

Every change should run:

```text
format and lint
TypeScript type check
unit and property tests
database migration and integration tests
MCP schema/contract tests
secret scan
dependency and SBOM scan
knowledge frontmatter/schema validation
retrieval regression test for KB or embedding changes
```

Live Kimi, Codex, and large Ollama evaluations should be scheduled or manually approved rather than required on every commit.

## 12. Build, deployment, and operations

### 12.1 Source repository layout

```text
platform/
├── apps/
│   ├── engineering-mcp/
│   ├── capture-worker/
│   ├── indexer/
│   └── publisher-cli/
├── packages/
│   ├── contracts/
│   ├── authz/
│   ├── database/
│   ├── retrieval/
│   ├── sanitization/
│   └── observability/
├── migrations/
├── deploy/
│   ├── ansible/
│   ├── systemd/
│   ├── nftables/
│   └── monitoring/
├── test/
│   ├── contract/
│   ├── security/
│   ├── retrieval/
│   └── replay/
├── pnpm-workspace.yaml
└── pnpm-lock.yaml

engineering-knowledge/
├── knowledge/
├── schemas/
├── evaluations/
└── AGENTS.md
```

Keep platform code and canonical knowledge in separate repositories. They have different owners, retention, review cadence, and rollback requirements.

### 12.2 Deployment model

Use native packages and systemd for production on the GX10. Containers remain useful for CI integration tests and optional monitoring components, but Kubernetes is unnecessary for a single host.

Deployment order:

1. DGX OS updates, backups, and recovery test.
2. Ollama and local model smoke tests.
3. PostgreSQL 18, pgvector, roles, migrations, and backup test.
4. Engineering MCP in read-only mode.
5. Capture spool and worker with pattern promotion disabled.
6. OpenClaw development and maintenance agents.
7. Kimi review route.
8. Codex MCP and capture hooks.
9. Solution-pattern promotion and local replay.
10. Monitoring, SLOs, and production acceptance.

### 12.3 Backup policy

Back up:

- Canonical knowledge Git repository and its remote.
- OpenClaw configuration and non-secret agent policy.
- Encrypted secret configuration, not decrypted runtime material.
- PostgreSQL with regular `pg_dump` plus restore tests.
- Content-addressed approved artifacts and audit records.
- Ansible, systemd, firewall, and monitoring configuration.

Do not back up Ollama model blobs unless bandwidth or availability makes re-pulling impractical. Record model names and immutable digests instead.

## 13. Version policy

Pin exact versions in deployment automation and update deliberately:

- Use Node.js 24 LTS, not Node 26 Current, until a later release is LTS and compatibility is proven.
- Use a supported PostgreSQL major; this design selects PostgreSQL 18 and the latest security/bug-fix minor in that major.
- Pin pgvector and test index recall before and after upgrades.
- Pin the production-stable MCP SDK; do not use a pre-alpha major release.
- Pin OpenClaw, Ollama, Codex CLI, and model digests in the environment manifest.
- Rebuild retrieval indexes after embedding identity, dimension, tokenizer, chunker, or normalization changes.

Maintain a machine-readable environment manifest containing OS image, kernel, NVIDIA driver, CUDA stack, Ollama, models and digests, OpenClaw, Codex CLI, Node, MCP SDK, PostgreSQL, pgvector, and schema revision.

## 14. Components intentionally omitted

Do not add these initially:

- Kubernetes: unnecessary control-plane and GPU/network complexity for one host.
- Kafka or RabbitMQ: PostgreSQL outbox jobs are sufficient at this workload.
- Redis: no justified cache or queue requirement yet.
- Elasticsearch/OpenSearch: PostgreSQL FTS plus pgvector is sufficient for the initial KB size.
- A custom web administration UI: use typed CLIs and reviewable Git changes first.
- Automatic fine-tuning: build and evaluate the approved pattern corpus before training.
- Automatic cloud fallback: cloud use must remain explicit and auditable.
- Raw transcript indexing: unstable format, excessive noise, and unacceptable data-leakage risk.

Introduce an omitted component only when measurements establish a specific bottleneck or missing control.

## 15. MVP substitutions

For an initial single-user proof of concept:

| Production component | MVP substitute | Migration trigger |
|---|---|---|
| PostgreSQL 18 + pgvector | SQLite WAL plus a small separate vector index | Multiple writers, larger corpus, job contention, or stronger audit needs. |
| Prometheus + Grafana | Journald and a health CLI | Production pilot or need for historical SLOs. |
| Ansible | Reviewed install script and systemd units | Second host, rebuild exercise, or production approval. |
| SOPS + systemd credentials | Root-owned environment files | More than one operator or production credentials. |
| Automated pattern publisher | Manual Markdown PR | More than a few patterns per week. |

Do not weaken the cloud boundary, secret scanning, candidate/approval states, or maintenance fail-closed behavior for the MVP.

## 16. Final selection

The best-balanced stack for this platform is:

```text
DGX OS + systemd
OpenClaw + Ollama
Qwen3-Coder-Next + Qwen3.5 9B + Qwen3-Embedding 4B
Kimi K3 + Codex as explicit cloud reviewers
Node.js 24 LTS + TypeScript + stable MCP SDK
PostgreSQL 18 + pgvector + PostgreSQL FTS
Git Markdown canonical knowledge
local SHA-256/Zstd artifact store
Codex hooks + protected spool + capture worker
Pino + Prometheus + Grafana
SOPS/age + systemd credentials + Gitleaks
Ansible + systemd + self-hosted CI runner
```

This stack gives one GX10 a strong local development and maintenance platform, controlled cloud review, reusable Codex-derived solution patterns, and a clean upgrade path without prematurely adding distributed infrastructure.

## 17. Primary references

- [NVIDIA DGX Spark platform and DGX OS](https://www.nvidia.com/en-us/support/dgx-spark/)
- [ASUS Ascent GX10 specifications](https://www.asus.com/in/networking-iot-servers/desktop-ai-supercomputer/ultra-small-ai-supercomputers/asus-ascent-gx10/techspec/)
- [Node.js release status](https://nodejs.org/en/about/previous-releases)
- [Official MCP TypeScript SDK](https://github.com/modelcontextprotocol/typescript-sdk)
- [MCP TypeScript server transports](https://github.com/modelcontextprotocol/typescript-sdk/blob/main/docs/server.md)
- [PostgreSQL versioning and supported releases](https://www.postgresql.org/support/versioning/)
- [pgvector](https://github.com/pgvector/pgvector)
- [Ollama embeddings](https://docs.ollama.com/capabilities/embeddings)
- [Qwen3-Coder-Next on Ollama](https://ollama.com/library/qwen3-coder-next)
- [Qwen3 Embedding on Ollama](https://ollama.com/library/qwen3-embedding)
- [OpenClaw MCP](https://docs.openclaw.ai/cli/mcp)
- [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp)
- [Codex lifecycle hooks](https://learn.chatgpt.com/docs/hooks)
- [SOPS with age](https://github.com/getsops/sops)
- [Gitleaks](https://github.com/gitleaks/gitleaks)
