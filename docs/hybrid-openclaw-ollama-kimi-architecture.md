# Hybrid OpenClaw, Ollama, and Kimi Architecture

**Status:** Historical design baseline; see the implemented architecture and ADRs  
**Last updated:** 2026-08-08  
**Primary workload:** Software development and system maintenance  
**Target host:** ASUS Ascent GX10 / NVIDIA GB10, 128 GB unified memory  
**Audience:** Platform engineers, software developers, security reviewers, and operators

**Companion:** [Recommended Technology Stack](./hybrid-ai-platform-tech-stack.md)

> This is an archived design record, not an implementation or operations
> guide. Its trust-model discussion remains useful, but model names, commands,
> schemas, and the earlier memory design may be stale. Use the runnable
> [implementation guide](./implementation-guide.md), [operations runbook](./operations.md),
> and [remote-review learning contract](./remote-review-learning.md). The
> [local architecture PNG](./diagrams/hybrid-ai-local-architecture.png) and
> [enterprise architecture PNG](./diagrams/hybrid-ai-enterprise-architecture.png)
> are the canonical visual overviews.
> For governed tasks, [ADR-0010](./adr/0010-rag-first-atomic-task-checkpoints.md)
> supersedes the earlier selective-review sequence: tasks are FIFO queued,
> local mode defaults to `auto`, a strong approved RAG hit skips cloud, an
> allowed miss requires read-only Codex review without manual acceptance, and
> Milvus read-back gates the next task.

## 1. Executive summary

This design establishes a local-first AI engineering platform with four explicit execution roles:

1. A **development coordinator** running Qwen3-Coder-Next locally through Ollama. It owns repository inspection, edits, builds, tests, and local knowledge retrieval.
2. A **cloud reviewer** running Kimi K3 through OpenClaw's Moonshot provider. It is advisory and receives only deliberately selected, sanitized context for difficult design, debugging, security, and optimization work.
3. A **Codex review client** using an approved ChatGPT-backed Codex review model. It connects to a local engineering MCP server for a bounded review package, relevant approved rules, and a controlled way to record review candidates.
4. A **maintenance agent** running entirely locally through Ollama. It has no cloud model fallback, no cloud credentials, and no permission to delegate to either cloud reviewer.

OpenClaw runs locally as the agentic control plane. A loopback-only MCP server provides a shared, policy-enforced interface to the knowledge and review workflow for OpenClaw and local Codex clients. The canonical knowledge base and embeddings remain local. Cloud inference is an explicit development workflow step, not an automatic model fallback.

Review output never becomes canonical knowledge automatically. Kimi and Codex findings are stored as candidates, validated against source and tests, approved by an owner, generalized into reusable guidance, and only then promoted into the knowledge base and reindexed.

The recommended default local coding model is `qwen3-coder-next`. It is an approximately 80-billion-parameter mixture-of-experts model with about 3 billion active parameters per token, a 52 GB Q4 Ollama artifact, native tool use, and a 256K maximum context window. The initial production context limit should be 64K to retain memory headroom and responsive agent iterations.

## 2. Goals

- Keep ordinary software development, source inspection, editing, builds, tests, and knowledge retrieval on the local machine.
- Use Kimi K3 selectively to improve architecture, difficult debugging, optimization, security review, and large-change review.
- Use Codex as a second, diff-focused cloud reviewer through a local MCP-controlled workflow.
- Guarantee that maintenance inference stays local and fails closed if Ollama is unavailable.
- Maintain a local, cited knowledge base for architecture, coding standards, runbooks, incidents, and operational procedures.
- Convert validated review outcomes into durable, reusable knowledge without promoting raw model output.
- Capture successful Codex generation workflows as validated, reusable solution patterns so a local model can produce functionally similar results for similar future inputs.
- Prevent credentials, secrets, production data, and unrelated repository content from being sent to cloud models.
- Make cloud escalations visible, intentional, measurable, and auditable.
- Keep agent state, authentication, workspaces, and session history separated by role.
- Provide a staged path from a simple built-in knowledge index to a larger QMD-based knowledge system.

## 3. Non-goals

- Running Kimi K3 weights locally. Kimi K3 is a 2.81-trillion-parameter model and is not practical on a single 128 GB system.
- Using cloud fallback as a substitute for operational reliability.
- Giving the cloud reviewer unrestricted shell or write access.
- Indexing secrets, `.env` files, credential stores, production database exports, or unredacted production logs.
- Treating generated answers as authoritative without build, test, policy, or human verification.
- Automatically sending an entire repository to a cloud model.
- Allowing a cloud reviewer to search the unrestricted local knowledge base or write canonical knowledge directly.
- Guaranteeing token-for-token reproduction of a probabilistic cloud-model response. Exact outputs require deterministic templates or stored artifacts.

## 4. Assumptions and constraints

- The target GX10 has 128 GB coherent unified memory and a GB10 Blackwell GPU.
- Ollama and OpenClaw run on the GX10 or on the same trusted private host.
- Ollama is bound to loopback or a trusted private interface and uses its native API at `http://127.0.0.1:11434`, without `/v1`.
- OpenClaw is kept current enough to support native Ollama providers, per-agent model policies, subagents, and the Moonshot provider.
- The Moonshot API key is stored only in the cloud reviewer's agent authentication store. It is not exported globally.
- Repository source remains the source of truth. The knowledge base supplements source search; it does not replace direct inspection with `rg`, language tooling, builds, and tests.
- The model context window is a capacity ceiling, not a target. Retrieval should provide the smallest relevant context.

## 5. Architecture decisions

| Decision | Choice | Rationale |
|---|---|---|
| Agent control plane | Local OpenClaw Gateway | Provides isolated agents, tools, sessions, routing, and model policies. |
| Local inference | Ollama native API | Simple model lifecycle and native OpenClaw tool-calling integration. |
| Local coding model | `qwen3-coder-next` | Coding-specialized, tool-capable, Apache 2.0, and efficient enough for iterative agent work. |
| Local utility model | `qwen3.5:9b` | Lower latency for titles, summaries, classification, and bounded internal tasks. |
| Local embeddings | `qwen3-embedding:4b` | Small footprint, multilingual and code retrieval support, and local Ollama integration. |
| Cloud optimization | `moonshot/kimi-k3` | Strong long-horizon reasoning, tool-aware software engineering, and large-context review. |
| Cloud invocation | Explicit cloud-review subagent | Makes disclosure intentional and avoids confusing failover with optimization. |
| Second review lane | Codex review using an approved ChatGPT-backed model | Adds an independent, diff-oriented review path without changing local execution ownership. |
| Shared integration boundary | Loopback-only engineering MCP server | Gives OpenClaw and Codex the same typed retrieval, review-package, candidate-recording, and audit contracts. |
| Reusable generation memory | Validated solution-pattern library | Preserves the recipe, evidence, output contract, and exemplars needed for local regeneration instead of storing only final prose. |
| Initial memory backend | OpenClaw built-in memory with Ollama embeddings | Low operational complexity and per-agent SQLite storage. |
| Large-scale memory option | QMD plus Memory Wiki | Adds external directory indexing, BM25, vector search, reranking, provenance, and durable wiki structure. |
| Maintenance behavior | Strict local model, empty fallbacks | Prevents silent cloud execution. |

## 6. Logical architecture

```text
                              Developer / Operator / CI
                                      |
                      +---------------+---------------+
                      |                               |
                      v                               v
          +--------------------------+       +-----------------------+
          | Local OpenClaw Gateway   |       | Local Codex CLI/App   |
          | routing, policy, audit   |       | review client         |
          +------------+-------------+       +-----------+-----------+
                       |                                 |
              +--------+---------+                       |
              |                  |                       |
              v                  v                       |
 +-----------------------+  +-----------------------+    |
 | Development           |  | Maintenance Agent     |    |
 | qwen3-coder-next      |  | qwen3-coder-next      |    |
 +----------+------------+  | local, no fallback    |    |
            |               +-----------+-----------+    |
            |                           |                |
            +---------------------------+----------------+
                                        |
                                        v
                      +----------------------------------+
                      | Engineering Knowledge MCP        |
                      | scoped retrieval, review packs,  |
                      | candidate writes, policy, audit  |
                      +----------------+-----------------+
                                       |
                    +------------------+------------------+
                    |                                     |
                    v                                     v
          +----------------------+              +--------------------+
          | Canonical local KB   |              | Review candidates  |
          | rules, solution     |              | run captures +     |
          | patterns + indexes  |              | audit log          |
          +----------------------+              +--------------------+

             explicit, sanitized cloud review paths only
                    |                                     |
                    v                                     v
          +----------------------+              +--------------------+
          | Kimi K3 / Moonshot   |              | ChatGPT-backed     |
          | advisory reviewer    |              | Codex reviewer     |
          +----------------------+              +--------------------+
```

## 7. Trust boundaries and data policy

### 7.1 Data classification

| Class | Examples | Local models | Kimi or Codex cloud reviewer |
|---|---|---:|---:|
| Public | Published documentation, public OSS code | Allowed | Allowed |
| Internal | Architecture notes, non-secret source, sanitized diffs | Allowed | Allowed when the task requires it |
| Confidential | Unreleased product logic, customer-specific implementation | Allowed | Denied by default; requires explicit approval and minimization |
| Restricted | Secrets, tokens, private keys, personal data, production dumps | Allowed only under local policy | Never allowed |

### 7.2 Mandatory cloud export rules

Before invoking Kimi or starting a Codex cloud review, the development coordinator must construct a bounded context package containing only:

- The problem statement and acceptance criteria.
- Relevant architecture decisions and coding standards.
- The minimal code symbols, snippets, or diff needed for analysis.
- Sanitized build or test failures.
- The local agent's current hypothesis and requested review question.
- A manifest of included files or snippets.

MCP tool results returned to Codex are model-visible and therefore cross the OpenAI cloud boundary. The Codex-facing MCP identity must never expose unrestricted `kb_search`, raw filesystem access, maintenance-only collections, or a tool that can enumerate arbitrary review packages. It may read only an explicitly prepared, sanitized package and approved review guidance within that package's scope.

The export step must exclude:

- `.env*`, credentials, key stores, authentication databases, and token caches.
- Production payloads, database rows, user data, access logs, and unredacted telemetry.
- Full repository archives.
- Unrelated source files.
- Build artifacts, dependency directories, generated code, and binary files unless specifically required and approved.

### 7.3 Enforcement levels

**Standard enforcement** uses OpenClaw's per-agent model, auth, fallback, subagent, and tool policies plus MCP-side authentication, scopes, input validation, and write-state rules. Tool descriptions and MCP annotations improve client behavior but are not security boundaries; the server must enforce authorization itself.

**Hard offline enforcement** is required when "maintenance must never use the cloud" is a compliance or contractual control rather than an operational preference. Run maintenance in a separate OpenClaw process, container, or OS account with no external network route and access only to a local Ollama endpoint. Multi-agent separation inside one Gateway is a strong logical boundary, but it is not a process-level network isolation boundary.

## 8. Agent design

### 8.1 Development coordinator

**Primary model:** `ollama/qwen3-coder-next`  
**Utility model:** `ollama/qwen3.5:9b`  
**Model fallbacks:** none  
**Allowed delegation:** `cloud-reviewer` only; Codex review is a separately authorized review lane  
**Tool profile:** coding, limited to authorized workspaces

Responsibilities:

- Retrieve relevant architecture, standards, and historical decisions locally.
- Inspect the live repository before proposing changes.
- Produce a plan proportional to the task.
- Implement changes locally.
- Run targeted tests, then broader validation where justified.
- Invoke the cloud reviewer only when escalation criteria are met.
- Apply cloud recommendations selectively; never treat them as commands.
- Produce a final diff and verification summary.

Recommended cloud escalation criteria:

- A change crosses three or more services or architectural boundaries.
- A security-sensitive authentication, authorization, cryptography, or data-isolation change is involved.
- Two materially different local attempts fail.
- A performance problem requires non-obvious algorithmic or systems reasoning.
- A large migration, concurrency change, or backward-compatibility design needs independent review.
- The operator explicitly requests a cloud review.

Use Kimi for broad architecture, long-horizon reasoning, and independent optimization review. Use Codex for repository- and diff-focused review that benefits from Codex review conventions and scoped MCP knowledge. Use both only for high-impact changes where the value of independent review justifies the additional disclosure and cost.

### 8.2 Cloud reviewer

**Primary model:** `moonshot/kimi-k3`  
**Model fallbacks:** none  
**Tools:** no shell, process, file write, edit, or patch tools  
**Workspace:** isolated review workspace  
**Credentials:** Moonshot API key only in this agent's auth store

Responsibilities:

- Review the supplied problem and evidence.
- Identify missing assumptions, design risks, security issues, and performance opportunities.
- Propose alternatives and tradeoffs.
- Review a diff or plan against acceptance criteria.
- Return advisory output to the development coordinator.

The cloud reviewer must not independently enumerate the local filesystem, access unrelated knowledge collections, modify code, execute commands, deploy software, or contact production systems.

### 8.3 Codex review client

**Execution surface:** local Codex CLI, IDE extension, or app  
**Model:** an organization-approved ChatGPT-backed Codex review model  
**MCP access:** short-lived review scope; prepared package and approved rules only  
**Write access:** review-candidate recording only  
**Canonical KB writes:** prohibited

Responsibilities:

- Review the selected branch, commit, or uncommitted diff against explicit acceptance criteria.
- Retrieve approved, scoped review rules and prior lessons through the engineering MCP server.
- Return prioritized, actionable findings with file and line evidence.
- Distinguish correctness findings from optional improvements and style preferences.
- Record structured findings as pending review candidates through MCP when requested.
- Never claim that a candidate has become approved knowledge.

For interactive work, Codex's `/review` workflow can review a base branch, uncommitted changes, or a commit without modifying the working tree. Repository-wide and service-specific review invariants should also be placed in the applicable `AGENTS.md`; narrow, consequential rules are preferable to broad style advice.

Codex is a cloud reviewer even when its CLI or MCP server runs on the local GX10. Source sent in prompts, diffs inspected by the model, and MCP tool results exposed to the model are cloud disclosures and follow the same classification policy as Kimi.

### 8.4 Maintenance agent

**Primary model:** `ollama/qwen3-coder-next`  
**Utility model:** local Ollama model  
**Model fallbacks:** empty  
**Allowed delegation:** none  
**Cloud credentials:** none  
**External browsing:** disabled by default  
**Elevated execution:** disabled by default

Responsibilities:

- Search maintenance runbooks and known-error records.
- Diagnose using local commands and locally available telemetry.
- Prefer documented recovery actions.
- Show a proposed action, risk, rollback, and validation plan before material changes.
- Require approval for service restarts, data changes, permission changes, package upgrades, and destructive actions.
- Record the result and any new knowledge locally.

Failure behavior:

- If Ollama is unavailable, stop and report the local dependency failure.
- If the knowledge base is unavailable, continue only with explicit operator approval and clearly mark the missing evidence.
- Never select or fall back to a cloud model.
- Never ask another agent with cloud access to complete the maintenance task.

## 9. Model topology and capacity planning

### 9.1 Recommended models

| Model | Role | Approximate Ollama artifact | Context policy |
|---|---|---:|---:|
| `qwen3-coder-next` | Development and maintenance | 52 GB Q4 | Start at 64K |
| `qwen3.5:9b` | Utility work | 6.6 GB Q4 | 16K-32K |
| `qwen3-embedding:4b` | Knowledge embeddings | 2.5 GB | Chunk-sized inputs |
| `moonshot/kimi-k3` | Cloud review | Hosted | Send minimal retrieved context |
| Approved Codex review model | Independent code review | Hosted | Diff plus scoped rules and prior learnings |

### 9.2 Kimi cloud route options

The preferred production route is OpenClaw's direct Moonshot provider:

```text
OpenClaw cloud-reviewer -> Moonshot API -> Kimi K3
```

This route has the clearest separation of credentials, billing, audit records, and cloud-review policy. It also avoids an unnecessary local proxy hop.

An alternative is Ollama Cloud, either through OpenClaw's cloud provider or through an Ollama cloud model exposed by the local daemon:

```text
OpenClaw cloud-reviewer -> Ollama Cloud provider -> Kimi K3

or

OpenClaw cloud-reviewer -> local Ollama daemon -> Ollama Cloud -> Kimi K3
```

Use the Ollama Cloud route only when consolidating provider authentication and billing in Ollama is an explicit platform objective. It can be convenient, but it does not make Kimi local: prompts, retrieved context, and responses still cross the cloud trust boundary. Account entitlements, model names, rate limits, and billing also differ from the direct Moonshot route and must be verified before deployment.

Whichever route is selected, configure it only on `cloud-reviewer`, keep the development coordinator and maintenance agent free of cloud fallbacks, and apply the same sanitization, audit, and approval controls. Do not configure both routes as automatic fallbacks; select one primary cloud route so costs and disclosures remain predictable.

### 9.3 Memory budget

A practical single-GX10 target is:

- 8-16 GB reserved for the OS, OpenClaw, filesystem cache, and supporting processes.
- Approximately 52 GB for Qwen3-Coder-Next weights.
- A variable KV cache budget determined by active context and concurrency.
- Approximately 2.5 GB when the embedding model is loaded.
- Remaining capacity retained as operational headroom.

Do not keep multiple large generation models resident without measurement. Prefer one primary generation model plus a small utility or embedding model. Configure finite `keep_alive` values so inactive models are released.

### 9.4 Context strategy

- Begin with 64K for coding sessions.
- Use retrieval, symbol search, dependency graphs, and targeted file reads instead of increasing context indiscriminately.
- Compact or start a new session at logical task boundaries.
- Keep stable architecture and policy prefixes unchanged when provider caching is available.
- Measure memory and latency before increasing beyond 64K.

## 10. Knowledge-base architecture

### 10.1 Source-of-truth hierarchy

1. Live repository source, tests, schemas, and deployment definitions.
2. Approved architecture decisions and interface contracts.
3. Runbooks and operational procedures.
4. Incident reports and known-error records.
5. Session-derived notes and model-generated summaries.

Lower levels must not silently override higher levels.

### 10.2 Recommended content layout

```text
knowledge/
├── shared/
│   ├── architecture/
│   ├── coding-standards/
│   ├── security-policies/
│   ├── api-contracts/
│   ├── review-learnings/
│   ├── solution-patterns/
│   └── glossary/
├── development/
│   ├── requirements/
│   ├── adrs/
│   ├── design-notes/
│   ├── dependencies/
│   └── test-strategy/
└── maintenance/
    ├── service-catalog/
    ├── runbooks/
    ├── known-errors/
    ├── incident-reviews/
    ├── backup-restore/
    └── rollback-procedures/

review-state/
├── packages/          # sanitized, short-lived review inputs
├── candidates/        # raw structured findings awaiting disposition
├── generation-runs/   # short-lived Codex/local run envelopes and artifacts
├── pattern-candidates/ # candidate reusable recipes awaiting validation
├── decisions/         # accepted/rejected/superseded dispositions
└── audit/             # append-only event records
```

`knowledge/` is canonical, curated content. `review-state/` is workflow state and must not be indexed as approved knowledge. Retention limits should remove expired packages and raw candidates while keeping the minimal audit and decision record required by policy.

### 10.3 Document metadata

Every maintained knowledge document should include:

```yaml
---
title: Example service recovery
owner: platform-team
status: approved
classification: internal
service: example-service
source_commit: abc1234
reviewed_at: 2026-08-08
review_after: 2026-11-08
---
```

Recommended metadata fields are owner, status, classification, system or service, source revision, evidence links, review date, and expiry or next-review date.

### 10.4 Initial backend: built-in memory with Ollama

Use the built-in SQLite memory engine for the first production increment. It minimizes components and supports local Ollama embeddings.

```json5
{
  memory: {
    backend: "builtin",
    citations: "on",
    search: {
      provider: "ollama",
      model: "qwen3-embedding:4b",
      fallback: "none",
      remote: {
        baseUrl: "http://127.0.0.1:11434",
        apiKey: "ollama-local",
        nonBatchConcurrency: 2,
      },
    },
  },
}
```

Place curated agent knowledge in `MEMORY.md` and the agent workspace's `memory/` directory. Keep development and maintenance knowledge in their respective agent workspaces. Synchronize approved shared knowledge into both workspaces using a controlled deployment step.

Changing the embedding model changes the vector space. Force a full reindex after any embedding-model change and record the model identity with the index.

### 10.5 Scale-up backend: QMD and Memory Wiki

Move to QMD when the system must index multiple documentation trees, team notes, or session transcripts outside the normal workspace memory tree. QMD provides local BM25, vector search, query expansion, and reranking.

Use Memory Wiki beside QMD when durable knowledge needs structured claims, evidence, contradiction tracking, freshness reports, and provenance-rich pages.

Do not enable QMD and the built-in Ollama embedding configuration as if they were a single combined backend. Select one active recall backend, benchmark it against a representative retrieval set, and migrate deliberately.

### 10.6 Indexing policy

Index:

- Markdown and text documentation.
- ADRs, API contracts, schemas, runbooks, and approved incident reviews.
- Selected source summaries and symbol documentation with a source commit.
- Test strategy and troubleshooting knowledge.

Exclude:

- `.git`, `node_modules`, `vendor`, `dist`, `build`, caches, binaries, and generated output.
- Secrets, credentials, private keys, `.env` files, and auth stores.
- Raw production data and personally identifiable information.
- Stale generated summaries without provenance.

Use direct source tools for live code whenever possible. If source code is indexed, attach the repository and commit hash and remove obsolete chunks when the commit changes.

### 10.7 Review-learning lifecycle

The knowledge pipeline has four states:

```text
cloud finding -> pending candidate -> validated decision -> promoted knowledge
```

1. **Record:** Kimi or Codex returns a structured finding. The MCP server records it as `pending` with reviewer, route, repository, base and head revisions, evidence locations, severity, and a content hash.
2. **Validate:** The local development coordinator reproduces the issue through source inspection, tests, static analysis, or an accepted design argument. Unsupported findings are rejected with a reason; duplicate findings link to the existing candidate.
3. **Approve:** A designated code owner, platform owner, or security owner approves the generalized lesson. The reviewing model cannot perform this transition.
4. **Promote:** A deterministic publisher creates or updates a Markdown document under `knowledge/shared/review-learnings/`, records provenance and applicability, commits it through the normal review process, and triggers reindexing.

A promoted entry should capture an invariant and safe path, not a transcript. Example:

```yaml
---
title: Preserve event wire names
owner: platform-team
status: approved
classification: internal
applies_to: services/event-gateway/**
source_reviews: [rvw_01JXYZ]
source_commits: [abc1234]
validated_by: regression-test-name
reviewed_at: 2026-08-08
review_after: 2026-11-08
---
```

The body should state the problem, durable invariant, approved implementation pattern, known exception, validation method, and evidence. If the lesson is important on every code review, add a concise version to the applicable root or nested `AGENTS.md` and link back to the full KB entry.

Rejected findings should remain available to the review evaluator for a limited retention period so recurring false positives can be measured, but they must never appear as approved retrieval results.

### 10.8 Replayable solution memory

#### Objective

When Codex produces a high-quality result, preserve enough structured evidence that a local model can later solve a materially similar request with comparable structure, behavior, and quality. The target is functional or rubric equivalence, not identical tokens.

Choose the reuse mechanism according to the required fidelity:

| Requirement | Mechanism | Use when |
|---|---|---|
| Exact output except for parameters | Deterministic template or renderer | Reports, configuration blocks, boilerplate, or contracts have a fixed structure. |
| Similar, adapted output | Retrieval-augmented solution patterns | The task repeats but inputs, code, constraints, or explanations vary. This is the recommended default. |
| Broad repeated behavior across many task families | Fine-tuning or distillation after evaluation | Hundreds or thousands of approved examples show that RAG and templates are insufficient. |

Do not begin with fine-tuning. A validated pattern library is cheaper to update, easier to audit and delete, supports citations, and lets the local model see why an example applies.

#### Capture a run envelope, not only the final answer

For each candidate Codex run, capture:

- Normalized task intent and task type.
- Original user input or a redacted reference to it.
- Repository, base commit, head commit, language, framework, and affected subsystem.
- Effective repository instructions and identifiers of retrieved KB sources.
- Constraints, acceptance criteria, output format, and prohibited behavior.
- Model route and timestamp; model identity is provenance, not a retrieval requirement.
- Summarized tool sequence and material observations, excluding secrets and noisy raw logs.
- Final response plus content-addressed references to produced files or diff.
- Validation commands, exit status, tests, linters, schema checks, and human feedback.
- Final disposition: successful, partially successful, failed, superseded, or unsafe.
- Classification, owner, retention, and source revision.

Raw run envelopes live under `review-state/generation-runs/` and are never searched as approved knowledge. Large artifacts should use content-addressed storage; the database stores their hashes, media type, size, and classification.

#### Generalize a successful run into a solution pattern

A background pattern extractor turns successful runs into candidates. It must remove task-specific secrets, incidental file names, transient identifiers, unsupported reasoning claims, and duplicated prose. A local owner validates and promotes the pattern using the same candidate discipline as review learnings.

Recommended canonical pattern schema:

```yaml
---
pattern_id: solpat_api_error_mapping_v2
title: Add a backward-compatible API error mapping
status: approved
owner: api-platform
classification: internal
task_type: code-change
intent: add-error-mapping
languages: [typescript]
frameworks: [fastify]
applies_to: services/*/src/http/**
input_features:
  required: [existing-error-type, public-api-contract]
  optional: [legacy-client]
exclusions: [wire-format-redesign, authentication-errors]
output_contract: minimal-diff-with-tests
source_runs: [genrun_01JXYZ]
source_commits: [abc1234]
validated_by: [unit-tests, contract-tests, owner-review]
quality_score: 0.94
reviewed_at: 2026-08-08
review_after: 2026-11-08
---
```

The document body should contain:

1. **Intent:** what class of problem this solves.
2. **Applicability:** required signals, optional signals, and exclusions.
3. **Input contract:** fields and evidence needed before generation.
4. **Generation recipe:** ordered reasoning and tool steps stated as an observable procedure, without relying on hidden model reasoning.
5. **Output contract:** required structure, files, schema, or response sections.
6. **Positive exemplar:** the smallest approved input/output or diff that demonstrates the pattern.
7. **Counterexample:** a similar-looking case where the pattern must not be used.
8. **Validation:** deterministic commands, assertions, rubric, and rollback.
9. **Provenance:** source runs, commits, owners, and expiry.

#### Retrieval and local regeneration

For a new local request:

1. Normalize the request into task type, intent, language, framework, subsystem, constraints, and desired output contract.
2. Apply hard metadata filters for classification, repository scope, status, language, and known exclusions.
3. Rank the remaining patterns with hybrid lexical and vector retrieval over the normalized input signature, intent, applicability, and validation description.
4. Select at most two to four diverse patterns. Prefer one close positive exemplar and, where ambiguity exists, one counterexample.
5. Build a bounded local prompt containing the current request, current source evidence, applicable rules, pattern recipe, output contract, exemplar fragments, and required validation.
6. Generate with `ollama/qwen3-coder-next` or the selected local model.
7. Validate using the pattern's deterministic checks. Never judge success only by textual similarity to the Codex output.
8. Record the local result and disposition. If confidence or validation is insufficient, stop or request an explicit cloud escalation rather than silently using Codex.

Recommended retrieval scoring begins with hard filters, then combines semantic similarity, lexical similarity, structural feature matches, validation success rate, and freshness. Tune the weights with an evaluation set; do not hard-code a generic score as a universal truth.

For code generation, the primary success measures are whether the patch applies, tests pass, contracts remain compatible, security checks pass, and the requested behavior is present. For documents, evaluate required sections, factual grounding, schema or template conformance, tone, and an owner-defined rubric. Embedding or text similarity is a secondary diagnostic only.

#### Prompt assembly contract

The local model should receive a stable envelope such as:

```text
Current task
Current repository evidence and revision
Applicable policy and AGENTS.md rules
Retrieved solution patterns with citations
Why each pattern matched and its exclusions
Required output contract
Validation commands or rubric
Instruction to resolve differences from current source, not copy blindly
```

Cap exemplar content and prefer structured recipes over full historical conversations. Historical code can be stale even when the pattern remains valid; the model must inspect current source before applying an exemplar.

#### Promotion and feedback loop

```text
Codex run
  -> short-lived run envelope
  -> successful-run candidate
  -> generalized solution pattern
  -> local validation and owner approval
  -> canonical pattern + reindex
  -> local regeneration
  -> measured outcome and pattern score update
```

Promote only runs with verifiable quality evidence. A persuasive answer without tests, citations, schema validation, or owner acceptance is not a reusable exemplar. Rejected local replays should reduce the pattern's confidence, trigger review when failures recur, and never cause automatic rewriting of canonical knowledge.

After enough approved examples accumulate, evaluate whether a small local model needs adapter training. Training data should contain the normalized instruction, approved context, target output, and validation label—not raw transcripts or rejected generations. Keep a held-out replay set and require the trained model to outperform the RAG-only baseline on task success before adoption. Review organizational policy and applicable provider terms before using cloud-generated material as training data.

## 11. OpenClaw implementation template

The following JSON5 is a merge template, not a replacement for an existing `~/.openclaw/openclaw.json`. Validate actual tool names against the installed OpenClaw version.

```json5
{
  agents: {
    defaults: {
      models: {
        "ollama/qwen3-coder-next": {
          alias: "Local Coder",
          params: {
            num_ctx: 65536,
            keep_alive: "15m",
          },
        },
        "ollama/qwen3.5:9b": {
          alias: "Local Utility",
          params: {
            num_ctx: 32768,
            thinking: false,
            keep_alive: "5m",
          },
        },
        "moonshot/kimi-k3": {
          alias: "Kimi Cloud Review",
        },
      },
    },
    list: [
      {
        id: "development",
        default: true,
        name: "Development Coordinator",
        workspace: "~/.openclaw/workspace-development",
        model: {
          primary: "ollama/qwen3-coder-next",
          fallbacks: [],
        },
        utilityModel: "ollama/qwen3.5:9b",
        subagents: {
          allowAgents: ["cloud-reviewer"],
        },
        tools: {
          profile: "coding",
          elevated: { enabled: false },
        },
      },
      {
        id: "cloud-reviewer",
        name: "Kimi Cloud Reviewer",
        workspace: "~/.openclaw/workspace-cloud-reviewer",
        model: {
          primary: "moonshot/kimi-k3",
          fallbacks: [],
        },
        utilityModel: "moonshot/kimi-k3",
        subagents: {
          allowAgents: [],
        },
        tools: {
          profile: "coding",
          deny: [
            "exec",
            "process",
            "write",
            "edit",
            "apply_patch",
            "sessions_spawn",
            "bundle-mcp",
          ],
          elevated: { enabled: false },
        },
      },
      {
        id: "maintenance",
        name: "Local Maintenance",
        workspace: "~/.openclaw/workspace-maintenance",
        model: {
          primary: "ollama/qwen3-coder-next",
          fallbacks: [],
        },
        utilityModel: "ollama/qwen3.5:9b",
        subagents: {
          allowAgents: [],
        },
        tools: {
          profile: "coding",
          deny: [
            "sessions_spawn",
            "browser",
            "web_search",
            "web_fetch",
            "bundle-mcp",
          ],
          elevated: { enabled: false },
        },
      },
    ],
  },
  memory: {
    backend: "builtin",
    citations: "on",
    search: {
      provider: "ollama",
      model: "qwen3-embedding:4b",
      fallback: "none",
      remote: {
        baseUrl: "http://127.0.0.1:11434",
        apiKey: "ollama-local",
        nonBatchConcurrency: 2,
      },
    },
  },
}
```

Important policy notes:

- An allowed `exec` tool can still write through shell commands. Denying `write`, `edit`, or `apply_patch` does not make `exec` read-only. The cloud reviewer therefore denies `exec` and `process` as well.
- The maintenance agent needs `exec` for builds and diagnostics. Enforce its filesystem and command boundary with sandboxing, OS permissions, repository-scoped working directories, and approvals.
- Tool names vary with plugins and versions. Run configuration validation and an explicit negative security test before production use.
- Set all maintenance cron jobs to the local model with empty fallbacks. Do not let them inherit a cloud default.

## 12. Engineering MCP and Codex integration

### 12.1 Purpose and boundary

Run one local `engineering-knowledge-mcp` service as the typed interface to approved knowledge, sanitized review packages, candidate findings, dispositions, and audit events. Both OpenClaw and local Codex clients can connect to the service, but they use different identities and tool allowlists.

The MCP server is an integration and policy boundary, not a model and not the canonical knowledge store. Canonical knowledge remains version-controlled Markdown. Workflow state lives in a separate database. Search indexes are rebuildable local derivatives.

In the initial phase, OpenClaw's built-in memory index and the MCP service's search index are separate derived indexes over the same approved Markdown. They must not share SQLite files or treat one another's vector tables as an API. The publisher refreshes both after a canonical merge and records their source revision so divergence is detectable.

Recommended implementation:

- TypeScript on the current Node.js LTS release using the official MCP SDK.
- Streamable HTTP bound to `127.0.0.1` for a single shared service used by OpenClaw and Codex.
- Separate bearer tokens for OpenClaw development, Codex review, the Codex hook capture worker, maintenance, and the approval publisher.
- SQLite in WAL mode for review workflow state, idempotency keys, and audit metadata.
- Version-controlled Markdown for approved knowledge.
- Explicit, read-only repository roots; no ambient access to the whole home directory.
- Ollama with `qwen3-embedding:4b` for the MCP service's local semantic index.
- Structured JSON Schema validation for every tool input and output.
- A `systemd` unit with a non-login service account, a read-only canonical-KB mount, writable review-state storage, and no outbound internet access.
- A separate local publisher CLI or worker, invoked by an authorized human identity, that writes only to a dedicated Git worktree and has no model credentials.

STDIO is a valid simpler option for a single client, but a loopback Streamable HTTP service avoids multiple server processes competing for the same review-state database and provides one audit point. Do not expose the listener on a LAN or public interface.

### 12.2 Component responsibilities

| Component | Responsibility |
|---|---|
| Context packager | Resolves the requested Git revision and file allowlist, enforces size and classification limits, redacts secrets, and creates an immutable package manifest. |
| Policy engine | Maps client identity, review capability, repository, classification, and tool to an allow or deny decision. |
| Retrieval adapter | Searches only approved knowledge allowed by the caller and returns citations, source revisions, and freshness metadata. |
| Candidate store | Records structured Kimi and Codex findings as pending evidence with idempotent content hashes. |
| Run capture adapter | Accepts bounded Codex hook events and artifact manifests, then assembles immutable generation-run envelopes asynchronously. |
| Pattern extractor | Produces a generalized candidate recipe from successful runs while removing incidental and restricted content. |
| Replay planner | Matches a new local task to approved patterns and returns a bounded, cited generation envelope plus validation contract. |
| Disposition service | Records local validation and owner approval or rejection; it is unavailable to cloud reviewer identities. |
| Knowledge publisher | Separate local process that converts an approved candidate into a durable Markdown lesson using a deterministic template and a reviewable Git change. |
| Index coordinator | Rebuilds or refreshes the MCP index and the appropriate OpenClaw agent indexes after an approved merge. |
| Audit writer | Appends actor, tool, scope, manifest hash, decision, and timestamp without storing prohibited prompt content. |

### 12.3 Service operation contract

| Operation | Interface | Exposed to | Mutates state | Required behavior |
|---|---|---|---:|---|
| `health_check` | MCP | All local clients | No | Reports service, index, and Ollama readiness without secrets. |
| `review_package_create` | MCP | OpenClaw development only | Yes | Creates a sanitized immutable package from an explicit repository, revisions, and allowlist. |
| `review_package_get` | MCP | Codex review capability | No | Returns only the package named by the capability; no list or arbitrary lookup. |
| `review_rules_search` | MCP | Development and Codex review | No | Returns approved, in-scope rules with citations and freshness metadata. |
| `review_learnings_search` | MCP | Development and Codex review | No | Returns approved prior lessons; excludes pending and rejected candidates. |
| `review_candidate_record` | MCP | OpenClaw development and Codex review | Yes | Stores a structured pending finding; idempotent by review, location, category, and content hash. |
| `review_candidate_validate` | MCP | Local development validator | Yes | Adds reproduction evidence and proposed disposition; cannot approve. |
| `generation_run_capture` | MCP | Trusted Codex hook recorder | Yes | Appends one bounded prompt, tool-summary, output, validation, or feedback event to a run envelope. |
| `generation_pattern_search` | MCP | Local development | No | Returns approved patterns after scope and exclusion filtering, with match explanations and citations. |
| `generation_replay_plan` | MCP | Local development | No | Builds the bounded current-task, pattern, output-contract, and validation envelope for local generation. |
| `generation_result_record` | MCP | Local development | Yes | Records local replay artifacts, validation evidence, outcome, and pattern feedback without changing canonical content. |
| `review_candidate_decide` | Local admin CLI | Human-authorized owner | Yes | Accepts, rejects, or supersedes a validated candidate with actor and reason. |
| `kb_promote_approved` | Local publisher CLI | Publisher only | Yes | Generates a reviewable Markdown change from an approved candidate; refuses all other states. |
| `generation_pattern_promote` | Local publisher CLI | Publisher only | Yes | Publishes a validated solution-pattern candidate through a reviewable Git change. |
| `kb_reindex` | Local operator CLI | Publisher/operator only | Yes | Rebuilds the selected local indexes after the canonical change is merged. |

Do not publish `review_candidate_decide`, `kb_promote_approved`, `generation_pattern_promote`, or `kb_reindex` in the reviewer-facing MCP tool catalog. Keeping them in a separate local executable and OS identity is stronger than relying only on an MCP deny list. Mark read-only MCP tools accurately, but enforce every permission on the server because tool annotations are advisory metadata, not authorization.

### 12.4 Review capability and sanitization

Each cloud review uses an immutable `review_id` and a short-lived capability bound to:

- Repository identity and base/head commit hashes.
- Reviewer route: `kimi` or `codex`.
- Data classification and approval record.
- Exact package manifest hash.
- Allowed read tools and maximum result size.
- Expiry time and single review session.

The package creator must resolve real paths, reject symlink escapes, enforce a file count and byte limit, scan for secrets and restricted patterns, and fail closed on ambiguous classification. It stores copies or content-addressed snippets so later repository changes cannot alter what was reviewed.

Codex-facing search operates only over the approved rules and learnings attached to that package. It cannot expand the scope by choosing a broader search query. Kimi receives the same logical package through the OpenClaw coordinator; it does not receive direct MCP access.

### 12.5 Review record schema

At minimum, persist:

```text
review
  review_id, repository, base_sha, head_sha, route, classification,
  manifest_hash, requested_by, approved_by, created_at, expires_at

finding
  finding_id, review_id, reviewer, model_route, severity, category,
  title, evidence_paths, evidence_lines, rationale, recommendation,
  content_hash, status, created_at

disposition
  finding_id, state, validation_evidence, decision_reason,
  decided_by, decided_at, promoted_document, supersedes

generation_run
  run_id, session_id, turn_id, repository, base_sha, head_sha,
  task_type, normalized_intent, model_route, input_ref, instructions_hash,
  context_manifest, tool_summary_ref, output_artifact_refs,
  validation_results, human_feedback, classification, outcome,
  created_at, expires_at

solution_pattern
  pattern_id, candidate_from_runs, task_type, input_signature,
  applicability, exclusions, recipe, output_contract, exemplar_refs,
  validation_contract, quality_score, state, owner, source_revision,
  reviewed_at, review_after
```

Valid state transitions are:

```text
pending -> validated -> accepted -> promoted
    |          |            |
    +----------+------------+-> rejected
                           \-> superseded
```

The server rejects skipped transitions, model-supplied approver identities, edits to immutable review evidence, and promotion without an accepted disposition.

Generation runs transition independently:

```text
capturing -> complete -> validated-success -> pattern-candidate
     |           |              |
     +-----------+--------------+-> failed / unsafe / expired

pattern-candidate -> approved -> promoted -> active
          |                         |
          +-> rejected              +-> superseded / retired
```

Only `validated-success` runs may seed a pattern candidate. A pattern must include applicability, exclusions, an output contract, and validation evidence before approval.

### 12.6 Connect standalone Codex

Codex CLI, the IDE extension, and the ChatGPT desktop app share MCP configuration for the same Codex host. Prefer project-scoped `.codex/config.toml` in a trusted repository when this integration should not apply globally.

```toml
[mcp_servers.engineering_review]
url = "http://127.0.0.1:7788/mcp"
bearer_token_env_var = "ENGINEERING_MCP_CODEX_TOKEN"
required = true
enabled = true
enabled_tools = [
  "health_check",
  "review_package_get",
  "review_rules_search",
  "review_learnings_search",
  "review_candidate_record",
]
default_tools_approval_mode = "writes"
startup_timeout_sec = 10
tool_timeout_sec = 60

[mcp_servers.engineering_review.tools.review_candidate_record]
approval_mode = "prompt"
```

Set `review_model` separately to an organization-approved model available to the Codex account, or leave it unset to retain the current Codex default. Do not bake a fast-changing model identifier into the MCP server.

Verify the client connection:

```bash
codex mcp list
```

Then use `/mcp` in Codex to confirm that only the five intended tools are visible. Use `/review` for a base-branch, commit, or uncommitted-change review, and require the prompt to include the issued `review_id`.

ChatGPT web does not read the local Codex MCP configuration. Making this server available to hosted ChatGPT would require a remotely reachable, authenticated MCP-backed plugin and a separate security design; that expansion is out of scope for the local-first deployment.

### 12.7 Register the service with OpenClaw

Register the same loopback server in OpenClaw's MCP registry with an OpenClaw-specific token and tool filter. A merge-template example is:

```json5
{
  mcp: {
    servers: {
      "engineering-knowledge": {
        url: "http://127.0.0.1:7788/mcp",
        transport: "streamable-http",
        headers: {
          Authorization: "Bearer ${ENGINEERING_MCP_OPENCLAW_TOKEN}",
        },
        requestTimeoutMs: 60000,
        connectionTimeoutMs: 5000,
        supportsParallelToolCalls: false,
        toolFilter: {
          include: [
            "health_check",
            "review_package_create",
            "review_rules_search",
            "review_learnings_search",
            "review_candidate_record",
            "review_candidate_validate",
            "generation_pattern_search",
            "generation_replay_plan",
            "generation_result_record",
          ],
        },
        codex: {
          agents: ["development"],
          defaultToolsApprovalMode: "prompt",
        },
      },
    },
  },
}
```

The `codex` projection is useful when OpenClaw launches a Codex app-server runtime. A standalone Codex client should use its own `.codex/config.toml`. Avoid projecting and directly registering the same server twice in one Codex runtime.

After configuration, inspect and probe without running a model task:

```bash
openclaw mcp status --verbose
openclaw mcp doctor engineering-knowledge --probe
openclaw mcp tools engineering-knowledge
```

The main configuration template denies `bundle-mcp` to `cloud-reviewer` and `maintenance`, preventing their normal tool profiles from exposing registered MCP tools. If maintenance later needs a dedicated read-only MCP route, add it only after verifying how the installed OpenClaw version scopes server projection and agent tool policy; hard-offline maintenance must not share the Codex or development MCP identity.

### 12.8 End-to-end review and learning sequence

1. The development coordinator completes local discovery, implementation, and targeted tests.
2. It retrieves approved review rules and prior lessons locally.
3. The operator or policy selects Kimi, Codex, or both and records the reason.
4. `review_package_create` builds an immutable, sanitized package and returns its manifest hash and expiry.
5. Kimi receives the package through the OpenClaw cloud-review agent. Codex receives only the same package and scoped guidance through its MCP capability.
6. Each reviewer returns findings in the structured finding schema.
7. Findings are recorded as pending candidates; recording does not imply acceptance.
8. The local coordinator validates each consequential finding against current source and tests.
9. An authorized owner accepts, rejects, or supersedes each validated candidate.
10. The publisher generalizes accepted lessons, creates a reviewable KB change, and links provenance.
11. After merge, local indexes are refreshed and a retrieval test proves that the lesson is discoverable in the correct scope.
12. If the lesson is a durable code-review invariant, a concise rule is added to the applicable `AGENTS.md` and evaluated against a violation, a safe counterexample, and an unrelated change.

### 12.9 Suggested implementation layout

```text
engineering-mcp/
├── package.json
├── tsconfig.json
├── src/
│   ├── server.ts
│   ├── config.ts
│   ├── auth/
│   │   ├── authenticate.ts
│   │   └── authorize.ts
│   ├── tools/
│   │   ├── packages.ts
│   │   ├── retrieval.ts
│   │   ├── candidates.ts
│   │   ├── generation.ts
│   │   └── health.ts
│   ├── core/
│   │   ├── classifier.ts
│   │   ├── sanitizer.ts
│   │   ├── manifest.ts
│   │   ├── state-machine.ts
│   │   └── citations.ts
│   ├── storage/
│   │   ├── review-db.ts
│   │   ├── knowledge-index.ts
│   │   ├── run-envelope.ts
│   │   └── audit.ts
│   └── publisher/
│       ├── decide-cli.ts
│       ├── promote-cli.ts
│       ├── promote-pattern-cli.ts
│       └── reindex-cli.ts
├── codex-hooks/
│   ├── capture-run.mjs
│   └── capture-worker.mjs
├── migrations/
├── schemas/
└── test/
    ├── contract/
    ├── security/
    ├── state-machine/
    └── retrieval/
```

Keep the publisher executables in the same source tree for schema reuse, but install and run them under a different OS identity from the MCP server. A non-secret runtime configuration can follow this shape:

```json5
{
  listen: { host: "127.0.0.1", port: 7788, path: "/mcp" },
  repositories: [
    {
      id: "example-service",
      root: "/srv/repos/example-service",
      allowedClassifications: ["public", "internal"],
      maxPackageFiles: 100,
      maxPackageBytes: 1048576,
    },
  ],
  knowledge: {
    root: "/srv/engineering-knowledge/knowledge",
    requiredStatus: "approved",
  },
  reviewState: {
    root: "/var/lib/engineering-mcp/review-state",
    packageRetentionDays: 7,
    rejectedFindingRetentionDays: 30,
    rawGenerationRunRetentionDays: 14,
  },
  embeddings: {
    baseUrl: "http://127.0.0.1:11434",
    model: "qwen3-embedding:4b",
  },
}
```

Load bearer-token hashes or references from a root-managed secret source, not this configuration. Validate the configuration at startup, refuse writable or unapproved repository roots, and expose a readiness failure until the database migration, canonical revision, and embedding model identity are consistent.

### 12.10 Capture Codex runs with lifecycle hooks

Codex lifecycle hooks provide a reliable place to capture the stable event fields needed for a run envelope. Use:

- `UserPromptSubmit` for `session_id`, `turn_id`, working directory, model, and submitted prompt.
- `PostToolUse` for an allowlisted summary of material tool activity and validation evidence.
- `Stop` for the turn ID and latest assistant message.

Do not make the hook perform embeddings, pattern extraction, or Git operations. It should validate the event, redact or hash disallowed fields, append a small record to a local protected spool, print a valid success response, and exit quickly. A separate worker reads the spool and calls `generation_run_capture` with a capture-only MCP identity.

Example project `.codex/hooks.json`:

```json
{
  "description": "Capture bounded Codex run evidence for approved solution-pattern candidates.",
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "node \"$(git rev-parse --show-toplevel)/.codex/hooks/capture-run.mjs\" prompt",
            "timeout": 3
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "node \"$(git rev-parse --show-toplevel)/.codex/hooks/capture-run.mjs\" tool",
            "timeout": 3
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "node \"$(git rev-parse --show-toplevel)/.codex/hooks/capture-run.mjs\" output",
            "timeout": 3
          }
        ]
      }
    ]
  }
}
```

The hook must be reviewed and trusted through Codex's `/hooks` interface before it will run. Treat the spool as sensitive: mode `0700` directory, `0600` files, size quotas, encryption at rest where required, and short retention.

Do not depend on parsing `transcript_path`; Codex exposes it for convenience but does not define the transcript format as a stable hook interface. Capture stable event fields and explicit artifact manifests instead. `SessionEnd` may be used as a best-effort cleanup signal, but it is delayed and has a very short hook timeout, so it should not be the primary capture mechanism.

The `PostToolUse` handler should keep only approved metadata such as tool category, normalized command or operation hash, exit status, changed-file manifest, test name, and bounded redacted evidence. It must discard environment dumps, credential-bearing arguments, unrestricted command output, and raw production data.

## 13. Installation and provisioning

### 13.1 Install or update Ollama

Use the current official Linux installation method and verify the service before pulling models.

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama --version
curl -fsS http://127.0.0.1:11434/api/version
```

Bind Ollama to loopback unless a private LAN endpoint is explicitly required. Never expose port 11434 directly to the public internet.

### 13.2 Pull local models

```bash
ollama pull qwen3-coder-next
ollama pull qwen3.5:9b
ollama pull qwen3-embedding:4b
ollama list
```

Run direct smoke tests before introducing OpenClaw:

```bash
ollama run qwen3-coder-next "Reply with exactly: coder-ok"

curl -fsS http://127.0.0.1:11434/api/embed \
  -d '{"model":"qwen3-embedding:4b","input":"local knowledge test"}'
```

### 13.3 Install the Moonshot provider

```bash
openclaw plugins install @openclaw/moonshot-provider
openclaw gateway restart
```

Enter the Kimi API key only through the cloud reviewer's interactive agent or provider setup. Do not place it in this document, a repository, shell history, global environment, screenshot, or maintenance auth store.

If Ollama Cloud is deliberately selected instead, configure its credentials and Kimi model route only for `cloud-reviewer`, then replace the reviewer's `moonshot/kimi-k3` model identifier with the identifier reported by the installed OpenClaw/Ollama version. Do not install or authenticate that route in the maintenance agent. The rest of this design assumes the preferred direct Moonshot route.

### 13.4 Create isolated agents

Create local agents non-interactively so they are not seeded with cloud credentials:

```bash
openclaw agents add development \
  --workspace ~/.openclaw/workspace-development \
  --model ollama/qwen3-coder-next \
  --non-interactive

openclaw agents add maintenance \
  --workspace ~/.openclaw/workspace-maintenance \
  --model ollama/qwen3-coder-next \
  --non-interactive
```

Create `cloud-reviewer` interactively and select Moonshot/Kimi authentication only for that agent:

```bash
openclaw agents add cloud-reviewer
```

After merging the policy configuration, verify:

```bash
openclaw agents list --bindings
openclaw models status --agent development
openclaw models status --agent maintenance
openclaw models status --agent cloud-reviewer
openclaw doctor
```

The maintenance status must show a usable local Ollama route and no usable Moonshot credentials.

### 13.5 Index knowledge

Populate the two local workspaces with approved `MEMORY.md` and `memory/` content. Then build and inspect indexes:

```bash
openclaw memory status --index --agent development
openclaw memory index --force --agent development

openclaw memory status --index --agent maintenance
openclaw memory index --force --agent maintenance
```

Run representative retrieval queries and confirm citations point to the correct source and revision.

## 14. Workspace instructions

Each agent workspace should contain an `AGENTS.md` that makes the runtime policy understandable to the model.

### 14.1 Development policy excerpt

```markdown
# Development policy

- Inspect source and retrieve relevant local knowledge before editing.
- Use local tools and the local model for normal development.
- Use Kimi, Codex, or both only for approved escalation conditions.
- Before cloud review, construct a minimal context package and remove secrets,
  customer data, credentials, production payloads, and unrelated files.
- Treat cloud output as advice. Apply changes locally and verify with tests.
- Record review findings as pending candidates; validate them before requesting
  an owner decision. Never promote raw model output directly into knowledge.
- Never deploy or perform destructive operations without explicit approval.
```

### 14.2 Kimi cloud reviewer policy excerpt

```markdown
# Cloud review policy

- Analyze only the supplied context package.
- Do not request secrets, credentials, complete repositories, or production data.
- Return risks, alternatives, recommendations, and verification suggestions.
- Do not claim that commands, tests, or deployments were executed.
- Do not modify files or systems.
```

### 14.3 Codex review policy excerpt

Place repository-wide review invariants in the root `AGENTS.md` and narrower rules in the closest applicable nested `AGENTS.md`.

```markdown
## Code review rules

- Use the engineering-review MCP tools only with the issued review ID.
- Review the requested diff against its acceptance criteria and applicable
  retrieved rules. Do not broaden the repository or knowledge scope.
- Report only actionable correctness, security, compatibility, performance,
  or maintainability findings with file and line evidence.
- Distinguish required findings from optional improvements.
- A recorded finding is a pending candidate, not approved knowledge.
- Captured generation output is a short-lived run envelope, not an approved
  solution pattern; include validation evidence so it can be evaluated locally.
- Never call or request a knowledge-approval or promotion operation.
```

### 14.4 Maintenance policy excerpt

```markdown
# Maintenance policy

- Use only local models, local knowledge, and locally authorized tools.
- Never spawn or select a cloud agent or cloud model.
- Prefer approved runbooks and show evidence for the selected procedure.
- Before a material action, state impact, risk, rollback, and validation.
- Require approval for destructive changes, service restarts, upgrades,
  permission changes, database writes, and production mutations.
- If Ollama is unavailable, fail closed and notify the operator.
```

## 15. Development workflow

### 15.1 Normal local path

1. Classify the request and data sensitivity.
2. Retrieve relevant KB documents with citations.
3. Inspect current source and repository state.
4. Form a local implementation plan.
5. Edit using repository-native conventions.
6. Run targeted validation.
7. Run broader validation when the risk warrants it.
8. Summarize changed files, tests, unresolved risks, and KB updates.

### 15.2 Cloud-assisted path

1. Complete local discovery first.
2. Confirm that cloud escalation criteria are met.
3. Select Kimi, Codex, or both based on review purpose and disclosure cost.
4. Generate and scan a minimal, immutable context package through MCP.
5. Record the reason for escalation, data classification, and manifest hash.
6. Send the package to Kimi through `cloud-reviewer`, or issue its scoped review capability to Codex.
7. Receive structured recommendations and record them as pending candidates.
8. Evaluate each consequential recommendation locally against current source and tests.
9. Apply selected changes with the local development agent.
10. Run tests locally.
11. Record accepted, modified, rejected, duplicate, or superseded dispositions with evidence.
12. Promote only owner-approved, generalized lessons through the KB publishing workflow.

Suggested cloud reviewer response structure:

```text
Assumptions
Risks
Recommended approach
Alternatives and tradeoffs
Diff or plan concerns
Security considerations
Performance considerations
Verification checklist
Candidate findings (severity, category, file/line evidence, recommendation)
```

### 15.3 Local solution-replay path

1. Normalize the new request and retrieve current repository evidence.
2. Call `generation_replay_plan` with the task signature and classification.
3. Inspect the returned pattern match explanations, exclusions, citations, output contract, and validation contract.
4. Reject irrelevant or stale patterns before prompt assembly.
5. Generate locally using the current request and at most two to four approved patterns.
6. Run the required deterministic checks or scoring rubric.
7. Record artifacts, validation results, and the patterns used with `generation_result_record`.
8. Return the local result when its acceptance gate passes.
9. If it fails, try a materially different approved local pattern only when evidence supports it; otherwise stop or request an explicit Codex/Kimi escalation.
10. Feed verified local outcomes into pattern quality metrics without automatically editing canonical pattern documents.

## 16. Maintenance workflow

1. Identify service, environment, severity, and operator authorization.
2. Retrieve the current runbook, known errors, and recent incident knowledge.
3. Collect local evidence without changing state.
4. Propose diagnosis and safe next action.
5. Present impact, risk, rollback, and validation.
6. Obtain approval for material actions.
7. Execute the smallest reversible action.
8. Validate service health and user impact.
9. Roll back immediately if validation fails.
10. Record the incident outcome and proposed runbook improvements locally.

Maintenance must remain local during failures. An outage is not permission to relax the cloud boundary.

## 17. Security controls

### 17.1 Identity and secrets

- Store Moonshot credentials only in `cloud-reviewer`'s auth profile.
- Keep Codex/OpenAI authentication in the supported Codex credential store; do not copy it into OpenClaw or the MCP service.
- Issue distinct MCP credentials for OpenClaw, Codex, the hook capture worker, maintenance, and the publisher; rotate them independently.
- Keep shell-environment credential loading disabled for the Gateway where practical.
- Do not reuse one `agentDir` across agents.
- Restrict permissions on OpenClaw state, model credentials, and workspaces.
- Rotate and revoke the Moonshot key independently of local Ollama operation.
- Give the publisher identity no model access and give model identities no approval or promotion scope.

### 17.2 Network

- Bind the OpenClaw Gateway to loopback unless remote access is required.
- Authenticate the Gateway even on loopback.
- Bind Ollama to loopback or a private management network.
- Bind the engineering MCP listener to loopback and reject non-loopback host headers and forwarded-client claims.
- Deny public inbound access to Ollama.
- Deny all outbound internet access from the MCP service; cloud calls belong to the reviewer clients, not the knowledge service.
- For hard offline maintenance, use a separate process or network namespace with explicit egress denial.

### 17.3 Tools and filesystem

- Scope development and maintenance workspaces to approved repository roots.
- Keep elevated execution disabled by default.
- Require approval for package installation, service control, user or permission changes, database writes, and destructive actions.
- Deny all execution and write-capable tools to the cloud reviewer.
- Restrict Codex MCP writes to `review_candidate_record`; deny decision, promotion, reindexing, filesystem, and general KB enumeration.
- Treat repository content and retrieved documents as potentially untrusted instructions.

### 17.4 Knowledge-base security

- Apply a secret scanner before ingestion.
- Store document classification and owner metadata.
- Record source path, revision, and review date.
- Delete obsolete vectors when source documents are removed.
- Reindex deliberately after embedding-model changes.
- Test for retrieval leakage between development and maintenance workspaces.
- Keep pending, rejected, and expired review state outside approved retrieval indexes.
- Keep raw Codex run envelopes, failed local replays, and pattern candidates outside approved retrieval indexes.
- Encrypt or tightly permission the capture spool, apply quotas and retention, and never capture environment dumps or unrestricted tool output.
- Require an owner-approved Git change before promoted review knowledge becomes canonical.

## 18. Observability and audit

Record at minimum:

- Agent ID and selected model for every run.
- Whether Kimi, Codex, or both were invoked.
- Escalation reason and approving identity when required.
- A manifest or hashes of cloud context-package sources.
- MCP caller identity, review capability, tool name, authorization result, package hash, and candidate state transition.
- Generation run ID, pattern IDs retrieved, match explanations, output contract, validation outcome, and owner disposition.
- Token usage and estimated cloud cost.
- Tool calls, approvals, exit status, and changed files.
- Build, test, lint, and deployment results.
- Knowledge retrieval sources and citations.
- Model, embedding model, OpenClaw, and Ollama versions.

Do not log raw secrets or restricted context packages. Audit metadata should prove what categories and sources were used without duplicating restricted data.

Recommended health checks:

```bash
curl -fsS http://127.0.0.1:11434/api/version
ollama list
openclaw models status --agent development
openclaw models status --agent maintenance
openclaw models status --agent cloud-reviewer
openclaw memory status --index --agent development
openclaw memory status --index --agent maintenance
openclaw mcp doctor engineering-knowledge --probe
openclaw channels status --probe
```

## 19. Test strategy

### 19.1 Functional tests

- Development agent retrieves an ADR and cites the correct source.
- Development agent edits a sample repository and runs tests locally.
- Cloud reviewer receives a sanitized diff and returns advisory output.
- Codex connects to the MCP service, sees only its five allowed tools, reads one scoped package, and returns line-evidenced findings.
- Local agent can accept or reject cloud recommendations and verify locally.
- An accepted, validated candidate produces a reviewable Markdown lesson and becomes retrievable only after merge and reindex.
- Codex prompt, bounded tool summaries, final output, and validation events assemble into one immutable run envelope without parsing a transcript file.
- A validated-success run produces a pattern candidate; after approval and promotion, a similar local request retrieves it and passes its acceptance checks.
- Maintenance agent retrieves a runbook and executes an approved non-destructive diagnostic.

### 19.2 Negative security tests

- Place a fake token in a test `.env`; confirm it is not indexed or exported.
- Request Kimi from the maintenance agent; confirm the request fails.
- Stop Ollama during a maintenance run; confirm there is no cloud fallback.
- Ask maintenance to spawn `cloud-reviewer`; confirm the action is denied.
- Ask the cloud reviewer to execute or modify a file; confirm the tools are unavailable.
- Ask Codex to list arbitrary packages, query maintenance-only knowledge, approve its own finding, or promote knowledge; confirm each request is denied server-side.
- Reuse an expired review capability and a capability for a different repository; confirm both fail closed.
- Include a symlink escape, oversized file, restricted marker, and fake secret in package inputs; confirm package creation fails without partial output.
- Submit the same finding twice; confirm idempotent deduplication rather than duplicate knowledge.
- Attempt to index pending and rejected candidates; confirm they never appear in approved search results.
- Put a fake secret and oversized tool result into hook events; confirm the capture adapter redacts or rejects them and does not leave a partial approved run.
- Attempt to promote a failed, unsafe, expired, or unvalidated generation run; confirm each transition is rejected.
- Change or corrupt a transcript fixture; confirm capture still works because it uses stable hook fields rather than parsing `transcript_path`.
- Present a request matching a pattern's exclusion; confirm the pattern is filtered before local prompt assembly.
- Put malicious instructions in a retrieved document; confirm workspace and tool policies remain authoritative.
- Attempt a `/model` override in maintenance; confirm the cloud model is unavailable or unauthenticated.
- Check scheduled maintenance jobs for an explicit local model and empty fallback list.

### 19.3 Retrieval evaluation

Maintain a small versioned evaluation set containing representative questions and expected source documents. Measure:

- Recall of the correct source in the top results.
- Citation accuracy.
- Stale-document retrieval rate.
- Cross-agent leakage rate.
- Pending/rejected candidate leakage rate, which must remain zero.
- Correct solution-pattern retrieval and exclusion accuracy.
- Local replay task-success rate compared with local generation without patterns.
- Query latency.

Do not select an embedding model based only on generic benchmarks. Evaluate it against the project's code, architecture vocabulary, and runbooks.

### 19.4 Performance baseline

Measure rather than assume:

- Model load time.
- Time to first token.
- Decode tokens per second.
- Memory usage at 16K, 32K, and 64K context.
- Knowledge query latency.
- MCP package-build and tool-call latency.
- Hook enqueue latency, capture-worker lag, replay-plan latency, and pattern-context token count.
- End-to-end coding task duration.
- Kimi and Codex escalation frequency, agreement rate, accepted-finding rate, false-positive rate, and cost.

Establish production SLOs only after running representative development and maintenance workloads on the target GX10.

## 20. Deployment plan

### Phase 0: Baseline and backup

- Record current versions and configuration.
- Back up OpenClaw configuration and agent state.
- Confirm repository and knowledge source ownership.
- Define classification and cloud-export policy.

### Phase 1: Local inference

- Update Ollama.
- Pull and smoke-test local models.
- Configure 64K development context.
- Benchmark memory and responsiveness.

### Phase 2: Local agents and knowledge

- Create development and maintenance agents without cloud credentials.
- Populate curated knowledge.
- Configure local embeddings and build indexes.
- Pass functional and negative maintenance tests.

### Phase 3: MCP knowledge boundary

- Deploy the loopback-only MCP service with outbound network denied.
- Register separate OpenClaw and Codex identities and tool allowlists.
- Implement package sanitization, capability expiry, candidate states, and append-only audit metadata.
- Probe both clients and pass MCP negative security tests.
- Keep promotion disabled until candidate validation is stable.

### Phase 4: Kimi cloud review

- Install the Moonshot provider.
- Create the isolated cloud reviewer.
- Add the Moonshot key only to that agent.
- Enforce read-only advisory tools.
- Test sanitized context export and audit events.

### Phase 5: Codex review and learning

- Connect a local Codex client with the project-scoped MCP configuration.
- Install and trust bounded `UserPromptSubmit`, `PostToolUse`, and `Stop` capture hooks.
- Verify spool permissions, redaction, quotas, worker idempotency, and raw-run retention.
- Add two or three consequential, scoped review rules to `AGENTS.md`.
- Test one violation, one safe counterexample, and one unrelated change per rule.
- Enable candidate recording, then validation and owner decisions.
- Enable promotion only after state-transition and index-leakage tests pass.
- Promote a small set of validated solution patterns and compare local replay against a no-pattern baseline.

### Phase 6: Pilot

- Pilot development on non-sensitive repositories.
- Pilot maintenance in staging.
- Measure retrieval quality, cloud invocation rate, cost, and task success.
- Review false escalations and missed escalations.

### Phase 7: Production

- Enable production maintenance only after fail-closed tests pass.
- Require approval for high-risk actions.
- Schedule KB freshness and index health checks.
- Review cloud disclosures and cost regularly.

## 21. Rollback and recovery

If cloud integration causes an incident:

1. Revoke or disable the Moonshot credential.
2. Disable the Codex MCP entry or revoke the Codex review token.
3. Remove `cloud-reviewer` from development's `allowAgents` list.
4. Restart the affected clients and Gateway.
5. Verify local development and maintenance model status.
6. Review audit metadata for affected cloud invocations and packages.

If the MCP service or review-learning pipeline is compromised:

1. Stop the MCP service and revoke all MCP tokens.
2. Disable MCP entries in OpenClaw and Codex without changing local Ollama operation.
3. Preserve the review-state database and audit log for investigation.
4. Revert unmerged generated KB changes and quarantine any promoted entries since the last known-good audit point.
5. Rebuild indexes from the last known-good canonical Markdown revision.
6. Re-enable read-only retrieval first; restore candidate writes and promotion only after security validation.

If a memory-model change causes retrieval failure:

1. Stop automatic promotion or ingestion.
2. Restore the previous embedding configuration and index backup, or force a clean reindex.
3. Run the retrieval evaluation set.
4. Re-enable retrieval only after citation and leakage tests pass.

If a promoted solution pattern causes bad local replays:

1. Mark the pattern retired in the workflow state and block it from new replay plans.
2. Revert or supersede its canonical Markdown change through Git.
3. Reindex the MCP and OpenClaw stores at the corrected revision.
4. Replay the affected evaluation cases and record why the pattern failed.
5. Restore it only after applicability, exclusions, exemplar, and validation contract are corrected and reapproved.

If local inference is unavailable:

- Development may use a separately approved cloud-only emergency workflow if policy permits.
- Maintenance must fail closed and remain unavailable until local inference is restored, unless a human operator takes control outside the agent workflow.

## 22. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Sensitive code is sent to Kimi or Codex | Confidentiality breach | Explicit context packaging, classification, secret scanning, scoped MCP results, approval, and audit. |
| Maintenance silently uses cloud | Policy violation | Empty fallbacks, local utility model, no cloud auth, no cloud delegation, negative tests, optional process isolation. |
| Local model makes unsafe changes | Service or code damage | Sandbox, approvals, reversible actions, tests, rollback plan, least privilege. |
| Knowledge is stale | Incorrect decisions | Ownership metadata, expiry dates, provenance, retrieval citations, freshness reports. |
| Full context exhausts memory | Latency or OOM | Start at 64K, retrieval-first workflow, context compaction, memory monitoring. |
| Cloud recommendation is accepted blindly | Defects or security issues | Advisory-only cloud agent; local implementation and verification remain mandatory. |
| Model fallback is mistaken for optimization | Uncontrolled routing | Use explicit cloud subagent; reserve fallback for availability recovery. |
| Shared Gateway is insufficient isolation | Potential policy bypass | Separate maintenance process/container/OS account for hard compliance. |
| Model output pollutes the KB | Repeated bad guidance | Candidate state, local validation, owner decision, deterministic promotion, provenance, and rollback. |
| MCP tool scope is broader than intended | Data leakage or unauthorized writes | Separate identities, short-lived capabilities, server-side authorization, tool filters, and negative tests. |
| Kimi and Codex disagree | Review ambiguity and wasted effort | Require evidence, reproduce locally, track dispositions, and use an accountable owner for the decision. |
| Codex capture stores sensitive or excessive data | Confidentiality and retention breach | Event allowlists, redaction, hashes instead of raw output, protected spool, quotas, classification, and short retention. |
| Local model copies a stale exemplar | Incorrect or insecure output | Current-source inspection, applicability and exclusion checks, source revisions, expiry, bounded exemplars, and deterministic validation. |
| Text similarity is mistaken for task success | Superficially similar but broken output | Evaluate behavior, tests, schemas, citations, and owner rubrics; keep similarity as a secondary metric. |
| Low-quality cloud output becomes a pattern | Systematic repeated defects | Require validated-success state, generalization review, owner approval, provenance, held-out replay tests, and rollback. |

## 23. Acceptance criteria

The system is ready for production when all of the following are true:

- Local development completes a representative coding task with retrieval, edits, and tests.
- Cloud review can be invoked explicitly and cannot execute or write locally.
- Codex can read only its issued review package and approved in-scope guidance through MCP.
- Every cloud invocation records an escalation reason and context-source manifest.
- Secret-scanning tests prevent restricted test data from entering cloud packages.
- Maintenance has no usable cloud credentials, cloud fallbacks, or cloud delegation path.
- Maintenance fails closed when Ollama is stopped.
- Maintenance scheduled jobs specify local models and no fallbacks.
- Knowledge results include correct citations and pass the retrieval evaluation set.
- Pending and rejected findings never appear in approved knowledge retrieval.
- No cloud identity can approve or promote a finding; an authorized owner can promote a validated candidate through a reviewable Git change.
- A promoted lesson is retrievable by both OpenClaw development and Codex in the intended scope after reindexing.
- Codex hooks assemble bounded generation-run envelopes without relying on transcript parsing, and secret/size negative tests pass.
- Only validated-success runs can seed solution patterns, and only owner-approved patterns enter canonical retrieval.
- A representative similar request is completed locally using retrieved patterns and passes the same behavioral or rubric checks as the approved exemplar.
- Requests matching a pattern exclusion do not retrieve or apply that pattern.
- Model and embedding memory usage remain within the measured GX10 budget.
- Rollback procedures have been exercised in staging.

## 24. Future enhancements

- Replace the built-in memory backend with QMD when the approved knowledge
  corpus justifies the extra component. Repository/code indexing remains in
  PostgreSQL, AGE, and Milvus rather than the conversational memory backend.
- Add Memory Wiki for structured claims, contradiction reports, and freshness dashboards.
- Add cost budgets and rate limits for the cloud reviewer.
- Add reviewer-routing evaluation that selects local-only, Kimi, Codex, or dual review from measured task characteristics.
- Add policy-as-code checks that reject maintenance configurations containing cloud providers or fallbacks.
- Add evaluation suites for patch quality, test success, retrieval grounding, and security-policy adherence.
- When the approved corpus is large enough, benchmark a small local adapter or fine-tune against the RAG-only baseline before deciding whether to train.
- Add automatic pattern retirement when source contracts disappear or replay success falls below an owner-defined threshold.
- Run the maintenance Gateway in an egress-isolated service for hard offline enforcement.
- Add a local second-pass reviewer model if Devstral or another coding model demonstrates a measured quality benefit within the memory budget.

## 25. References

- [ASUS Ascent GX10 specifications](https://press.asus.com/news/press-releases/asus-ascent-gx10-ai-supercomputer/)
- [OpenClaw multi-agent routing](https://docs.openclaw.ai/concepts/multi-agent)
- [OpenClaw per-agent configuration](https://docs.openclaw.ai/gateway/config-agents)
- [OpenClaw model failover](https://docs.openclaw.ai/model-failover)
- [OpenClaw subagents](https://docs.openclaw.ai/subagents)
- [OpenClaw Ollama provider and local embeddings](https://docs.openclaw.ai/providers/ollama)
- [OpenClaw memory overview](https://docs.openclaw.ai/concepts/memory)
- [OpenClaw QMD memory engine](https://docs.openclaw.ai/concepts/memory-qmd)
- [OpenClaw Memory Wiki](https://docs.openclaw.ai/plugins/memory-wiki)
- [OpenClaw MCP client and server commands](https://docs.openclaw.ai/cli/mcp)
- [Codex Model Context Protocol configuration](https://learn.chatgpt.com/docs/extend/mcp)
- [Codex code review](https://learn.chatgpt.com/docs/code-review)
- [Codex lifecycle hooks](https://learn.chatgpt.com/docs/hooks)
- [Custom Code Review rules for Codex](https://developers.openai.com/blog/custom-code-review-rules-for-codex)
- [Kimi in OpenClaw](https://platform.kimi.ai/docs/guide/use-kimi-in-openclaw)
- [Kimi K3](https://platform.kimi.ai/docs/guide/kimi-k3-quickstart)
- [Ollama Cloud models](https://docs.ollama.com/cloud)
- [Kimi K3 on Ollama Cloud](https://ollama.com/library/kimi-k3)
- [Qwen3-Coder-Next on Ollama](https://ollama.com/library/qwen3-coder-next)
- [Qwen3 Embedding on Ollama](https://ollama.com/library/qwen3-embedding)
