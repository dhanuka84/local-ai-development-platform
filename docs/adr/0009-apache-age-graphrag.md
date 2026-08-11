# ADR-0009: Apache AGE projection and governed GraphRAG

- Status: Accepted
- Date: 2026-08-11

## Decision

Keep PostgreSQL relational tables authoritative and add Apache AGE inside the
same PostgreSQL 17 deployment as a rebuildable active-topology projection.
Keep Milvus as the rebuildable semantic discovery index. The Go data plane
combines them as:

```text
Ollama embedding -> Milvus seeds -> PostgreSQL hydration
  -> AGE bounded expansion -> PostgreSQL re-hydration -> ranked context
```

The `GraphStore` port has Apache AGE and recursive PostgreSQL implementations.
AGE is selected in the local Compose deployment; stale or unavailable AGE
falls back to recursive SQL. Agents receive only bounded, typed MCP operations
and never unrestricted Cypher.

## Authority and projection rules

- PostgreSQL owns approvals, workflow state, revisions, evidence, identities,
  active code heads, and every graph source record.
- AGE contains `Repository`, active `CodeEntity`, and approved `KnowledgeItem`
  vertices plus typed topology edges.
- Projection-head tables record repository relation versions, active analysis
  runs, and approved knowledge versions. AGE results are rejected when these
  do not match PostgreSQL.
- Milvus indexes approved knowledge, selected active symbols, repository
  relation descriptions, and selected code/knowledge edge descriptions.
- Every AGE or Milvus identifier is hydrated and project-filtered through
  PostgreSQL before it is disclosed.

## Consequences

- `graph_context_search` provides semantic discovery plus exact multi-domain
  traversal under server limits of five hops, 32 seeds, 200 nodes, and 400
  edges; defaults are smaller.
- `admin age-rebuild` deterministically recreates the complete AGE projection.
- AGE can be rolled back by setting `GRAPH_BACKEND=postgres`; no authoritative
  data migration is required.
- AGE may become the unconditional default only after workload benchmarks and
  operational acceptance; the fallback remains available during that period.
