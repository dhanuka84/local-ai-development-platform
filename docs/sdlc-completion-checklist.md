# AI-native SDLC functional completion

All twelve items in the September 13 scope passed local functional acceptance
on September 14, 2026. This closes the configured feature and incident profiles;
live target acceptance and representative adoption measurements remain below.
Generated knowledge approvals stay separate from execution.

The tested branch was `feature/ai-native-sdlc-20260913`, based on commit
`645bd582d8eaead73e4ed11114926a33d7bde54d` plus the retained working source.
Both suites report source SHA-256
`f6b2c91ca4ac1fb3ae811870c402d37a3f77730c4a3d0908e0be8f23669b1ec7`.
This snapshot, rather than the base commit alone, identifies the implementation
under test. The [runtime guide](sdlc-runtime.md) explains configuration and use;
[the scope review](documentation-scope-validation-20260914.md) records documentation validation.

| Scope | Required functional proof | Status |
|---|---|---|
| A01 | Local intent drafting/clarification and accepted criteria linked through plan, files, patch, protected checks and delivery | Passed — operator CLI and independent feature E2Es |
| A02 | Durable bounded repair, restart, stale-worker fencing, cancellation and interrupted-effect reconciliation | Passed — lease, crash, backup/restore and read-only reconciliation E2Es |
| A03 | Exact local model/image packages, tools/budgets, regression qualification, activation and rollback | Passed — package canaries, negative gates and real local-model feature E2E |
| A04 | Current semantic/structural context, exact active code revision/symbol bridges and refreshed source windows | Passed — product projection, real analyzer/code bridge, stale-head and native incident E2Es |
| A05 | Separate evaluator executes protected tests in an isolated environment; rejected cases cannot complete | Passed — repair/held-out checks and four adversarial sandbox cases |
| A06 | Expiring owner/workload/run/product/source/field/purpose/environment authority, concurrency and total budgets | Passed — policy, live grant/revocation, resource/budget and source denials |
| A07 | Native Kafka, S3 lake, Loki, Prometheus and audit readers; forge, CI, artifact/staging read-back | Passed — native services, stable action IDs, delivery crash recovery and unchanged Kafka consumer-group offsets |
| A08 | Competing KB-grounded diagnoses using five source kinds; separate remedy and technical/business recovery | Passed — misleading history, delayed lake, failed remedy and two new-window business criteria |
| A09 | CLI submission, clarification, progress, costs, blockers, trace/evidence, pause/resume/cancel/reconcile | Passed — actual CLI processes, stable paginated trace and hashed evidence export |
| A10 | Pending KB/requirement/test/runbook/package improvements and regression-controlled activation/rollback | Passed — idempotent pending feedback and immutable package campaigns/decisions |
| A11 | Correlated model/source/handoff/action/denial/retry/outcome evidence and distinct fixture/model measurements | Passed — exact artifacts, early MCP/auth denials, scoped trace/export and acceptance report |
| A12 | Compatible disposable services, full feature/incident paths, database/artifact restore and resumption | Passed — 28/28 foundation requirements plus 12/12 SDLC requirements; repository and policy checks |

The [coverage map](../tests/sdlc-coverage.json) names the executed test for each
item. The feature proof injected stale context, an incorrect patch, denied
operations and a publisher interruption after the native PR write. It restored
the database and artifact store, reclaimed the same fenced step and reconciled
native effects. A separate absent-effect reconciliation correctly ended blocked.
The incident proof injected delayed data, contradicted history, invalid schemas,
field denial, expired authority, exhausted query budgets and a failed remedy.
It completed only after one actual remedy write and independently observed
technical plus business recovery.

## Executed evidence

| Check | Result and retained receipt |
|---|---|
| `make check` | Passed formatting, vet, race-enabled package tests and script checks; `.local/sdlc-completion-20260913T212900Z/final-make-check.log` |
| `make authz-policy-test` | Passed 129 policy assertions; `final-policy.log` in the same directory |
| Combined functional acceptance with real local Qwen | Passed; `final-functional-trace.log` in the same directory |
| Existing suite | 28/28 requirements, 11 suites, 142 passing test/subtest results; `.local/agent-ready-e2e/run-20260914T001943Z-xnJ9qc/summary.json` |
| Native SDLC suite | 12/12 requirements, 13 passing test/subtest results; `.local/sdlc-e2e/run-20260914T002157Z-BQD5Zr/summary.json` |

The evaluator image was
`sha256:df6bd781900cd3ff5a1bb3f8ba7c800dd85b4348f5cea002ad422d583fa09d3c`.
The real local-model feature run `b2fabd9f-87f9-4569-a69e-0a0caef62080`
completed using `qwen3.6:35b` at digest
`07d35212591fc27746f0a317c975a6d68754fb38e9053d82e25f06057af28522`:
two model calls, one rejected candidate, 13,431 actual tokens and 35,444 ms
reported model inference. Conservative reserved token charges are recorded
separately from actual use. This is a synthetic product task using a real local
model, not a representative quality, latency or adoption cohort.

Native incident run `340a364b-0613-459c-be7b-036174bf5c6f` used 13 source
queries and 260 reserved rows. Its diagnostic model responses were deterministic
protocol fixtures. Source protocols, fixed remedy effects, distinct worker
processes and verification were executed against actual disposable services.
The summary and immutable artifacts preserve that distinction and all failed
attempts; no model assertion substitutes for independent completion checks.

## Remaining target acceptance

Live cutover, representative adoption cohorts and organization-specific
identity, storage, retention, availability and production change windows are
deferred acceptance. They do not defer the local functional proofs above.

| Target item | Exact prerequisite and required evidence |
|---|---|
| L12 — Retained pilot definitions | Authenticated target runtime, current exact definition versions and local validation, then authorized publication and governed query read-back |
| L18 — Live rollout | Named deployment, available credentials/vault, retained compatible images and data backup, change window, rollout/rollback and target-service checks |
| E02 — Adoption/model quality | Agreed representative task cohort and baseline; measured 7/14-day observation windows and an attributed stage decision |
| E06 — Ownership | Named target deployment, QA, operations and escalation owners and their acceptance |
| E07 — Retention/storage | Selected retention, hold and deletion requirements; implement and test any required archive/deletion integration against that policy |
| E08 — Enterprise identity/HA | Target IdP, tenant/workload mapping, cluster, storage and recovery objectives; measured failover and restore evidence |
| Additional native vendors | Named forge, CI, lakehouse or deployment target plus credentials, reviewed contract and positive/negative conformance tests |

The scope diagram and [assessment](sdlc-gap-assessment.md) retain broader SDLC
expectations. Parallel general-purpose planning, arbitrary vendor integrations
and universal production autonomy are not established by these serial profiles.
