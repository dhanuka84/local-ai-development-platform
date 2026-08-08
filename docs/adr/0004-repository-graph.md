# ADR-0004: Dual repository graph representation

- Status: Accepted
- Date: 2026-08-08

## Decision

Store typed, evidence-backed Git-repository nodes and edges in PostgreSQL. Project each edge as an embedding in Milvus using the transactional outbox.

## Rationale

Software products often span application, library, infrastructure, schema, and deployment repositories. Agents need exact questions (dependency path, API direction, deployment grouping) and fuzzy questions (repositories likely affected by a contract change). SQL recursive traversal answers the former with integrity; semantic vectors answer the latter. Neither representation alone serves both safely.

## Consequences

- SQL is authoritative; Milvus relation matches are discovery hints.
- Repository relation writes require evidence and an accountable actor.
- Knowledge and relation documents share a collection but are isolated by `project_id` and `document_type` scalar filters.
- A future graph database may be added as another projection if bounded PostgreSQL traversal becomes a measured bottleneck.
