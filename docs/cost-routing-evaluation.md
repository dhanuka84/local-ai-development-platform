# Local Execution and Remote Review Evaluation

Updated September 12, 2026. The [developer guide](developer-guide.md) and
[functional E2E guide](agent-ready-e2e.md) describe the tested local behavior.
This evaluation protocol addresses representative quality, cost and adoption
claims that remain unmeasured; passing functional tests does not establish them.

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
| Local model handles routine development | Demonstrated for a bounded pilot; broader quality unmeasured | Retained real-Ollama Task A/Task B pilot and actual pilot-executable E2E using deterministic model fixtures. | Run representative cohorts with local worker participation, failures, retries and validation outcomes. |
| Maintenance is local-only | Configured + policy-tested | OpenClaw uses an `ollama/*` allowlist; work-packet policy rejects maintenance cloud review. | Deploy a hard-offline maintenance process and pass negative egress tests when this is a compliance boundary. |
| Task classification | Partial | Work packets require `development` or `maintenance`, mode, data class, and categories. | Add an OpenClaw coordinator step that always emits the packet before delegated execution. |
| Risk assessment | Implemented at packet boundary | Protected categories, destructive actions, restricted data, approvals, and disclosure rules are evaluated deterministically. | Add organization-specific category rules and actor authorization at the enterprise gateway. |
| Bounded context and write scope | Partial | Allowed/forbidden file patterns, patch-byte, file-count, and diff-line limits are enforced; supplied review manifests are stored immutably. | Build an automatic minimal cloud context packager with secret/DLP scanning before export. |
| Result verification | Implemented locally | Candidate patches apply in a disposable clone; exact argv checks run with timeouts; scope, diff limits, side effects, and binary patches are checked. | Run the verifier inside an egress-denied OS/container sandbox for untrusted repositories. |
| Bounded local delegation | Implemented and functionally tested | Task-scoped CLI credentials, actual gateway expiry/revocation, issuer-role withdrawal, packet verification and complete traces. OpenClaw controller contracts remain separate. | Complete the general isolated runner and representative adoption evaluation. |
| Codex final review | Configured + persistence implemented | An allowed RAG miss enters the provider-gated read-only OpenAI lane; `review_record` stores reviewer/model/verdict plus raw-output and context-manifest artifacts. Cloud cannot revise candidate content. | Automate sanitized package issuance and record a complete live review trace. |
| Validated improvements become reusable | Implemented with explicit user approval/read-back gates | Actual gateway/worker E2E verifies trusted validation, exact-version publication, source withdrawal, stale projections and Task B reuse with its new entry pending. | Measure retrieval and outcome quality on representative review-derived KB entries. |
| PostgreSQL knowledge authority | Implemented | Workflow state, provenance, approvals, relationships, code snapshots, and outbox are canonical. | Add enterprise tenant isolation and managed HA operation. |
| Semantic reuse through Milvus | Implemented | Approved knowledge, repository relationships, and selected code entities use stable PostgreSQL IDs. | Measure retrieval precision and local regeneration quality. |
| Transparent and measurable routing | Implemented provenance, governed metrics and trace export; model economics unmeasured | PostgreSQL records routes, providers, exact artifacts and decisions. Four fixed metric formulas and actual collector export are functionally tested. | Publish retained real definitions with current validation/access and run representative model cost/latency benchmarks. |
| 40–70% savings | Unmeasured | No platform-specific A/B benchmark exists. | Run the protocol below; report measured distributions, not a marketing estimate. |
| Faster delivery without lower quality | Unmeasured | Unit/integration tests cover platform controls, not representative coding-task throughput. | Compare wall time, validation rate, review findings, and accepted outcomes against Codex-only and local-only baselines. |

## Required review and learning lifecycle

Remote review is advisory and policy-selected, never a provider fallback.
`execution_mode=auto` starts a required review without a human acceptance
prompt; `manual` adds that prompt. Both retain explicit user approval when
publishing generated KB entries. This diagram is the new-knowledge path:

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

Eligible validated reuse has a separate completion event after recorded context
and trusted validation checks; its newly generated candidate remains pending.

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
