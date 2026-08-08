# ADR-0002: PostgreSQL plus Milvus

- Status: Accepted
- Date: 2026-08-08

## Decision

Use PostgreSQL as the authoritative workflow, provenance, audit, and graph database. Use Milvus as an asynchronous, rebuildable semantic index. Keep artifacts in content-addressed storage and product source in Git.

## Rationale

The system needs atomic multi-record state transitions: generation plus artifacts, review plus revision, approval plus indexing intent, and repository edge plus indexing intent. PostgreSQL provides transactions and constraints for those invariants. Milvus is optimized for vector retrieval and has a distributed scaling path, but it is not the right authority for approval workflows or edge integrity.

SQLite remains useful for a single-process prototype, but sharing one file across multiple independent sessions introduces writer serialization, migration coordination, backup, and network-filesystem limitations. This repository targets the production-shaped local stack, so it starts with PostgreSQL.

## Consequences

- Search is eventually consistent after approval.
- Every Milvus record must be hydrated and authorized from PostgreSQL.
- Milvus can be rebuilt from PostgreSQL through the outbox/reindex command.
- Operational ownership includes both a relational database and a vector system.
