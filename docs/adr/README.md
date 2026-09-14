# Architecture Decision Records

Read the [developer guide](../developer-guide.md) for the current system.
ADRs preserve decision history; later records explicitly clarify earlier ones.

The [AI-native expectations](../ai-native-sdlc-expectations.md) define the current
product direction and [implementation assessment](../sdlc-gap-assessment.md)
records its open work. Retain the PostgreSQL/AGE/Milvus, execution/data-plane
and accountable-publication boundaries in these decisions while extending the
product KB, source adapters and agent roles. A target diagram is not evidence
that a proposed ADR capability has shipped.

- [0001 — Go for the MCP and data plane](0001-go-for-the-mcp-data-plane.md)
- [0002 — PostgreSQL plus Milvus](0002-postgresql-and-milvus.md)
- [0003 — Reviewed knowledge promotion](0003-reviewed-knowledge-promotion.md)
- [0004 — Dual repository graph representation](0004-repository-graph.md)
- [0005 — Revisioned code graph and isolated MPL analyzer](0005-code-graph-analyzer.md)
- [0006 — Bounded local execution with advisory cloud review](0006-bounded-local-execution-and-cloud-review.md)
- [0007 — OpenClaw managed agentic workflows](0007-openclaw-managed-agentic-workflows.md)
- [0008 — Cerbos contextual authorization](0008-cerbos-contextual-authorization.md)
- [0009 — Apache AGE projection and governed GraphRAG](0009-apache-age-graphrag.md)
- [0010 — RAG-first atomic task checkpoints](0010-rag-first-atomic-task-checkpoints.md)
- [0011 — Scoped autonomy and local functional acceptance](0011-scoped-autonomy-and-functional-acceptance.md)
- [0012 — Bounded AI-native SDLC execution](0012-bounded-sdlc-runtime.md)
