# Enterprise Deployment

## Invariants

The enterprise deployment changes scale and identity, not semantics:

- PostgreSQL remains authoritative.
- Milvus remains derived and project/tenant filtered.
- Knowledge and repository relations require provenance and approval.
- Maintenance remains local/private with no cloud fallback.
- Cloud review is explicit, minimized, and audited.
- MCP is the typed client boundary.

![Enterprise architecture](diagrams/hybrid-ai-enterprise-architecture.png)

## Target mapping

| Local | Enterprise |
|---|---|
| One gateway container | Stateless gateway replicas, HPA, PodDisruptionBudget. |
| Static bearer token | OIDC/OAuth at API gateway; workload identity/mTLS internally. |
| PostgreSQL container | Managed HA PostgreSQL, multi-AZ, PITR, connection pooler. |
| Milvus Standalone | Milvus Distributed on Kubernetes or managed Zilliz/Milvus. |
| Local artifact volume | Encrypted, versioned S3-compatible object storage. |
| One worker | Independently scaled worker deployment partitioned by tenant/project. |
| PostgreSQL outbox polling | Keep polling initially; add CDC to Kafka/NATS only after measured need. |
| One Ollama daemon | Private GPU inference pool with model routing and quotas. |
| Local Kimi/OpenAI credentials | Central egress broker, DLP, per-project policy, cost limits. |

## Deployment topology

The Kustomize base under `deploy/kubernetes/base` deploys only the application layer. PostgreSQL, Milvus, Ollama/GPU inference, object storage, identity, and ingress are platform dependencies and should be provisioned with their supported operators or managed services.

The gateway uses the MCP SDK's stateless Streamable HTTP mode, allowing ordinary request balancing across replicas. Keep client compatibility tests in the release gate because older clients may negotiate an earlier MCP revision; do not enable server-to-client request features that require a durable session.

## Multi-tenancy

`project_id` is the current logical namespace. Enterprise deployments should derive it from authenticated authorization, not trust a model-supplied value. Add an authorization layer that maps subject and scopes to permitted projects, then enforce it in repository queries and, if needed, PostgreSQL row-level security. Include tenant/project in Milvus partitioning or scalar filters and in object-store prefixes.

## Repository graph at scale

PostgreSQL recursive CTEs are suitable for bounded product graphs. If deep, high-rate topology traversal becomes a measured bottleneck, introduce a graph projection (for example Neo4j) behind a domain interface. PostgreSQL must remain the authority, and updates should flow through the same outbox/CDC pattern. Milvus complements the graph with semantic discovery; it does not replace edge integrity or traversal.

## Availability and consistency

Writes are strongly consistent in PostgreSQL. Milvus indexing is eventual. Track:

- Oldest pending outbox age.
- Attempts and terminal/repeated errors.
- PostgreSQL-to-Milvus projection delay.
- Search fallback rate.
- Embedding latency and dimension errors.
- Candidate approval/rejection/revision rates.
- Cloud review volume, exported bytes, latency, and cost.

The gateway can continue exact PostgreSQL retrieval during a vector outage. Repository semantic search is unavailable, but exact graph traversal remains available.

## Delivery phases

1. Run Compose on one trusted GB10 host and establish retrieval/approval evaluation sets.
2. Move PostgreSQL and artifacts to managed durable services while retaining local Milvus/Ollama.
3. Deploy gateway/workers to Kubernetes behind identity-aware ingress.
4. Move Milvus to distributed mode and reindex from PostgreSQL.
5. Add dedicated GPU inference pools, policy egress broker, observability, and autoscaling.
6. Add CDC/event streaming only when outbox polling no longer meets measured latency/throughput targets.

The visual transition is captured in [hybrid-ai-local-to-enterprise-evolution.png](diagrams/hybrid-ai-local-to-enterprise-evolution.png) and its Mermaid source.
