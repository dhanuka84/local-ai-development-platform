# ADR-0004: Dual repository graph representation

- Status: Accepted
- Date: 2026-08-08

## Decision

Store the complete Git-repository catalog plus typed, evidence-backed edges in
PostgreSQL. Project each edge as an embedding in Milvus using the transactional
outbox. Keep forge default branch metadata distinct from the branch and exact
revision selected for code analysis.

## Rationale

Software products often span application, library, infrastructure, schema, and deployment repositories. Agents need exact questions (dependency path, API direction, deployment grouping) and fuzzy questions (repositories likely affected by a contract change). SQL recursive traversal answers the former with integrity; semantic vectors answer the latter. Neither representation alone serves both safely.

## Consequences

- SQL is authoritative; Milvus relation matches are discovery hints.
- Catalog membership does not require supported source. Documentation-only or
  unsupported repositories remain visible without manufacturing an empty code
  graph.
- Code-analysis branch overrides do not rewrite forge default-branch metadata.
- Repository relation writes require evidence and an accountable actor.
- Knowledge and relation documents share a collection but are isolated by `project_id` and `document_type` scalar filters.
- Apache AGE was later accepted as a rebuildable traversal projection in
  [ADR-0009](0009-apache-age-graphrag.md); this relational representation and
  its recursive traversal remain the authority and fallback.
