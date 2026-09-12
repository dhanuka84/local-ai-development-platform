# Plain-English Glossary

Updated September 12, 2026. [Developer guide](developer-guide.md) ·
[Documentation index](README.md).

Use this page when a design or operations document uses an unfamiliar term.

| Term | Meaning in this platform |
|---|---|
| Agent | A model-driven worker that receives a task and can use allowed tools. |
| Approval gate | A decision requiring the permitted actor and exact evidence. Generated KB entries need an explicit user decision; validated definitions can use standing task authorization. |
| Analysis branch | The checked-out branch actually scanned. Its canonical API/SQL field is `branch`; it may differ from `default_branch` through an explicit override. |
| Artifact | An exact saved file, such as a prompt, response, patch, test result, or review package. Its SHA-256 hash shows whether its contents changed. |
| Authentication | Proving which person or service is making a request. |
| Authorization | Deciding what that authenticated person or service may do. |
| Branch override | An explicit repository-to-branch rule used when code analysis must follow a branch other than the forge default. The platform records both branches separately. |
| Candidate | A proposed KB entry, such as a fix or procedure. It stays pending and unavailable to ordinary retrieval until its exact version is validated and explicitly approved by the user. |
| Catalog-only repository | A known Git repository with authoritative identity metadata but no active code graph, usually because it contains only documentation or unsupported source. |
| Cerbos | The policy service that decides whether a person or service may perform an action. |
| Code graph | Exact links between code items, such as packages, files, functions, calls, imports, and tests. |
| Codex | The coding client used by the configured cloud route or an explicit local Ollama route. A locally running CLI does not establish where inference occurs; inspect the selected provider. |
| Controller | The non-human OpenClaw component that tracks a workflow and chooses the next permitted step. |
| Evidence | Saved facts used to support a result or decision, such as a patch, test output, source location, or review. |
| Fail closed | Deny an action when a required security check cannot be completed. |
| Governance profile | Rules for who may perform each role. `solo` allows one person to hold every role; `team` and `regulated` can require different people. |
| Git revision | The exact commit analyzed. Its canonical API/SQL field is `revision`; this is the concept sometimes called `git_commit`. |
| Hydrate | Load the full, current PostgreSQL record after a search returns its ID. |
| Idempotent | Safe to retry without creating the same change twice. |
| Immutable | Saved so that later changes create a new version instead of silently replacing the old contents. |
| Kimi | Moonshot AI's cloud model, used here only for an explicit, policy-approved review. |
| Knowledge base (KB) | Governed reusable software knowledge with versions, provenance and evidence. Approved, currently eligible entries can be retrieved for later tasks. |
| KB entry / lesson | The same reusable item: a fix, procedure or recommendation. “Lesson” is the older pilot label, not a separate product or store. |
| Domain definition | A versioned description of the subject area and its data contract in the governed registry. |
| Capability definition | A versioned description of a supported operation and its contract in the governed registry. |
| Metric definition | A versioned formula, dimensions and fixed reviewed SQL; metric queries never execute model-generated SQL. |
| Registry validation | Executed contract fixtures and PostgreSQL query preparation tied to exact definition hashes. It is not a new E2E run. |
| Functional acceptance | The local behavior proven by all mapped tests against disposable services; reproduced with `make agent-ready-functional`. |
| Deferred acceptance | A rollout/adoption prerequisite retained as open, such as target identity, live access or a measured cohort. It cannot excuse a failed functional test. |
| Standing task authorization | The user’s instruction permits necessary in-scope work under the existing operator roles. It does not grant new roles or approve generated KB entries. |
| Task credential | An expiring workload token limited to one active task and the issuer’s current development authority. |
| Source freshness | Current eligibility of the exact repository/branch/revision and validation binding. Changed or missing source can withdraw a previously approved entry. |
| Validated reuse | A task records eligible approved context and passes trusted local validation; it may complete while its newly generated KB entry stays pending. |
| Retention alert | A request to review/hold old evidence. The current implementation does not delete or archive evidence automatically. |
| Trace | Correlated durable task events, artifact references and decisions, with optional collector export. |
| Local-only | Data and model work stay on the controlled local or private infrastructure. No cloud-model fallback is allowed. |
| MCP | Model Context Protocol. It gives Codex, OpenClaw, and other clients a typed way to call platform tools. |
| Milvus | The vector database used to find approved items with similar meaning. It is not the official source of truth. |
| Ollama | The local model server used for coding, maintenance, and embeddings. |
| Outbox | A PostgreSQL table of work that must happen after a successful database change, such as updating Milvus. A worker retries this work safely. |
| Outbox compaction | Marking duplicate, superseded, or non-active pending projection work complete while retaining audit rows and one event for each active entity. |
| Apache AGE | A PostgreSQL extension used here as a rebuildable active property-graph projection for bounded Cypher traversal. |
| GraphRAG | Retrieval that combines Milvus semantic seeds, exact AGE/PostgreSQL topology, and authoritative PostgreSQL hydration. |
| PostgreSQL authority | PostgreSQL holds the official workflow, approval, provenance, repository, and code-graph records. |
| Principal | An authenticated person or service identity. |
| Projection | A searchable copy derived from official data. Milvus is a projection and can be rebuilt from PostgreSQL. |
| Provenance | Information about where a result came from: model, provider, repository revision, prompt, tools, and reviewer. |
| Semantic search | Search by similar meaning instead of only exact words. |
| Stable ID | A PostgreSQL UUID reused across systems so an AGE or Milvus result can be loaded from the official SQL record. |
| Transaction | A group of database changes that all succeed together or all fail together. This prevents half-finished approvals and indexing requests. |
| Vector embedding | A list of numbers representing meaning. Milvus compares these lists to find similar items. |
| Work packet | A bounded task description containing the repository revision, allowed files, checks, risk, disclosure rules, and execution limits. |

For the short architecture explanation, start with the repository
[README](../README.md#how-it-works). For commands, use the
[Operations Runbook](operations.md) and [Role Workflows](role-workflows.md).
