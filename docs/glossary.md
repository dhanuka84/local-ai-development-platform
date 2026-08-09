# Plain-English Glossary

Use this page when a design or operations document uses an unfamiliar term.

| Term | Meaning in this platform |
|---|---|
| Agent | A model-driven worker that receives a task and can use allowed tools. |
| Approval gate | A point where the workflow must wait for an allowed person to decide. |
| Artifact | An exact saved file, such as a prompt, response, patch, test result, or review package. Its SHA-256 hash shows whether its contents changed. |
| Authentication | Proving which person or service is making a request. |
| Authorization | Deciding what that authenticated person or service may do. |
| Candidate | A proposed reusable lesson. It is not searchable knowledge until it is approved. |
| Cerbos | The policy service that decides whether a person or service may perform an action. |
| Code graph | Exact links between code items, such as packages, files, functions, calls, imports, and tests. |
| Codex | OpenAI's cloud-backed coding agent. The CLI runs on the local machine, but model inference is not local. |
| Controller | The non-human OpenClaw component that tracks a workflow and chooses the next permitted step. |
| Evidence | Saved facts used to support a result or decision, such as a patch, test output, source location, or review. |
| Fail closed | Deny an action when a required security check cannot be completed. |
| Governance profile | Rules for who may perform each role. `solo` allows one person to hold every role; `team` and `regulated` can require different people. |
| Hydrate | Load the full, current PostgreSQL record after a search returns its ID. |
| Idempotent | Safe to retry without creating the same change twice. |
| Immutable | Saved so that later changes create a new version instead of silently replacing the old contents. |
| Kimi | Moonshot AI's cloud model, used here only for an explicit, policy-approved review. |
| Knowledge base | The approved lessons, procedures, evidence, and links that agents can retrieve for later tasks. |
| Local-only | Data and model work stay on the controlled local or private infrastructure. No cloud-model fallback is allowed. |
| MCP | Model Context Protocol. It gives Codex, OpenClaw, and other clients a typed way to call platform tools. |
| Milvus | The vector database used to find approved items with similar meaning. It is not the official source of truth. |
| Ollama | The local model server used for coding, maintenance, and embeddings. |
| Outbox | A PostgreSQL table of work that must happen after a successful database change, such as updating Milvus. A worker retries this work safely. |
| PostgreSQL authority | PostgreSQL holds the official workflow, approval, provenance, repository, and code-graph records. |
| Principal | An authenticated person or service identity. |
| Projection | A searchable copy derived from official data. Milvus is a projection and can be rebuilt from PostgreSQL. |
| Provenance | Information about where a result came from: model, provider, repository revision, prompt, tools, and reviewer. |
| Semantic search | Search by similar meaning instead of only exact words. |
| Stable ID | A PostgreSQL UUID reused across systems so a Milvus result can be loaded from the official SQL record. |
| Transaction | A group of database changes that all succeed together or all fail together. This prevents half-finished approvals and indexing requests. |
| Vector embedding | A list of numbers representing meaning. Milvus compares these lists to find similar items. |
| Work packet | A bounded task description containing the repository revision, allowed files, checks, risk, disclosure rules, and execution limits. |

For the short architecture explanation, start with the repository
[README](../README.md#how-it-works). For commands, use the
[Operations Runbook](operations.md) and [Role Workflows](role-workflows.md).
