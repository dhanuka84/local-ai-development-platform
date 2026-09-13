# Documentation scope review and implementation alignment

Reviewed September 13, 2026 against `main` revision
`9fba87f23f0e733a6bba0528b505f647bbac5aef` and the uncommitted documentation
snapshot recorded with this review. This is a **pre-implementation receipt**.
Later code changes and their current boundaries are tracked in the
[implementation progress](sdlc-gap-assessment.md#implementation-progress) and
[product KB guide](product-knowledge-and-evaluated-sources.md); the observations
and validation counts below retain their original time boundary.

The implementation is **partially aligned** with the agreed AI-native SDLC
scope. It has a governed KB, code/repository graphs, workflow records,
authorization, local verification and knowledge-publication controls. It does
not yet provide the complete shared product/operational knowledge model,
source adapters, accountable agent execution and evaluated delivery/incident
loop required by that scope.

The [expectation-by-expectation assessment](sdlc-gap-assessment.md#implementation-alignment-with-each-expectation)
records partial support for nine expectations and no complete implementation
of the KB-driven incident-resolution expectation. These are capability
judgments, not a percentage of implementation completion. The
[12 primary gaps](sdlc-gap-assessment.md#primary-ai-native-gaps) have source
evidence, proposed owners, dependencies and closure criteria; the earlier
16 delivery findings remain cross-referenced.

## Review boundary and method

The review covered all **55 Markdown documents present at intake** and all
**seven repository Mermaid diagrams**, including their intended current or
target status. This receipt is the 56th Markdown document. The inventory below
includes unchanged documents so coverage can be checked without inferring it
from a Git diff.

1. Read repository guidance and search the local MCP KB before substantial
   assessment. The scoped search returned zero candidates; this says nothing
   about unrelated KB content.
2. Use the [10 expectations](ai-native-sdlc-expectations.md#expectations-and-related-functionality)
   and [15-stage lifecycle](sdlc-guide.md) as the product scope. Compare current
   claims with MCP registrations, domain/source contracts, graph projections,
   workflow/controller code, validation, access policies, audit records and CI.
3. Review current guides for scope conflicts and actionable examples. Review
   ADRs for retained architecture boundaries. Keep dated pilots, receipts,
   design proposals and the publication example identifiable as history;
   do not rewrite old results as new acceptance evidence.
4. Correct documentation discrepancies, check local navigation and examples,
   render changed diagrams, and run the required repository checks. This
   review does not execute illustrative backup/restore, publication,
   production-query or deployment commands.
5. Retain file hashes, check outputs, sanitized MCP discovery and the exact
   authored documentation output as evidence. Generated KB capture stays
   pending and does not approve any platform capability or lesson.

The [expectations](ai-native-sdlc-expectations.md) define the target. The
[implementation assessment](sdlc-gap-assessment.md) describes the inspected
code. Current runbooks describe available operations; dated evidence retains
its original revision and test boundary. A technical document can be aligned
within its subsystem without claiming to implement every SDLC stage.

## Documentation discrepancies corrected

| Finding | Correction | Implementation consequence |
|---|---|---|
| D01 — Scope described mainly code generation and delivery handoffs | Link the shared structural/semantic product KB and the full lifecycle across entry points, setup, architecture and operations guides | Product/BRS/feature/release/incident relationships and context construction remain [A01](sdlc-gap-assessment.md#a01)/[A04](sdlc-gap-assessment.md#a04) |
| D02 — Human role names could be mistaken for complete agent responsibilities | Distinguish four operator roles from the target agent responsibilities, workload/delegator identity and accountable owner | General role-capability delegation and source-level policy remain [A06](sdlc-gap-assessment.md#a06) |
| D03 — QA review and publication examples omitted required decision evidence | Explain that `review_record` does not create the required publication validation report; only the actual local Ollama path may revise a candidate; include version, validation ID, idempotency key and reason in approval examples | Existing generated-knowledge approval gate stays mandatory; documentation grants no approval |
| D04 — Controller and pipelines could be read as a general model executor | State that the controller mirrors governed state and example pipelines verify a supplied patch; model execution belongs to the configured client/worker | Durable execution and versioned agent packages remain [A02](sdlc-gap-assessment.md#a02)/[A03](sdlc-gap-assessment.md#a03) |
| D05 — Platform operations could be mistaken for production evidence ingestion | Separate platform health/reindex/backup tools from proposed log, metric, Kafka, lake and external audit adapters and business-recovery verification | Evaluated ingestion and KB-driven diagnosis remain [A04](sdlc-gap-assessment.md#a04)/[A07](sdlc-gap-assessment.md#a07)/[A08](sdlc-gap-assessment.md#a08) |
| D06 — Existing traces and artifact hashes suggested complete audit | Explain missing external-read/action, role/owner, source-window and partial-result coverage | Complete audit and measured outcomes remain [A11](sdlc-gap-assessment.md#a11) |
| D07 — A proposed benchmark sounded already measured | Mark the 30-task set as proposed; preserve the narrower recorded pilot and fixture evidence | Independent product/agent evaluation and representative cohorts remain [A05](sdlc-gap-assessment.md#a05)/[A11](sdlc-gap-assessment.md#a11) |
| D08 — Old rollout prerequisites sounded like freshly inspected facts | Attribute the old migration/vault observations to their dated records; require current capability and compatibility checks | The observed live/source tool mismatch remains [A12](sdlc-gap-assessment.md#a12); current image revision and migration level were not established |
| D09 — Shell placeholders made current examples syntactically invalid | Replace redirection-like placeholders in executable fences with named example values and explain exact candidate/version inputs | Examples are syntax-checked without executing publication or destructive operations |
| D10 — Artifact wording promised original generation bytes | Document `Service.Capture` whitespace normalization separately from exact raw-review/context-manifest storage; update the local runtime and capture diagrams | Original generation-byte preservation remains a specific [A11](sdlc-gap-assessment.md#a11) implementation gap |
| D11 — Confidential review rules differed between documented layers | Explain that standalone packet `cloud_approved_by` does not override the governed atomic route, which keeps confidential work local | Unified effective authority and adapter allow/deny tests remain [A06](sdlc-gap-assessment.md#a06) |
| D12 — Cancellation wording implied reversal of external effects | Require idempotency, effect reconciliation and separately authorized compensation | Durable execution and action adapters remain [A02](sdlc-gap-assessment.md#a02)/[A07](sdlc-gap-assessment.md#a07) |

These are documentation corrections. None closes the corresponding platform
gap merely by describing the required behavior.

## File-by-file scope disposition

“Retained” means reviewed and compatible within the document's stated purpose;
it does not mean its examples or historical claims were newly executed.

| Document | Purpose | Scope disposition |
|---|---|---|
| [AGENTS.md](../AGENTS.md) | Repository instructions | Retained: scoped authority, KB use, pending capture and local verification fit the target |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | Contribution workflow | Updated: require scope mapping, source evidence, role/access and audit considerations |
| [README.md](../README.md) | Product entry and local usage | Updated: target versus foundation, complete approval examples, placeholders and capture limitation |
| [SECURITY.md](../SECURITY.md) | Vulnerability reporting | Retained: reporting policy; product access controls are documented separately |
| [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md) | Licensing notices | Retained: licensing scope does not assert SDLC capability |
| [OpenClaw plugin](../automation/openclaw-plugin/README.md) | Controller integration | Updated: state mirror and supplied worker boundary; broader agent runtime remains open |
| [Code graph](../components/codegraph/README.md) | Compiler-aware code analysis | Updated: code graph is one structural input, not the full product/operational model |
| [Kubernetes base](../deploy/kubernetes/README.md) | Application deployment scaffold | Retained: requires externally supplied data/identity/storage and target acceptance |
| [Source-verification overlay](../deploy/kubernetes/overlays/source-verification/README.md) | Optional code freshness checks | Retained: bounded source verification and external repository access, not generic ingestion |
| [Documentation index](README.md) | Navigation and evidence precedence | Updated: expectations, lifecycle, implementation gaps and this review are discoverable |
| [ADR-0001](adr/0001-go-for-the-mcp-data-plane.md) | Go MCP boundary | Retained: deterministic data/control boundary remains compatible |
| [ADR-0002](adr/0002-postgresql-and-milvus.md) | Canonical and semantic stores | Retained: PostgreSQL authority and candidate hydration remain required |
| [ADR-0003](adr/0003-reviewed-knowledge-promotion.md) | Knowledge publication | Retained: validation and accountable exact-version decision; no self-approval |
| [ADR-0004](adr/0004-repository-graph.md) | Repository relationships | Retained: existing structural foundation, not a complete lifecycle ontology |
| [ADR-0005](adr/0005-code-graph-analyzer.md) | Code graph projection | Retained: source/revision-bound facts remain one input to shared context |
| [ADR-0006](adr/0006-bounded-local-execution-and-cloud-review.md) | Execution/review separation | Clarified: worker execution boundary does not establish a durable supervisor |
| [ADR-0007](adr/0007-openclaw-managed-agentic-workflows.md) | OpenClaw workflow integration | Clarified: target agent roles, knowledge eligibility and current confidential routing |
| [ADR-0008](adr/0008-cerbos-contextual-authorization.md) | Authorization architecture | Retained: trusted actor/resource boundary; new source/agent policy coverage is still required |
| [ADR-0009](adr/0009-apache-age-graphrag.md) | AGE and GraphRAG | Retained: graph projection and semantic discovery remain complementary views |
| [ADR-0010](adr/0010-rag-first-atomic-task-checkpoints.md) | Atomic task control | Retained: task state and evidence gates are foundations, not the complete SDLC executor |
| [ADR-0011](adr/0011-scoped-autonomy-and-functional-acceptance.md) | Scoped autonomy and acceptance | Retained: standing task authority does not approve generated KB entries |
| [ADR index](adr/README.md) | Architecture navigation | Updated: current product scope and open implementation boundary |
| [Data operations](agent-ready-data-operations.md) | Indexing, validation and recovery | Updated: dated runtime observations and the missing general ingestion contract |
| [Data plan](agent-ready-data-plan.md) | Dated agent-ready plan and results | Added current-scope pointer; retained historical plan/results and command context |
| [End-to-end acceptance map](agent-ready-e2e.md) | Existing functional test boundary | Updated: existing 28-item coverage versus the new feature and incident proofs |
| [Agent-ready checklist](agent-ready-gap-checklist.md) | Existing delivery register | Linked expanded scope; retained recorded 22/28 rollout and 28/28 functional boundaries |
| [Operating decisions](agent-ready-operating-decisions.md) | Product/operating assumptions | Updated: broader agent/operational KB expectations are implementation requirements, not authorization |
| [September 6 pilot](agent-ready-pilot-20260906.md) | Dated evidence | Retained as history; does not prove current broad product scope |
| [September 10 pilot](agent-ready-pilot-20260910.md) | Dated evidence | Retained with its measured environment and limitations |
| [September 10 release](agent-ready-release-20260910.md) | Dated handoff | Retained; its prerequisites are not new observations of the current runtime |
| [September 12 validation](agent-ready-validation-20260912.md) | Dated acceptance evidence | Retained exact fixture-based acceptance and deferred target requirements |
| [AI-native expectations](ai-native-sdlc-expectations.md) | Product scope and acceptance | Reviewed all ten expectations; clarified logical scope diagram versus worker execution boundary |
| [Flowable publication example](blog/local-hybrid-flowable-bpmn-designer.md) | Historical worked example | Retained as an example; current instructions take precedence for live operation |
| [Cost/routing evaluation](cost-routing-evaluation.md) | Evaluation method and evidence | Updated: product/agent evaluation gaps and proposed versus measured benchmark |
| [Developer guide](developer-guide.md) | Current development guide | Updated: product scope, review navigation and generation-byte limitation |
| [Diagram guide](diagrams/README.md) | View semantics and rendering | Updated: target versus implemented views, evidence wording and review receipt |
| [Original diagram prompt](diagrams/hybrid-ai-review-learning-explainer.prompt.md) | Historical visual provenance | Retained: superseded raster prompt, not current diagram specification |
| [September 12 docs receipt](documentation-validation-20260912.md) | Dated documentation evidence | Retained exact prior source and check results |
| [Enterprise deployment](enterprise-deployment.md) | Target deployment acceptance | Updated: deploying the foundation does not implement missing AI-native capabilities |
| [Glossary](glossary.md) | Shared terminology | Updated: KB views, BRS, observations, hypotheses, roles, owner and audit |
| [Technology stack](hybrid-ai-platform-tech-stack.md) | Inspected implementation choices | Updated: target component boundary and normalized generation artifacts |
| [Original hybrid architecture](hybrid-openclaw-ollama-kimi-architecture.md) | Historical design proposal | Retained as design history; current guides and ADRs define implemented behavior |
| [Routing verification](hybrid-routing-verification.md) | Dated verification record | Retained measured runs; not a fresh representative model evaluation |
| [Implementation guide](implementation-guide.md) | Current code and contracts | Updated: KB scope, worker boundary, effective review authority and capture normalization |
| [Setup and indexing](local-setup-and-indexing.md) | Local code/repository indexing | Updated: broader BRS/operational source ingestion remains separate work |
| [Backup/restore](manual-backup-restore-postgres-milvus-google-drive.md) | Platform data recovery | Updated: platform bundle does not recover every external evidence source |
| [Agentic automation plan](openclaw-agentic-automation-plan.md) | Existing foundations and next work | Updated: durable execution, policy, audit, agent evaluation and current closure order |
| [Operations](operations.md) | Local platform runbook | Updated: platform versus product operations, controller boundary and complete decision examples |
| [Remote-review learning](remote-review-learning.md) | Review evidence and knowledge loop | Updated: review-specific scope, source-adapter limits and raw generation/review distinction |
| [Role workflows](role-workflows.md) | Human operating procedures | Updated: agent accountability, QA/report distinction and required decision arguments |
| [SDLC gap assessment](sdlc-gap-assessment.md) | Implementation assessment | Added ten-row alignment matrix and concrete audit/authority findings; retained 12 primary and 16 supporting gaps |
| [SDLC lifecycle guide](sdlc-guide.md) | Target usage across 15 stages | Reviewed KB inputs, agent responsibilities, evidence, decisions and implementation limits |
| [Security controls](security.md) | Current and required protections | Updated: missing source/role/action access and audit coverage under the expanded scope |
| [Task delegation](task-delegation.md) | Current task credentials | Updated: Development-only task role versus target logical agent responsibilities |
| [Cerbos policies](../policies/cerbos/README.md) | Authorization implementation | Updated: new source/environment/purpose scopes and agent attribution need policy fixtures |
| [This review](documentation-scope-validation-20260913.md) | Review receipt | Added: complete inventory, corrected claims, evidence boundary and next acceptance work |

## Diagram disposition

| Mermaid view | Scope and review result |
|---|---|
| [AI-native SDLC scope](diagrams/ai-native-sdlc-scope.mmd) | Target: shared KB, all lifecycle stages, MCP source/action boundary, evaluated ingestion and role/access/audit controls |
| [Local architecture](diagrams/hybrid-ai-local-architecture.mmd) | Implemented services: changed artifact label to captured bytes; the guide explains capture normalization |
| [Review and learning loop](diagrams/hybrid-ai-review-learning-loop.mmd) | Current task routes plus labeled conditional review; exact review evidence and publication remain separate |
| [Knowledge publication](diagrams/hybrid-ai-review-learning-explainer.mmd) | Generated entries versus governed definitions; changed capture label to retained evidence |
| [OpenClaw workflow](diagrams/openclaw-agentic-automation-workflow.mmd) | Controller and authority boundary; supplied worker responsibility is explained in the current guides |
| [Enterprise architecture](diagrams/hybrid-ai-enterprise-architecture.mmd) | Reference target; no claim of an installed or accepted enterprise platform |
| [Local-to-enterprise progression](diagrams/hybrid-ai-local-to-enterprise-evolution.mmd) | Acceptance boundaries; replaced stale vault-unlock assertion with current credentials/runtime verification |

## Verification and retained evidence

| Check | Observed result | Limit |
|---|---|---|
| `make check` | Passed: Go formatting, vet and race-enabled package tests; shell/Python syntax and 14 Python tests | Go test results were cached; this is not a new full integration campaign |
| `make diagrams` | Passed: all seven Mermaid sources rendered to SVG/PNG with the pinned local renderer | The three changed existing views were visually inspected for readable, unclipped labels and correct boundaries; the scope view was also inspected when authored |
| `make docs-check` | Passed: 56 Markdown files, local links/anchors and navigation, seven source/export pairs with matching hashes and dimensions | External site availability and product behavior are outside this check |
| Current executable examples | Passed: 128 shell blocks with `bash -n`, seven JSON blocks and four embedded MCP JSON arguments | No illustrative publication, backup/restore or deployment operation was executed; 35 shell and one JSON historical blocks were excluded |
| Example/source consistency | Passed: 245 Make invocations reference current targets, four MCP tool references exist in source, all seven Make knowledge-decision examples include required arguments | This checks documented names and arguments, not deployed schemas or live effects |
| Scope/navigation coverage | Passed: all 55 intake documents plus this receipt are inventoried; ten expectations, 15 stage anchors, ten alignment rows, 12 primary and 16 supporting gap anchors | Presence and traceability do not establish capability completion |
| Authenticated local MCP discovery at 12:48 UTC | Reconfirmed 20 live tools versus 32 source registrations; 12 missing tools listed in [G01](sdlc-gap-assessment.md#g01) | Current image revision and database migration level were not inspected |
| `git diff --check` | Passed | Checks whitespace in tracked changes |

The local evidence directory is
`.local/documentation-scope-20260913T121734Z/`. It contains the initial inventory,
scoped KB query, example checks, final documentation/diagram hashes, exact
repository check logs and sanitized runtime tool comparison. Evidence records
must identify their actual command results and source snapshot.

The September 12 **28/28 requirements, 11 suites, 132 passing test/subtest
results** remain dated platform acceptance. This review does not rerun that
full integration campaign, validate a production environment, run a
representative model benchmark, or prove the new feature/incident loop.
Documentation consistency and passing unit checks cannot substitute for those
implementation outcomes.

## Next implementation acceptance

Follow the [closure order](sdlc-gap-assessment.md#recommended-closure-order):
establish a compatible disposable baseline, then one product/source slice with
shared KB contracts, accountable roles and correlated audit. Add executable
criteria, a local worker and an independently controlled evaluator before
connecting the durable execution loop. Prove both the
[feature path](sdlc-gap-assessment.md#first-feature-proof) and the
[incident path](sdlc-gap-assessment.md#first-incident-proof), including denied
access, partial data, interruption, business-outcome verification and pending
generated knowledge. Live rollout and target enterprise acceptance remain
separate evidence requirements.
