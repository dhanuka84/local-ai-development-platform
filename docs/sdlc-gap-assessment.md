# AI-native SDLC platform gap assessment

Navigation: [Lifecycle usage guide](sdlc-guide.md) · [Documentation index](README.md) ·
[Expectations and scope](ai-native-sdlc-expectations.md) ·
[Existing agent-ready checklist](agent-ready-gap-checklist.md).
The [documentation scope review](documentation-scope-validation-20260913.md)
records the repository-wide claim and example corrections.

Baseline assessment: September 13, 2026 against `main` revision
`9fba87f23f0e733a6bba0528b505f647bbac5aef`. Updated with implementation progress
on `feature/ai-native-sdlc-20260913`; see the [validation receipt](ai-native-sdlc-implementation-20260913.md).

## Assessment outcome

The product target is an AI-native, AI-centric SDLC platform: agents progress
authorized intent to verified product outcomes, and people direct intent,
constraints and accountable decisions. A shared structural/semantic KB supplies
product, BRS, code, feature, operational and incident context throughout that
work. MCP tools collect bounded source evidence and feed evaluated records
back into the KB. Each agent role has distinct responsibilities, delegated
access and an accountable owner; the whole path must be auditable. The
[revised lifecycle guide](sdlc-guide.md) defines the agent execution loop and
assigns agent responsibilities across all 15 stages.

The code is a **governed agent foundation with product context, evaluated source
ingestion and bounded local task demonstrations**. Its authoritative context, workflow state, permission checks,
verification and knowledge controls should be retained. The missing core is a
durable, evaluated execution loop over that shared product KB, with source
adapters and role-specific governance connecting the work between those
boundaries. Versioned product/BRS/feature/intent/observation/incident records
and a bounded HTTP source-view protocol are now implemented. Native Kafka,
lake, log and audit bridges, delivery effects and a complete execution
supervisor still require work.

The initial assessment treated manual handoffs and external systems as parts
of a team delivery procedure. Under this target, agents need usable adapters,
current context, independent evaluators and authority to complete those handoffs.
Requirements, context and model evaluation therefore move into the first
implementation slice. Production integrations still depend on their target.

There are **12 primary AI-native capability gaps (A01–A12)**. The **16 earlier
delivery findings (G01–G16)** remain supporting evidence and are cross-mapped
below. These are two views of overlapping work, not 28 independent gaps.
Priorities and owner roles are recommendations, not assigned commitments,
estimates or an industry maturity score.

**Alignment verdict: partial.** The selected storage, projection, MCP, identity
and knowledge-governance foundations support the direction. They do not yet
implement the complete AI-native SDLC operating model. In particular, a
successful code/KB task is not evidence of an agent delivering a product feature
or resolving a production incident from collected evidence.

The [expectations register EXP-01–EXP-10](ai-native-sdlc-expectations.md#expectations-and-related-functionality)
maps each requested behavior to these gaps and observable completion evidence.
The [scope diagram](diagrams/ai-native-sdlc-scope.svg) depicts the intended
architecture; it does not represent the observed deployment.

### What changes in the evaluation

| Question | AI-native acceptance expectation |
|---|---|
| Does an AI help at a lifecycle stage? | Can an agent perform the authorized work, inspect the result and continue or escalate from evidence? |
| Does a workflow remember status? | Can a durable execution supervisor resume, replan, enforce budgets and reconcile partial external effects? |
| Can the system retrieve knowledge? | Do both KB dimensions connect BRS, features, code, operations and observations into current, scoped context at every stage? |
| Can MCP connect a source? | Are source queries bounded and authorized, and are their results validated, time/version-bound and eligible before KB reuse? |
| Does an agent have a role label? | Does its authenticated identity carry enforceable responsibilities and permissions, a delegator and an accountable owner? |
| Are there logs? | Can an authorized reviewer reconstruct source reads, decisions, effects, denials and failures across the whole task? |
| Can an agent summarize an incident? | Can it compare live evidence with BRS/code/history, test competing hypotheses and verify separately authorized business recovery? |
| Are generated changes tested? | Are accepted criteria protected, evaluators independently controlled, and both product and agent behavior assessed? |
| Are tools and deployment scripts available? | Can agents call them through typed scoped adapters and verify the effects in the actual target? |
| Are humans assigned to every stage? | Can humans direct outcomes and exceptions while delegated routine work progresses without repeated permission requests? |
| Is a useful answer captured? | Can validated feedback improve knowledge and agent/config releases without automatic self-approval? |

The clearest source evidence is
[`development-local.lobster`](../automation/workflows/development-local.lobster):
it requires a packet and patch, then runs evaluate/verify.
[`controller.ts`](../automation/openclaw-plugin/src/controller.ts) creates,
queues, transitions and mirrors workflow state. These establish useful
contracts but do not constitute a general agent that produces and delivers a
change from product intent.

## Implementation alignment with each expectation

This matrix includes the implementation described in the
[product KB guide](product-knowledge-and-evaluated-sources.md). **Partial** means
required behavior remains missing. **Not implemented end to end** means the
complete path is absent even when supporting tools exist. Composite statuses
are not a maturity percentage. Live availability remains a separate finding.

| Expectation | Alignment | Implemented evidence | Missing behavior needed for the expectation | Gaps |
|---|---|---|---|---|
| EXP-01 — Two-dimensional product KB | Partial | Versioned [product records](../internal/domain/product.go), local semantic projection/read-back, PostgreSQL structural context and [AGE product projection](../internal/age/product.go) | Automatic code-symbol/product bridging, deeper lifecycle relationships and richer task relevance | [A01](#a01), [A04](#a04) |
| EXP-02 — KB in every SDLC stage | Partial | [Accepted intent context](../internal/service/intent.go), product context and existing task-context recording | Autonomous stage-specific construction and requirement-to-test-to-release/effect progression | [A01](#a01), [A02](#a02), [A04](#a04), [A07](#a07), [A10](#a10) |
| EXP-03 — MCP sources and actions | Partial | [Eleven product tools](../internal/mcpserver/product.go), bounded HTTP source-view protocol, existing graph/metric tools | Native system bridges, source-wide budgets and forge/CI/artifact/environment actions with read-back | [A07](#a07), [A12](#a12) |
| EXP-04 — Evaluated ingestion and reuse | Partial | Schema/time/field/offset/snapshot validation, immutable receipts, idempotent observation retention and eligibility checks | Native source truth/coverage verification, broader correction/enrichment lineage and generalized feedback pipelines | [A04](#a04), [A10](#a10) |
| EXP-05 — Accountable agent roles | Partial | [Principal/workflow records](../internal/domain/workflow.go), [task delegation](../internal/domain/delegation.go), human role and validation-executor boundaries | Versioned packages for the target responsibilities, general role-capability delegation and explicit owner/delegator attribution across all agent work | [A03](#a03), [A06](#a06) |
| EXP-06 — Access control throughout | Partial | Cerbos, expiring Development task credentials, source-purpose/field policies and retained-observation reauthorization | General role delegation, per-product/environment grants, execution isolation and total resource budgets | [A06](#a06), [A12](#a12) |
| EXP-07 — Complete audit | Partial | Original generation bytes, input hashes, source receipts, owner/role attribution, immutable evaluations and existing traces | Complete external reads/actions, agent handoffs and accepted-outcome measurements | [A11](#a11), [A12](#a12) |
| EXP-08 — End-to-end agent execution | Partial | Protected versioned intent bindings, deterministic observation evaluator, workflow states, controller and bounded pilot | Durable plan/action/evaluate/repair supervisor, independent product-test execution, delivery reconciliation and exception interface | [A01](#a01), [A02](#a02), [A03](#a03), [A05](#a05), [A07](#a07), [A09](#a09) |
| EXP-09 — KB-driven incident resolution | Not implemented end to end | Bounded source collection, BRS-bound reconciliation with satisfied/violated/inconclusive results, platform recovery commands | Competing causal hypotheses, diagnostic agent, separately authorized remediation and independently observed technical/business recovery | [A04](#a04), [A07](#a07), [A08](#a08), [A11](#a11) |
| EXP-10 — Governed improvement | Partial | [Capture/validation/review services](../internal/service/service.go), exact-version knowledge decisions and validated reuse | Observation-to-requirement/test/runbook feedback, versioned agent/config improvements, held-out evaluation, controlled rollout/rollback and measured accepted outcomes | [A03](#a03), [A05](#a05), [A10](#a10), [A11](#a11) |

The 12 A-items below specify owners, dependencies and observable closure tests
for these missing behaviors. Documentation corrections clarify the contracts;
they do not implement a missing connector, execute a real-model benchmark or
close a platform capability gap.

## Implementation progress

The new [product KB and source boundary](product-knowledge-and-evaluated-sources.md)
implements the first shared-context dependency slice. Its disposable acceptance
passed **28/28 existing mapped checks, 11 suites and 139 test/subtest results**.
The new tests are additional capability evidence; the original 28-item map is
not a coverage claim for all 12 AI-native gaps. See the
[exact source snapshot and checks](ai-native-sdlc-implementation-20260913.md).

| Gap | Implemented in this change | Remaining closure requirement |
|---|---|---|
| A01 | Strict versioned intent, exact BRS/context bindings, criterion IDs, protected packet digests, visible assumptions and clarification blockers | Agent-assisted compilation/clarification and complete intent-to-effect links |
| A02 | Current context and evaluator boundaries now available to a supervisor | Durable agent plan/action/evaluate/repair execution, cancellation and effect reconciliation |
| A03 | Existing local-only embedding/provider controls retained | Evaluated builder/evaluator packages, routing/budgets, regressions and rollback |
| A04 | Immutable product versions/relations, distinct observations, local semantic discovery with authoritative hydration, AGE projection and bounded PostgreSQL structural context | Automatic code-symbol bridges, richer lifecycle ontology, deeper context construction and source refresh/resume behavior |
| A05 | Server-executed versioned observation oracle and immutable outcomes; exact accepted work-packet bindings | Independent protected product-test execution, held-out agent evaluation and full completion authority |
| A06 | Incident Diagnosis read role, fixed source/field/purpose scopes, inherited field restrictions, owner attribution and existing delegated Development proposals | General role delegation, product/environment scopes, cumulative budgets and execution isolation |
| A07 | Operator-configured bounded HTTP source views with schema/time/offset/snapshot validation and idempotent committed retention | Native Kafka/lake/log/audit bridges and delivery/operational action adapters with effect reconciliation |
| A08 | Reproduce a known BRS violation, recognize a reconciled window and expose delayed lake uncertainty | Causal investigation, autonomous diagnosis, separately authorized remedy and verified recovery |
| A09 | Machine-readable intent readiness, blockers, context and evaluation evidence | Outcome/progress/exception interface connected to execution |
| A10 | Evaluated observations and results retained separately from pending generated lessons | Governed observation-to-product/test/runbook/agent-package improvement and controlled rollout |
| A11 | Exact original generation bytes, tool-input digests, source actor/role/owner receipts, immutable evaluation evidence | Complete external action/handoff audit and accepted-outcome metrics/cohorts |
| A12 | Compatible disposable gateway/worker/schema/policy/vector/graph acceptance | Retained live cutover and target identity/storage/retention/HA/recovery acceptance |

The specific capture-byte defect is fixed. Composite A-items remain open until
their full proof contracts pass. In particular, synthetic source reconciliation
is not an end-to-end diagnostic/remediation agent, and the HTTP source protocol
is not a native Kafka consumer. The next dependency slice is A03/A05/A02:
protected independent evaluation and a bounded local execution worker consuming
these accepted intent/context contracts.

## Evidence and interpretation

| Evidence | What it establishes | Limit |
|---|---|---|
| [MCP registration](../internal/mcpserver/server.go), [semantic tools](../internal/mcpserver/semantic.go), [workflow transitions](../internal/service/workflow.go), [task transitions](../internal/service/taskcheckpoint.go) | Implemented tool and state boundaries at the assessed commit | Source presence does not prove deployment or an external integration |
| [Work-packet verifier](../components/workpacket/verify.go), [controller plugin](../automation/openclaw-plugin/README.md), [automation plan](openclaw-agentic-automation-plan.md) | Bounded verification and controller foundations; explicit remaining automation proposals | The verifier does not supply OS/network isolation |
| [CI](../.github/workflows/ci.yaml), [coverage map](../tests/agent-ready-coverage.json), [September 12 receipt](documentation-validation-20260912.md) | Recorded 28/28 local requirements, 11 suites and 132 passing test/subtest results | Dated fixture-based platform acceptance, not a new run or a model/product benchmark |
| [Security](security.md), [enterprise deployment](enterprise-deployment.md), [backup runbook](manual-backup-restore-postgres-milvus-google-drive.md), [operating decisions](agent-ready-operating-decisions.md) | Implemented local controls and explicit production/adoption prerequisites | Target environment, organization-level settings and recovery objectives were not audited here |
| Authenticated local MCP discovery, September 13 at 11:01 UTC, reconfirmed at 12:48 UTC | 20 live tools versus 32 source registrations with local indexing enabled; 12 expected tools absent | Identifies an API surface mismatch; does not establish the running image's exact source revision or database migration level |
| Local knowledge and repository graph lookups during this assessment | The scoped SDLC knowledge query and this repository's depth-1 relationship query each returned zero results | These query results do not prove the entire knowledge base or repository catalog is empty |

The local discovery receipts are retained under
`.local/sdlc-documentation-20260913T110109Z/` as
`live-mcp-tools.json`, `mcp-tool-comparison.json` and
`repository-graph-result.json`. They contain no credential values.
The observed missing tool names are preserved in G01 so the finding remains
reviewable from the checked-in document.
The repeated discovery is retained as `live-tool-inventory.json` and
`current-tool-comparison.json` under
`.local/documentation-scope-20260913T121734Z/`.

Gap types:

- **Deployment:** implementation exists but the observed environment lacks it.
- **Integration:** the team needs an explicit connection to an external system.
- **Implementation:** a platform component or enforcement path remains incomplete.
- **Evidence:** a capability or outcome needs relevant measured validation.
- **Operating decision:** an accountable owner must specify a deployment or policy input.

For the A-register, **P0** means needed for the first bounded AI-native feature
and incident proofs. **P1** means needed to expand those proofs reliably or
operate the named deployment target. The minimal P0 scope can support one
repository, disposable source systems and a serial agent loop; it does not
require a fleet of agents or a production cluster.

## Primary AI-native gaps

The descriptions below retain the baseline findings and full acceptance
contracts. Use [implementation progress](#implementation-progress) for what the
new branch has delivered and what is still open.

| ID | Priority | Missing capability | Existing foundation | Proposed owner |
|---|---|---|---|---|
| [A01](#a01) | P0 | Versioned intent and executable specification | Request artifacts, intake and governed definitions | Product Owner + platform Development + QA |
| [A02](#a02) | P0 | Durable plan/action/evaluate execution supervisor | Workflow state, FIFO tasks and OpenClaw state mirror | Platform Development |
| [A03](#a03) | P0 minimum / P1 expansion | Evaluated agent packages and local model/capability routing | Agent examples, provider records and local pilot | Platform Development + QA |
| [A04](#a04) | P0 | Shared product KB, evaluated observations and task context | PostgreSQL, AGE/Milvus, freshness and approved knowledge | Platform Development + domain/data owner |
| [A05](#a05) | P0 | Independent product/agent evaluation and protected acceptance criteria | Work-packet validation and deterministic platform tests | QA + platform Development |
| [A06](#a06) | P0 | Accountable agent roles, fine-grained access, budgets and isolation | Cerbos, expiring task credentials, patch/check limits | Platform Development + Security |
| [A07](#a07) | P0 feature/incident slices / P1 breadth | MCP source ingestion and delivery/action adapters | KB/code MCP tools, fixed metrics and Git/CI/deployment commands | Development + Operations + data owner |
| [A08](#a08) | P0 incident proof / P1 live operations | KB-driven diagnosis, verification and recovery | Health, traces, backup/recovery tools | Operations + Development |
| [A09](#a09) | P0 minimum / P1 interface | Outcome-oriented human direction and exception handling | Role gates, API/CLI and controller status | Product Owner + platform Development |
| [A10](#a10) | P0 KB feedback / P1 agent improvements | Governed evidence retention, lessons and agent/config improvement | Pending KB capture, validation and publication | QA + platform Development + Product Owner |
| [A11](#a11) | P0 audit/evidence / P1 cohorts | Complete agent/source audit and outcome evaluation | Provenance, traces, fixed knowledge metrics and two-task pilot | QA + Operations |
| [A12](#a12) | P0 local compatibility / P1 production | Compatible runtime and target trust/recovery controls | Local deployment foundation and enterprise design | Operations + Security |

<a id="a01"></a>
### A01. Compile intent into an executable specification

The [intake schema](../contracts/workflow/v1/intake.schema.json) accepts a request,
classification and identifiers. It does not bind an entire increment's
criteria, evaluator versions, scope, assumptions and resource/decision policy.
An agent needs these records to determine what to do and when it is done.

**Deliver:** a versioned intent/specification contract, agent-assisted
clarification and criterion generation, and links through plan, patch, tests
and deployment. Bind product/feature/BRS identities and accepted business
meaning in the KB so operational evidence can be evaluated against the same
requirements. Agents may propose changes; accepted criteria change through
an attributed version transition that invalidates affected evidence.

**Prove:** ambiguous/conflicting intent causes a precise clarification;
unsupported assumptions remain visible; a mid-run requirement change cannot
reuse stale acceptance or silently weaken an evaluator. Crosswalk: G02.

<a id="a02"></a>
### A02. Implement the durable execution supervisor

The [controller](../automation/openclaw-plugin/src/controller.ts) mirrors and
transitions state; the [development workflow](../automation/workflows/development-local.lobster)
verifies a supplied patch. Build the missing runtime that invokes the local
agent, observes tools/tests, chooses the next bounded action and records progress.

**Deliver:** a serial plan/action/evaluate loop first, with persisted checkpoints,
attempt IDs, stop conditions, cancellation, repair and reconciliation after
interruption. Extend the OpenClaw/execution-worker boundary; keep typed state
and authority enforcement in the Go MCP service. Add dependency graphs,
parallel specialists and file ownership
only after a serial slice is evaluated. Preserve PostgreSQL authority.

**Prove:** one intent produces a tested patch without a person moving outputs
between steps. Process loss, context exhaustion, stale source, tool failure,
repeated callbacks and cancellation resume or terminate correctly. Repeated
external actions must be reconciled before retry. Dependencies: A01, A04–A06.
Crosswalk: G03/G04.

<a id="a03"></a>
### A03. Treat agent behavior as a versioned release

Example agent configuration and model provenance exist. The inspected
controller/work-packet contracts do not supply a general evaluated package
combining role instructions, tool schemas, client/model version, context rules,
budget defaults and regression tests.

**Deliver:** one tested builder package and a separately controlled evaluator
first. Pin versions; select routes using capability, data policy, measured
quality, local resource availability and budget. Limit GPU concurrency and
queue/defer when the required local model is unavailable. Maintenance has no
cloud fallback. Role specialization does not require a separate model per role.

**Prove:** each result identifies its complete agent configuration; unsupported
tools/models fail clearly; prompt/model/tool changes run regression evaluations
and can be rolled back. Local maintenance remains local under failure.
Dependencies: A05/A06/A11. Crosswalk: G04/G12.

<a id="a04"></a>
### A04. Extend the shared KB and construct current task context

Approved retrieval, code/repository relationships and source freshness are
implemented. The broader product/BRS/feature/deployment/observation/incident
model, evaluated source ingestion and a task-scoped context builder need
extensions. Structural and semantic knowledge must share authoritative
identities and eligibility rather than evolve into conflicting truth stores.

**Deliver:** the minimum entity/relation and source-validation contracts for one
feature and incident. Link accepted BRS meaning, code/release versions,
time-scoped observations and verified outcomes. Preserve source receipts,
classification, schema, time/offset/snapshot coverage, uncertainty and correction
history. Build provenance-bearing context within relevance/size limits;
separate temporary memory, run evidence, validated observations and approved
generalized knowledge. Recheck source and external state on resume. Retrieved
content cannot change instructions or authority.

**Prove:** structural and semantic queries answer known product/incident
questions against the same authorized current records. Reject malformed,
duplicate, stale, cross-project or poisoned input as appropriate; represent
partial observations and unproven causal links explicitly. Compaction preserves
constraints and evidence. A vector hit or summary cannot override PostgreSQL
eligibility. Dependencies: A01, A06/A07 source contracts and existing retrieval
controls. Crosswalk: G11/G13.

<a id="a05"></a>
### A05. Make independent evaluation the completion authority

The verifier and platform E2E are valuable. AI-native completion additionally
needs application-specific oracles and agent-behavior evaluations. Model
agreement or an agent-written success message cannot certify a product outcome.

**Deliver:** versioned application criteria, protected test/evaluator inputs,
independently controlled execution, negative/held-out cases and calibrated
qualitative rubrics. Evaluate result state and task trajectory separately.
Builders can propose tests but cannot silently edit accepted grading criteria.

**Prove:** a deliberately incorrect patch, weakened test, fabricated tool
result, unsafe action and incomplete criterion fail; a correct task succeeds
across recorded repeated trials. Human judgments remain explicit where
required. Dependencies: A01 and controlled test environments.
Crosswalk: G06/G12; [evaluation rationale](sdlc-guide.md#evaluate-the-product-and-the-agents).

<a id="a06"></a>
### A06. Enforce accountable roles, access and bounded autonomy

Cerbos, task credentials and
[work-packet limits](../components/workpacket/packet.go) cover important
boundaries. The packet bounds file/diff size and individual checks; it does not
define a complete per-intent model/token/spend/compute budget or agent-loop
termination policy. The verifier explicitly delegates OS/network isolation to
the environment.

**Deliver:** bind each role to workload identity, responsibilities, delegator,
task and accountable owner. Enforce project/source/dataset/field/environment,
purpose, time and action scope at tool reads, KB writes and operational effects.
Keep read, propose, validate, approve, execute and administer capabilities
distinct. Enforce credential lifetime/revocation, egress, wall time, model/tool
usage, retries and concurrency outside the model. Require write preconditions,
effect receipts, cancellation and an escalation path.

**Prove:** runaway plans, prompt-injected tool requests, scope escapes,
expired/revoked authority and budget exhaustion stop correctly. Test each
role's allowed work and another role's protected capability; a diagnostic read
role cannot deploy, alter audit data or reset consumer offsets. Independent
validation cannot be replaced by the builder assigning itself a new prompt
role. Permitted work
continues under standing authority without redundant approval prompts.
Existing human gates still apply. Dependencies: A01, A11 and the execution
environment. Crosswalk: G04/G07/G09.

One integration case needs explicit coverage: the standalone
[packet evaluator](../components/workpacket/policy.go) can represent confidential
cloud review with `cloud_approved_by`, while the
[governed atomic route](../internal/service/taskcheckpoint.go) permits cloud
review only for public/internal development. Packet fields are descriptive
inputs, not authenticated authority. The supervisor must honor the applicable
server route and policy intersection; a supplied approval string cannot bypass
the current local-only route or disclose prohibited data.

<a id="a07"></a>
### A07. Connect source evidence and delivery through MCP capabilities

Existing MCP tools serve governed knowledge/code/workflow operations; the
[fixed metric registry](../internal/contextregistry/registry.go) serves four
platform metrics. The assessed tool registrations do not provide general
production-log, Kafka, lake or external audit queries. Forge, CI and deployment
commands also need typed agent contracts and effect reconciliation.

**Deliver:** a source registry and MCP adapters for the chosen BRS/code,
log/metric, Kafka/event, lake and audit evidence contracts. Start with disposable
fixtures. Bind identity, permitted query/view, schema, window/offset/snapshot,
timeout, row/byte/rate limits and partial-result semantics. Validate and link
results through A04; never silently embed raw production payloads. Provide
separate forge/CI/artifact/staging action capabilities with idempotency,
preconditions, read-back and compensation. The
[source adapter requirements](ai-native-sdlc-expectations.md#source-adapter-requirements)
specify the expected controls.

**Prove:** collected evidence reconciles to known source events; missing metrics
are not treated as zero, late lake data is not mistaken for absent Kafka events,
and diagnosis leaves production consumer-group offsets untouched. Denied,
truncated and failed queries remain auditable. For delivery, create a linked
PR, reconcile exact-commit CI/artifact and verify staging; a timeout after a
successful write cannot duplicate the effect. Dependencies: A04/A06/A11;
delivery execution adds A01/A02/A05. Crosswalk: G02/G05/G07/G08/G13/G16.

<a id="a08"></a>
### A08. Diagnose and recover using live evidence and the shared KB

Platform health, trace and recovery tools exist. They do not yet constitute
the product incident loop requested here: collect live evidence, compare BRS
and deployed code, inspect incident history, test hypotheses and verify an
authorized remedy. Scheduled orchestration also remains planned.

**Deliver:** the [incident walkthrough](ai-native-sdlc-expectations.md#production-troubleshooting-walkthrough)
as an executable bounded local scenario. Link diagnostic reads to the incident,
business criteria and exact deployment; preserve alternatives and contradictory
evidence. Give remediation a separate action capability, verify business
recovery and feed eligible observations/pending lessons into the KB. Escalate
uncertainty, missing authority or exhausted limits. All troubleshooting and
maintenance inference stays local, with no cloud fallback.

**Prove:** a known injected fault produces supported findings and verified
business recovery; misleading similar incidents, missing evidence and a failed
remedy do not create false certainty or endless repairs. The trace links source
queries through the diagnosis/action to KB retention and proposed learning.
Measure recovery against stated objectives before live operation.
Dependencies: A02/A04/A06/A07/A10/A11/A12. Crosswalk: G03/G08/G10/G13/G16.

<a id="a09"></a>
### A09. Center the human experience on intent and exceptions

Current status and role gates provide a starting point. An AI-centric product
needs a coherent way to submit/steer intent, inspect progress/evidence/cost,
cancel work and resolve a specific decision. People should not have to copy
intermediate tool outputs between agents.

**Deliver:** a minimal control surface over the current API/controller first.
Show requested outcome, accepted scope, completed criteria, next action,
uncertainty, limits and blocked decisions. Bind each decision to its exact
version and acting authority; reuse standing task authorization.

**Prove:** an operator can start and steer a complete bounded increment, inspect
a failure, cancel it and resume permitted work with clear evidence. Missing
information yields a focused question; pending KB publication stays explicit.
Dependencies: A01/A02 and existing role policy. Crosswalk: G14.

<a id="a10"></a>
### A10. Improve the system through governed feedback

The existing pending/validated/approved knowledge path is a strong foundation.
Improving prompts, tools, context selection, evaluators or model configuration
requires a separate versioned change and evaluation lifecycle.

**Deliver:** turn recorded failures and reviewer corrections into proposed
regression cases, lessons and agent/config changes. Evaluate on held-out
scenarios, retain baseline comparisons and support controlled rollout/rollback.
Link operational observations to product requirements, regression tests and
runbook improvements. Distinguish source-validated observation retention from
generated diagnoses/summaries/generalized lessons: the latter remain pending
and cannot bypass explicit approval by being labeled as facts. This proposal
does not require online model-weight training.

**Prove:** a captured failure can produce a candidate improvement that passes
its target case without regressing the evaluation suite. Models cannot approve
their own knowledge, change their own authority, silently weaken evaluators or
self-deploy new configurations. Generated KB publication retains the explicit
user decision. Dependencies: A03/A05/A11. Crosswalk: G11/G12/G13.

<a id="a11"></a>
### A11. Audit the complete path and measure accepted outcomes

Existing traces, provider records and four fixed knowledge metrics do not
establish full source/agent/action audit coverage or measure the complete
AI-native delivery loop. The two-task real-model pilot and
fixture-based acceptance are different evidence from representative product work.

The baseline byte-preservation defect is now fixed: [`Service.Capture`](../internal/service/service.go)
uses whitespace trimming only to reject blank input and preserves the original
prompt/response bytes in artifacts and stored records. Regression evidence
covers boundary whitespace, CRLF and Unicode. `RecordReview` continues to
preserve supplied `raw_output` and `context_manifest`. This closes that specific
defect; complete source/action/handoff audit remains open.

**Deliver:** correlate intent/incident, workload/role, delegator/owner, source
query/version/window, policy decision, result digest, interpretation status,
validation, effects and KB publication. Include reads, denials, failures,
partial results, rejected hypotheses and human interventions. Protect sensitive
payloads and preserve durable attributed evidence. Record accepted-outcome
rate, false-success/unsafe-action counts, recovery/retry behavior, latency,
resource/cost use and intervention effort. Unknown measurements remain unknown.
Preserve original permitted generation bytes separately from any normalized
searchable content, with hashes binding the original output and disclosure
manifest to the run.

**Prove:** an authorized reviewer reconstructs a feature and an incident from
source read through action and KB write, including failures and denied access.
Required audit failure blocks protected effects; evidence survives restart.
Round-trip outputs with leading/trailing whitespace and line endings without
changing the original evidence bytes; verify any derived normalization separately.
Repeated runs on a representative versioned task set report failures and
variance. Offline trace inspection is possible; deterministic reproduction of
new model output is not assumed. Increase autonomy only from measured evidence.
Bootstrap the audit contract with A06; end-to-end measurement adds A02–A05/A07.
Crosswalk: G05/G09/G10/G11/G12/G15.

<a id="a12"></a>
### A12. Establish the compatible deployment and trust boundary

The previously observed 20-live/32-source tool mismatch remains a deployment
prerequisite to using current functionality. For broader operation, identity,
tenant isolation, egress, secrets, durable evidence and recovery must hold in
the actual execution/deployment environment.

**Deliver:** establish a compatible disposable local baseline for implementation
and its capability smoke tests, then perform the retained live cutover in G01
when its actual credentials and recovery prerequisites are available. Add the
target-specific production controls described in G07 and
G09–G11 when moving to that target.

**Prove:** intended source/schema/tool versions and relevant allow/deny cases;
for production, adversarial agent/tool tests, tenant isolation, credential/key
lifecycle, tamper detection, interruption and measured restoration.
Infrastructure health alone is insufficient. The disposable baseline uses its
test fixtures; retained live cutover additionally requires normal vault access,
the named owner and target decisions. Crosswalk: G01/G07/G09–G11/G16.

## Crosswalk to the earlier delivery findings

| Earlier finding | Primary AI-native capability |
|---|---|
| G01 live baseline | A12 |
| G02 requirement traceability | A01, A07 |
| G03 lifecycle orchestration | A02, A07, A08 |
| G04 general executor/classifier/disclosure | A02, A03, A06 |
| G05 forge and CI binding | A07, A11 |
| G06 product/nonfunctional acceptance | A05 |
| G07 secure release | A06, A07, A12 |
| G08 deployment and rollback | A07, A08 |
| G09 enterprise identity/isolation | A06, A11, A12 |
| G10 operations and recovery | A08, A11, A12 |
| G11 evidence lifecycle | A04, A10, A11, A12 |
| G12 model quality/adoption | A03, A05, A10, A11 |
| G13 context coverage | A04, A07, A08, A10 |
| G14 ownership and experience | A06, A09, A11 |
| G15 outcome/cost metrics | A11 |
| G16 retirement | A07, A08, A12 |

## Supporting delivery findings

The G-register retains the earlier evidence and delivery-oriented priorities
for comparison. The A-register above controls the AI-native implementation
order; in particular, the shared KB, source tools, role/access/audit contracts,
evaluation and a minimal human control surface belong in the first slice.

| ID | Priority / type | Affected stages | Gap | Proposed owner |
|---|---|---|---|---|
| [G01](#g01) | P0 / Deployment | 05–14 | Live gateway lacks current governance/semantic tools | Operations + platform Development |
| [G02](#g02) | P1 / Integration + implementation | 01–04, 08–12 | Requirement-to-test-to-release traceability | Product Owner + Development + QA |
| [G03](#g03) | P1 / Integration + implementation | 02, 06, 10–12 | Full delivery orchestration and dependency handoffs | Platform Development |
| [G04](#g04) | P1 / Implementation | 04–09, 14 | General supervised/local executor, classification and disclosure preparation | Platform Development + Security |
| [G05](#g05) | P1 / Integration | 08–12 | Forge/CI events and exact-revision enforcement | Development + QA |
| [G06](#g06) | P1 / Evidence + integration | 03, 09, 10 | Application-level and nonfunctional acceptance | QA + Product Owner |
| [G07](#g07) | P1 / Implementation + integration | 08, 09, 11 | Secure release and software supply-chain gates | Security + Development |
| [G08](#g08) | P1 / Integration | 11, 12 | Release publication, environment promotion and rollback | Operations + Development |
| [G09](#g09) | P1 for enterprise / Implementation + evidence | 05, 12, 13 | Target identity, isolation and availability | Platform Operations + Security |
| [G10](#g10) | P1 for production / Operating decision + evidence | 12–14 | SLOs, incident routing and measured recovery | Operations / service owner |
| [G11](#g11) | P1 for production / Implementation + operating decision | 13–15 | Durable production evidence lifecycle | Operations + data owner |
| [G12](#g12) | P1 before broader autonomy / Evidence | 07–10, 14 | Representative real-model/retrieval evaluation | QA + Product Owner |
| [G13](#g13) | P2 / Integration + evidence | 01, 03–05, 14 | Useful product context and repository coverage | Domain owner + Development |
| [G14](#g14) | P2 / Operating decision + integration | 02, 06, 10, 13, 14 | Named ownership and a usable cross-role work queue | Product Owner + Operations |
| [G15](#g15) | P2 / Integration + implementation | 02, 13, 14 | Delivery, product-outcome and cost measurements | Product Owner + Operations |
| [G16](#g16) | P2 / Integration + operating decision | 15 | Product retirement and verified closure | Product Owner + Operations |

<a id="g01"></a>
### G01. Bring the running service to the intended baseline

**Finding:** authenticated discovery returned 20 tools. The assessed source
registers 32 when indexing is enabled; the live service already exposes the
indexing tool. These 12 tools were absent:

```text
context_definition_decide
context_definition_search
context_registry_validate
knowledge_candidate_quality
knowledge_index_failures
knowledge_index_retry
knowledge_quality_reviews
knowledge_validation_record
platform_evidence_health
platform_metric_query
workflow_task_context_record
workflow_trace_get
```

This directly affects current validation, governed definitions/metrics, context
use, trace inspection and recovery instructions. It is consistent with the
existing L18 cutover deferral, but the current migration level was not queried.
Do not treat an old tool with the same name as proof of current semantics.

**Close it:** use the [prepared release procedure](agent-ready-release-20260910.md)
to identify the running images/schema, obtain normal vault access, rehearse the
upgrade and deploy compatible gateway, worker, admin and policies together.
Do not create replacement credentials to evade an access prerequisite.

**Acceptance:** record image/source and schema versions, required tool names and
schemas, preserved data/artifacts, authenticated allow/deny checks, validation
and trace smoke results, worker projection/restart results, and rollback
evidence. Count equality alone is insufficient. Dependencies: existing vault
access and the target deployment owner.

<a id="g02"></a>
### G02. Connect requirements to tests and delivery

**Finding:** the [intake contract](../contracts/workflow/v1/intake.schema.json)
and workflow metadata can retain a request and references. The inspected
contracts/service have no complete product requirement/version model or enforced
link from acceptance criterion through design, task, PR, test and release.

**Close it:** select the authoritative issue/requirements system and adopt the
[handoff record](sdlc-guide.md#keep-the-records-connected). Add a small versioned
link contract and completeness checks at release preparation; an adapter can
keep the issue tracker authoritative.

**Acceptance:** one feature and one defect can be traced both directions from
accepted criterion to deployed artifact. Changing a criterion or tested source
invalidates affected acceptance evidence, and an untested required criterion
blocks release recommendation. Owner: Product Owner, Development and QA.

<a id="g03"></a>
### G03. Connect the workflow to the complete delivery lifecycle

**Finding:** the [managed state machine](../internal/service/workflow.go)
reaches `promotion_pending` / `completed`; atomic tasks have validation and
knowledge-reuse/publication checkpoints. Those states do not record an
application release, target deployment or retirement. FIFO tasks do not
provide a multi-repository dependency scheduler. The controller integration is
implemented; the [webhook relay and scheduled orchestration](openclaw-agentic-automation-plan.md)
remain planned.

**Close it:** define explicit handoffs to the issue tracker and delivery system,
with artifact/environment identity, dependency status and callback contracts.
Keep product delivery and knowledge publication as separate outcomes.

**Acceptance:** a supervised feature flows from intake through staging
deployment; restart, duplicate/out-of-order callbacks, failed dependencies,
cancellation and a pending knowledge decision preserve correct delivery state.
Dependencies: G01, G02 and G05; delivery hooks coordinate with G08.

<a id="g04"></a>
### G04. Complete the bounded execution path

**Finding:** the [automation plan](openclaw-agentic-automation-plan.md) explicitly
leaves the automatic classifier, general isolated worktree runner and disclosure
packager planned. The [verifier](../components/workpacket/verify.go) executes
checks in a disposable clone and documents that OS/network isolation is the
deployment's responsibility. A controller adapter is insufficient to claim a
complete autonomous agent runner.

**Close it:** first implement a local supervised executor with exact source,
packet limits, expiring authority, environment isolation, bounded retries,
cancellation and retained outputs. Add optional disclosure preparation only
for policy-permitted development work; maintenance must retain a local-only
route without cloud credentials or fallback.

**Acceptance:** successful and failing tasks execute with exact provenance;
scope escapes, stale revisions, expired credentials, forbidden egress and
cancelled/repeated attempts are rejected. Any optional review sends only its
approved minimized manifest and cannot apply or approve a patch. Dependencies:
G01 and the existing work-packet/task-delegation contracts.

<a id="g05"></a>
### G05. Bind forge and CI events to the exact change

**Finding:** [repository CI](../.github/workflows/ci.yaml) exists, while the
automation plan labels the Git/CI webhook relay as planned. The assessed
workflow boundary has no implemented end-to-end PR creation/review/merge adapter
or automatic enforcement that the CI result matches the final merge revision.
Organization-level branch protection was not inspected.

**Close it:** integrate authenticated forge/CI events with stable delivery IDs,
explicit repository permissions, idempotency and exact commit/artifact digests.
Keep required checks and merge authority enforced by the forge.

**Acceptance:** opening/updating/merging a PR updates its workflow evidence;
stale CI, forged callbacks, duplicate events and a conflicting merge cannot
reuse an earlier passing receipt. Demonstrate with a real test repository.
Dependencies: G02 and project forge configuration.

<a id="g06"></a>
### G06. Prove application and nonfunctional acceptance

**Finding:** the [28-item coverage map](../tests/agent-ready-coverage.json)
tests platform governance, persistence, retrieval and related runtime behavior.
It does not provide arbitrary applications' user journeys, accessibility,
performance budgets, security acceptance or UAT. Model fixtures do not exercise
a model's ability to implement those applications.

**Close it:** choose one representative application and define its test matrix,
data, environments, criterion IDs and required manual observations. Connect the
results to G02 and preserve their exact revision and failure history.

**Acceptance:** representative happy/negative paths and the application's
agreed nonfunctional criteria run on the release candidate, with accountable
UAT and defect disposition. Missing/skipped mandatory tests fail that
application's gate. Dependencies: product requirements and target-like test data.

<a id="g07"></a>
### G07. Supply secure release gates

**Finding:** CI includes `npm audit --omit=dev` for the controller, policy and
contract tests, and container builds. The inspected workflow does not define a
complete cross-language vulnerability/secret scan, SBOM, artifact signing,
signature verification or release provenance pipeline. The
[security guide](security.md#production-requirements) lists production controls.
External organization-wide scanners were not audited.

**Close it:** inventory existing organizational controls, then add the missing
language/container scans, credential detection, dependency review, artifact
identity/provenance and verification gates for the chosen delivery target.

**Acceptance:** synthetic secret/vulnerable dependency fixtures and tampered or
untrusted artifacts are rejected; passing evidence binds the exact release
digest. Exceptions have an owner, scope and expiry. Dependencies: G05, G08 and
the project's threat model.

<a id="g08"></a>
### G08. Implement release and deployment handoffs

**Finding:** CI's container builds use `push: false`. The repository provides
local Compose, migration commands and a Kubernetes application base, but the
inspected CI has no release publication or environment-promotion job.

**Close it:** connect the chosen registry and delivery system. Define release
identity, configuration and schema compatibility, deployment authority,
staging checks, production decision, health verification and recovery.

**Acceptance:** build once, promote the identical verified digest through
environments, record deployment actor and post-deploy results, then rehearse
rollback with a data-compatible application version or tested restoration.
Dependencies: G02, G05, G06, G07 and target credentials/ownership.

<a id="g09"></a>
### G09. Validate the enterprise environment

**Finding:** local bearer principals, Cerbos, task credentials and project
checks exist. The [enterprise target](enterprise-deployment.md) still needs
identity federation/workload identity, ingress/TLS, managed secrets, tenant
network/storage controls and highly available dependencies. Local isolation
tests and rendered Kubernetes manifests do not certify that environment.

**Close it:** choose the actual identity, cluster, data and inference services,
then implement and test the stated integration contracts.

**Acceptance:** target identity lifecycle, denial behavior, cross-tenant
isolation, key/credential rotation and dependency/replica failover pass with
named owners and retained evidence. Dependencies: target infrastructure and
security policy; relates to E08 in the existing register.

<a id="g10"></a>
### G10. Define service objectives and rehearse operations

**Finding:** health tools, durable trace/export mechanisms and encrypted
backup/restore tooling exist. Product SLOs, alert routing, on-call ownership
and measured target RTO/RPO are not established by the reviewed local receipts.
Scheduled operations automation remains planned. A restore test using a fake
Drive client does not establish actual Drive delivery or production recovery.

**Close it:** define objectives and escalation, connect the collector/application
signals to the chosen monitoring system, schedule backups and exercise incident
and restore procedures with approved test data and real target dependencies.

**Acceptance:** a controlled failure triggers the responsible operator,
recovery meets the stated objectives, restored data/artifacts are checked, and
the incident links to the release and corrective task. Dependencies: G08, and
G09 for enterprise deployment; existing E06/L18 apply.

<a id="g11"></a>
### G11. Complete production evidence storage and retention

**Finding:** local content-addressed artifacts and retention review/hold alerts
exist. The [operating decisions](agent-ready-operating-decisions.md) and
[enterprise guide](enterprise-deployment.md) leave production storage,
archival/deletion and retention authority open. Current alerts never delete
evidence.

**Close it:** choose retention periods, hold/release authority, encryption/key
ownership, durable storage, export and authorized archival/deletion procedures.
Preserve source/projection withdrawal and audit semantics.

**Acceptance:** evidence survives a node/storage recovery, held records resist
deletion, authorized expiry is auditable, and records removed or withdrawn from
eligibility cannot reappear through a stale index. Dependencies: accountable
data/storage decisions; maps to E07.

<a id="g12"></a>
### G12. Measure real-model quality and adoption readiness

**Finding:** the retained pilot demonstrates two local tasks; the broad
functional suite uses deterministic model responses. The
[evaluation protocol](cost-routing-evaluation.md) and
[staged-autonomy criteria](agent-ready-operating-decisions.md#staged-autonomy)
describe future representative evaluation, not observed productivity gains.
Client/model MCP reliability also needs version-specific validation.

**Close it:** run an owned representative set with the actual local
model/client, repository types, failure cases and permitted routes. Record
quality, denied actions, retries, latency, resource/cost use and rollback.
Maintenance examples remain local throughout.

**Acceptance:** publish task selection, versions, hardware, complete outcomes
and limitations; only broaden a capability's autonomy after its accountable
cohort decision. No cost/success threshold is invented by this assessment.
Dependencies: G04, G06 and measured observation windows; maps to E02.

<a id="g13"></a>
### G13. Establish useful product context coverage

**Finding:** governed definitions, approved lessons and supported-language code
graphs exist. Relationship discovery is explicit; source formats and analyzers
have stated limits. This assessment's two scoped lookups returned no matches.
That establishes missing context for these queries, not global data absence.

**Close it:** inventory each application's repositories, domain definitions,
contracts and approved reusable procedures. Assign source owners, measure
representative question coverage, and choose integrations for requirement/design
documents only when the product needs them.

**Acceptance:** known questions retrieve the correct current source and known
dependencies; stale, unsupported or missing sources are reported explicitly.
New captures remain pending until their own validation and approval.
Dependencies: G01, source-owner access and the source freshness contracts.

<a id="g14"></a>
### G14. Make ownership and handoffs usable

**Finding:** four-role workflows, a default solo operator and OpenClaw
coordination exist. The broader ownership register remains unassigned for a
target deployment. The reviewed application surface is API/CLI/controller
oriented; a complete product backlog, reviewer inbox and escalation experience
would need an external integration or a further interface.

**Close it:** name owners and response expectations, then expose task state,
pending decisions, evidence and escalation through the team's existing work
system before considering a separate UI.

**Acceptance:** a user in each role can find assigned work, inspect the exact
version/evidence and complete or escalate a handoff; unauthorized users cannot.
An overdue decision remains visible without granting automatic approval.
Dependencies: G02/G03; maps to E06.

<a id="g15"></a>
### G15. Connect lifecycle outcomes and costs to metrics

**Finding:** the [fixed query registry](../internal/contextregistry/registry.go)
implements `pending_index_age_seconds`, `candidate_validation_rate`,
`candidate_approval_rate` and `validated_reuse_rate`. These describe knowledge
operations. They do not establish deployment frequency, change lead time,
production change failures, recovery time, user value or complete inference cost.

**Close it:** define the required product/delivery measurements, authoritative
event sources, denominator, time window and ownership. Link deployment and
incident events before adding reviewed metric queries; retain unknown costs as
unknown.

**Acceptance:** known fixture histories produce reproducible aggregates and
real runs reconcile to source events; definitions and executable meaning have
versioned validation. Dependencies: G02, G05, G08 and G10; L12 also covers
publication/querying of existing definitions in the retained runtime.

<a id="g16"></a>
### G16. Define and verify retirement

**Finding:** the assessed workflow has no product retirement state or
cross-system decommissioning integration. Index withdrawal and knowledge
retention are only part of retiring a running product.

**Close it:** agree dependency migration, user communication, access
revocation, data disposition, infrastructure removal and evidence recovery
with the Product Owner, Operations and data owners.

**Acceptance:** an example retirement leaves no active dependent clients,
orphaned jobs or credentials; retained evidence remains accessible to its
authorized owner, and required deletion/hold decisions are recorded.
Dependencies: G02, G10, G11 and the product's data obligations.

## How this relates to the existing checklist

The [agent-ready register](agent-ready-gap-checklist.md) keeps its original
22/28 broader-rollout count and the separately accepted 28/28 local functional
scope. This assessment expands the question to product delivery across the
whole SDLC. The 16 supporting findings neither replace those criteria nor change
their counts.
The 12 primary A-items reframe and extend that supporting evidence around the
shared KB, MCP source tools and accountable agent execution.

| Existing deferred item | Related SDLC work |
|---|---|
| L12: retained runtime definitions, metrics and complete report | G01, G13, G15 |
| L18: compatible live release and collector cutover | G01, G10 |
| E02: representative adoption cohorts and stage decisions | G12 |
| E06: named ownership and escalation | G10, G14 |
| E07: production retention/storage and archival/deletion | G11, G16 |
| E08: target identity, isolation, availability and recovery | G09, G10 |

## Recommended closure order

| Sequence | Deliverable | Gaps addressed | Proof before moving on |
|---|---|---|---|
| 1. Establish a compatible local baseline | Current tools, authenticated operator/workload identities, named owners and a bounded product/source inventory | A12; initial A06/A09 | Record source/schema/tools and demonstrate existing allow/deny and knowledge publication gates |
| 2. Build the shared KB and governance slice | One product/BRS/feature/code/release model, structural/semantic context, source/ingestion contracts, agent role policies and correlated audit | A01/A04/A06/A07/A11 | Known facts resolve consistently; source fixtures validate; cross-role/project requests fail; a query-to-KB audit can be reconstructed |
| 3. Establish executable criteria and agent packages | Versioned specification, a local worker, independently controlled evaluator and a small regression set | A01/A03/A05; initial A09/A11 | Correct and deliberately incorrect outcomes are distinguished; versions, uncertainty, limits and responsible owners are visible |
| 4. Connect durable execution | Bounded plan/action/evaluate loop, role-scoped calls, checkpoints, cancellation and external-effect reconciliation | A02 with A03–A07/A09/A11 | One task completes without copied handoffs; interruption, denied actions, repeated callbacks and exhausted budgets produce accurate state |
| 5. Prove a feature and an incident end to end | Agent-delivered staging increment and KB-driven diagnosis/remediation with evaluated feedback | A07/A08/A10 plus the integrated P0 slice | Both proof contracts below pass, including failure/uncertainty cases, accountable decisions and actual KB read-back after any approved publication |
| 6. Expand and operate the chosen target | More source adapters and roles, governed agent/config rollout, representative model evaluation, production identity/storage/retention/recovery | P1 expansion of A03/A07–A12 | Measured quality/cost/recovery, target-specific policy tests and named operating decisions justify expanded autonomy |
| 7. Prove lifecycle closure | Retirement rehearsal, dependency migration, revocation and data/evidence disposition | A07/A08/A12; G16 | Verify removal and continued authorized recovery of required evidence |

These are incremental delivery slices. Establish minimal contracts together
where components depend on each other, then deepen their evidence; the register
is not a requirement to finish every subcomponent before testing an increment.
Dates and estimates require the chosen application's scope and target inputs.
Use disposable local services for implementation acceptance. Retained live
cutover prerequisites do not prevent independent source, contract or test work.

### First feature proof

Use the [CSV export example](sdlc-guide.md#worked-example-add-a-csv-export-to-an-application)
or another bounded feature in one disposable application. Seed accepted BRS,
feature and code context, protected criteria and an applicable approved lesson.
An identified local builder must retrieve both KB dimensions, plan/implement,
repair from independent test results, reconcile a linked PR/CI/artifact, and
verify the staging behavior. Required human decisions remain attributed to
their exact version. Capture source reads, allowed/denied effects, test results,
resource use and observed outcome; leave its new generated lesson pending.

Inject stale context, a bad patch, a tool denial and an interrupted action.
Completion must depend on observed criteria and effects, with no duplicate
write, fabricated success, silent criterion change or unapproved KB publication.
Record real local-model trials separately from deterministic contract tests.

### First incident proof

Use the [production troubleshooting scenario](ai-native-sdlc-expectations.md#production-troubleshooting-walkthrough)
with synthetic log/metric, Kafka/event, lake and audit data in disposable
sources. Link them to known BRS/code/deployment/history records and inject a
known failure. The diagnostic role must collect bounded evidence, validate
coverage and joins, compare expected behavior and competing explanations, and
produce a reproducible supported finding. A separately authorized remediation
role applies the permitted remedy and verifies both technical and business
recovery. Retain eligible observations and pending generated lessons with their
distinct states and complete query-to-action-to-KB lineage.

Include delayed lake ingestion, incomplete logs, misleading similar incidents,
invalid schemas, denied fields, expired authority and a failed remedy. The
system must expose uncertainty, stay within local inference and query/action
limits, preserve consumer-group offsets during diagnosis, and escalate when
required. If an exact generated candidate is subsequently approved, verify
projection read-back and applicable reuse by a later design/build/QA task.
Approval remains a separate decision and is not implied by the proof request.

The [expectations proof checklist](ai-native-sdlc-expectations.md#first-end-to-end-proof)
adds the detailed role, source and audit assertions. These are proposed tests
for new capabilities. Documentation validation and the existing 28-item
functional result do not close these gaps or claim a production diagnosis.
