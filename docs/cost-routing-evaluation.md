# Local Execution and Remote Review Evaluation

## Outcome

The platform already has a stronger knowledge and evidence foundation than a
simple cost router, but the complete cost-routing outcome is not yet proven.
Local Ollama execution, RAG-first atomic checkpoint routing, read-only Codex
review, reviewed knowledge, repository graphs, and code graphs are implemented
or configured. Automatic context-package construction and operational
benchmarks still need end-to-end evidence.

Do not claim a 40–70% cost reduction, faster delivery, or unchanged quality
until the evaluation protocol in this document passes on representative work.
For ChatGPT-authenticated Codex, measure account usage/credits and cloud calls
avoided rather than presenting an invented per-token API cost.

## Capability scorecard

Status meanings:

- **Implemented:** executable code and automated tests exist.
- **Configured:** a deployable client/runtime configuration exists, but the
  repository does not prove a live end-to-end run on the operator's machine.
- **Partial:** some enforcement exists, with a material control still missing.
- **Unmeasured:** the result may be plausible but is not supported by a
  repeatable benchmark.

| Target capability | Status | Current evidence | Remaining proof or work |
|---|---|---|---|
| Local model handles routine development | Configured | OpenClaw `developer` uses local Ollama with no fallback. | Run representative tasks and record local worker participation and validation success. |
| Maintenance is local-only | Configured + policy-tested | OpenClaw uses an `ollama/*` allowlist; work-packet policy rejects maintenance cloud review. | Deploy a hard-offline maintenance process and pass negative egress tests when this is a compliance boundary. |
| Task classification | Partial | Work packets require `development` or `maintenance`, mode, data class, and categories. | Add an OpenClaw coordinator step that always emits the packet before delegated execution. |
| Risk assessment | Implemented at packet boundary | Protected categories, destructive actions, restricted data, approvals, and disclosure rules are evaluated deterministically. | Add organization-specific category rules and actor authorization at the enterprise gateway. |
| Bounded context and write scope | Partial | Allowed/forbidden file patterns, patch-byte, file-count, and diff-line limits are enforced; supplied review manifests are stored immutably. | Build an automatic minimal cloud context packager with secret/DLP scanning before export. |
| Result verification | Implemented locally | Candidate patches apply in a disposable clone; exact argv checks run with timeouts; scope, diff limits, side effects, and binary patches are checked. | Run the verifier inside an egress-denied OS/container sandbox for untrusted repositories. |
| Cheap/local worker delegation | Configured | OpenClaw is the orchestrator; Ollama is the default worker. | Add routing telemetry and end-to-end execution fixtures. |
| Codex final review | Configured + persistence implemented | An allowed RAG miss enters the provider-gated read-only OpenAI lane; `review_record` stores reviewer/model/verdict plus raw-output and context-manifest artifacts. Cloud cannot revise candidate content. | Automate sanitized package issuance and record a complete live review trace. |
| Review improvements become reusable | Implemented with approval/read-back gates | Ollama-revised content is validated and approved in PostgreSQL, embedded by the outbox worker, and must pass Milvus UUID read-back before queue advancement. | Build quality/freshness evaluation for promoted review lessons. |
| PostgreSQL knowledge authority | Implemented | Workflow state, provenance, approvals, relationships, code snapshots, and outbox are canonical. | Add enterprise tenant isolation and managed HA operation. |
| Semantic reuse through Milvus | Implemented | Approved knowledge, repository relationships, and selected code entities use stable PostgreSQL IDs. | Measure retrieval precision and local regeneration quality. |
| Transparent and measurable routing | Implemented for checkpoint provenance; usage metrics partial | PostgreSQL stores route, mode, model/provider, RAG results, influence, candidate, evidence, authorization, and every transition. | Add model tokens/cost and latency as first-class metrics. |
| 40–70% savings | Unmeasured | No platform-specific A/B benchmark exists. | Run the protocol below; report measured distributions, not a marketing estimate. |
| Faster delivery without lower quality | Unmeasured | Unit/integration tests cover platform controls, not representative coding-task throughput. | Compare wall time, validation rate, review findings, and accepted outcomes against Codex-only and local-only baselines. |

## Required review and learning lifecycle

Remote review is advisory and policy-selected, never a provider fallback.
`execution_mode=auto` starts a required review without a human acceptance
prompt; `manual` adds that prompt. Both retain human knowledge approval:

```text
FIFO activation -> approved RAG lookup
  -> local Ollama implementation
  -> deterministic work-packet verification
  -> on allowed RAG miss: minimal sanitized context package
  -> read-only Codex review
  -> review_record + immutable review artifact
  -> Ollama reproduction and validation of accepted recommendations
  -> pending generalized knowledge candidate
  -> accountable approval in PostgreSQL
  -> outbox-driven embedding in Milvus
  -> Milvus UUID read-back -> next FIFO task
```

Raw review output is valuable evidence but is not automatically searchable
knowledge. PostgreSQL and the artifact store retain it while pending. Milvus
receives only approved, generalized improvements. Maintenance can retrieve
those previously approved lessons locally, but it cannot invoke a remote model
during the maintenance task.

The exact storage and promotion contract is documented in
[Remote Review and Local Learning](remote-review-learning.md).

## Evaluation protocol

Build a versioned suite of at least 30 representative tasks:

- 10 read-only explanation, search, and impact-analysis tasks.
- 10 bounded tests, documentation, and localized patch tasks.
- 5 cross-repository or architecture tasks.
- 5 sensitive or maintenance tasks that must exercise rejection and local-only
  behavior.

Run each eligible task through three lanes where policy permits:

1. Codex-only baseline.
2. Approved RAG-hit local Ollama implementation without remote review.
3. RAG-miss local Ollama implementation plus automatic read-only Codex review.

Record for every run:

- local and cloud model/provider;
- route decision and reason;
- input/output token or account-usage measurements when available;
- wall-clock latency;
- work-packet rejection or acceptance reason;
- changed files and diff size;
- deterministic check results;
- remote-review findings by severity;
- accepted, rejected, duplicate, and superseded recommendations;
- final owner disposition and rollback outcome;
- knowledge candidate and approved item IDs.

## Acceptance gates

The platform may advertise a measured optimization only when:

- all maintenance and restricted-data cloud attempts are rejected;
- every accepted patch passes its declared deterministic checks;
- no accepted patch escapes its file or diff limits;
- every policy-required remote review records its exported manifest; auto mode
  starts it without manual acceptance, while manual mode records that decision;
- raw model/review output is never embedded before approval;
- the reviewed hybrid lane has no statistically meaningful regression in final
  task acceptance compared with the Codex-only baseline;
- cost/usage and latency claims include the task set, model versions, hardware,
  dates, failures, and calculation method.

The first optimization target should be read-only and bounded low-risk work.
Architecture, security, destructive, production, and ambiguous tasks remain
explicit judgment lanes.
