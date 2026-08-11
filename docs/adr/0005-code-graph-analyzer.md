# ADR-0005: Revisioned code graph and isolated MPL analyzer

**Status:** Accepted

## Decision

Add a headless Go code-analysis component behind a language-neutral `Analyzer`
interface. Persist complete, revision-scoped snapshots in PostgreSQL and
project selected code-entity summaries into Milvus through the transactional
outbox. PostgreSQL remains authoritative for exact traversal.

The reusable graph-domain and incremental-analysis concepts adapted from
Bevel Software's `code-to-knowledge-graph` are isolated under
`components/codegraph` and licensed MPL-2.0. The rest of the platform remains
MIT. The source revision and exclusions are recorded in the component NOTICE.

The native provider analyzes Go using package, syntax, and type information.
SCIP adapters cover Maven/Gradle Java, Gradle Kotlin, TypeScript/JavaScript, and
Python, and every provider emits the same snapshot contract.

## Why this boundary

The product already records reviewed engineering knowledge and exact
repository-to-repository relationships. It lacks the intra-repository graph
that connects a task to packages, files, types, functions, calls, references,
implementations, and tests. A code graph provides that bridge without making
the vector database responsible for topology.

A separate component makes the MPL file-level obligations explicit and avoids
bringing the upstream VS Code HTTP bridge, Neo4j UI, ANTLR grammars, or
description-merging behavior into the production data plane.

## Identity and history

Logical entity identity is derived from repository, language, kind, and
qualified name. Content hashes identify revisions of that entity but do not
define its permanent identity. Each successful analysis creates an immutable
run containing occurrences and relations, then atomically advances the
repository's active head. Failed or partial analysis never replaces the head.

PostgreSQL assigns one UUID to that repository-scoped stable key and reuses it
across revisions. Selected first-party entity embeddings use the same UUID as
their Milvus primary key. Semantic search therefore discovers a stable entity
ID, which is hydrated against the active PostgreSQL occurrence before exact
graph traversal. Edges are not inferred from or made authoritative in Milvus.

Repository catalog identity is independent from code-analysis eligibility.
Documentation-only and unsupported-language repositories are registered as
catalog nodes without creating empty analysis runs. Remote default branch,
checked-out analysis branch, and exact revision are distinct facts. A
non-default branch requires an explicit orchestration override.

The transactional outbox may contain repeated stable UUIDs after corrected or
superseding analyses. Workers hydrate only active entities, batch Ollama
embeddings and Milvus upserts, and coordinate replicas through PostgreSQL row
locking. Administrative compaction completes superseded events without
deleting audit history or the newest active projection intent.

## Consequences

- Local analysis is headless and can run without a cloud model.
- Exact impact analysis is deterministic and revision-aware.
- Milvus may be dropped and rebuilt from active PostgreSQL entities.
- Organization-wide indexing is idempotent at the catalog and active-head
  boundary; asynchronous semantic completion remains separately observable.
- Old snapshots require an explicit retention policy at enterprise scale.
- Language servers and future build-aware analyzers must run with filesystem,
  process, time, and memory limits because repositories are untrusted input.
