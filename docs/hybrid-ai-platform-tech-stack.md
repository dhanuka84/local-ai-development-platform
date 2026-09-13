# Implemented technology stack

Updated September 12, 2026. This page describes the checked-in stack and the
reason for each choice. Versions below are repository pins, not claims about
the latest upstream release. The [developer guide](developer-guide.md) explains
how to build and change it; the [original architecture](hybrid-openclaw-ollama-kimi-architecture.md)
retains the earlier design proposals.

## Runtime and development choices

| Concern | Implemented choice | Why / authoritative configuration |
|---|---|---|
| MCP and data plane | Go; module Go 1.25.8, toolchain Go 1.26.8 | Typed contracts, concurrency and small binaries; [go.mod](../go.mod), [ADR-0001](adr/0001-go-for-the-mcp-data-plane.md) |
| MCP protocol | Official Go SDK 1.7.0; Streamable HTTP or STDIO | Shared typed tool boundary for clients; `internal/mcpserver` |
| Durable records | PostgreSQL | Transactions bind versions, decisions, audit and outbox; `internal/postgres`, `migrations` |
| Property graph | Apache AGE 1.6.0 / PostgreSQL 17 in Compose | Rebuildable active topology; recursive SQL fallback; [ADR-0009](adr/0009-apache-age-graphrag.md) |
| Semantic index | Milvus 2.6.21 Standalone; Go client 2.6.5 | Derived knowledge/definition/repository/code indexes with SQL UUIDs |
| Milvus dependencies | etcd and MinIO | Included in the Compose profile and consistent backup set |
| Local inference | Ollama 0.32.6 container | Explicit local coding and embedding endpoints; no hidden cloud fallback |
| Coding model default | `qwen3.6:35b` | Configured local development choice; pull separately and evaluate on actual work |
| Embedding default | `embeddinggemma`, 768 dimensions | One local embedding contract; model/dimension changes require a compatible collection and reindex |
| Authorization | Cerbos 0.54.0 | Trusted actor/resource checks with policy fixtures and durable decision correlation |
| Evidence | Local SHA-256 content-addressed files plus PostgreSQL references | Exact immutable prompt/output/review/validation bytes; not automatically published knowledge |
| Asynchronous work | PostgreSQL outbox and Go worker | Retryable indexing, source checks and evidence export without a separate event bus |
| Source analysis | Compiler-aware Go plus SCIP adapters | Deterministic revisioned facts for Go, JVM, TypeScript/JavaScript and Python |
| Bounded validation | Go work-packet verifier, Git and declared executable checks | Applies a scoped patch to an exact revision in a disposable clone |
| OpenClaw adapter | TypeScript plugin; tested against OpenClaw `2026.7.1-2` | Integration boundary only; Go/PostgreSQL remain authoritative |
| Local credentials | Python vault, scrypt and AES-256-GCM; private tmpfs files | Independent operator/controller/database credentials without plaintext secrets in Git |
| Trace export | Durable SQL queue and optional OpenTelemetry collector | Actual collector export is covered by the disposable E2E profile |
| Backup | Cold volume snapshot, authenticated encryption and direct Google Drive client | Consistent PostgreSQL/Milvus dependencies/artifact bundle; [runbook](manual-backup-restore-postgres-milvus-google-drive.md) |
| Diagrams | Mermaid CLI 11.16.0 and local Chrome | Editable source, reproducible SVG/PNG exports and digest checks |

The full container pins and service dependencies live in
[Compose](../deploy/compose/compose.yaml). Controller dependencies live in its
[package manifest](../automation/openclaw-plugin/package.json). Read these
files when upgrading rather than copying versions from a dated experiment.

## What is authoritative

Git holds product source, policies, contracts, documentation and migrations.
PostgreSQL holds runtime KB content, definition versions, approvals, source
bindings, graphs, workflow events, traces and projection intent. The artifact
store holds exact evidence bytes referenced by hash.

AGE and Milvus are rebuildable projections. Semantic matches are candidates:
the service hydrates current PostgreSQL records and checks project, version,
approval, source and projection eligibility before returning them. There is no
automatic write-back of generated KB entries into a Git wiki.

![Current local deployment](diagrams/hybrid-ai-local-architecture.png)

## Publication and model routes

`generation_capture` always creates a pending KB entry. Source-bound local
validation and the user's explicit exact-version decision are required before
publication. Raw cloud reviews remain evidence; local reproduction and
generalization happen before any proposed KB entry is approved.

Domain, capability and metric definitions have a separate registry lifecycle.
Standing task authorization permits validated definition decisions under the
operator's existing roles, with exact hashes, validation IDs and attributed
reasons. It does not approve generated entries. `AUTO_APPROVE_LOCAL=true` is
rejected in every environment.

Ollama owns local execution in governed tasks. A strong approved RAG hit skips
cloud review. A policy-allowed development miss enters the read-only Codex lane;
maintenance and protected-data tasks remain local. Kimi is an optional advisory
integration, not a local fallback. The automatic context packager is still
planned. See [Remote Review and Local Learning](remote-review-learning.md).

## Verification stack

| Layer | Repository command | Scope |
|---|---|---|
| Formatting, vet, race and script regressions | `make check` | Required before every handoff; service-dependent tests may skip here |
| Complete local functional acceptance | `make agent-ready-functional` | Actual gateway, worker, CLI and pilot with disposable PostgreSQL/AGE/Milvus/Cerbos/collector |
| Migration and relational integration | `make integration-test-fresh` | Newly provisioned disposable PostgreSQL |
| Policy and contracts | `make authz-policy-test`, `make contracts-check` | Real policy compiler and positive/negative schemas |
| Controller integration | `make openclaw-plugin-check`, `make openclaw-config-check` | TypeScript/Vitest and pinned OpenClaw configuration |
| Credentials and backup | `make vault-test`, `make gdrive-test`, `make backup-restore-test` | Disposable secrets, fake Drive client and temporary volumes |
| Documentation | `make diagrams`, `make docs-check` | Mermaid rendering, local links/anchors and source/export digests |

The [September 12 receipt](agent-ready-validation-20260912.md#functional-acceptance-continuation)
records 28/28 local functional requirements, 11 suites and 132 passing
test/subtest results. Synthetic Ollama protocol responses make that suite
deterministic; it does not benchmark inference or certify a real cloud session.

## Proposed components and deployment work

The earlier stack proposal included a TypeScript MCP service, pgvector/QMD as
the main retrieval store, Git as the runtime KB authority, a protected hook
spool, compression, a standalone model router, reranker and preinstalled
Prometheus/Grafana. Those are not the implemented local runtime. Capture is a
typed service operation; evidence is stored in local CAS and PostgreSQL; the
worker processes durable jobs. The telemetry overlay is optional.

The Kubernetes base supplies application manifests. Enterprise OIDC/workload
identity, managed HA data services, object-storage adapter, isolated analyzer
job service, disclosure broker and target recovery procedures remain
deployment or implementation work. An event bus, reranker or extra model
service should be justified by measured requirements before being added.

See [Enterprise Deployment](enterprise-deployment.md) and the
[deferred acceptance register](agent-ready-gap-checklist.md#functional-scope-of-the-six-deferred-items).
No benchmark in this repository establishes a cost-saving percentage, broad
autonomy stage or production availability target.
