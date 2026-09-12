# Project documentation

Updated September 12, 2026. Start with the [developer guide](developer-guide.md)
for what the platform does, why it is built this way, and how to change it.
The [implementation guide](implementation-guide.md) describes the code and
contracts; the runbooks describe operating an installed stack.

## Current status

Local functional acceptance is complete: **28/28 mapped requirements**, across
**11 suites and 132 passing test/subtest results**, with no failures or skips.
These are the recorded results of the September 12 run, not a claim that every
later checkout has been tested. See the exact revision, source snapshot and
evidence in the [documentation refresh receipt](documentation-validation-20260912.md).
The [earlier acceptance receipt](agent-ready-validation-20260912.md#functional-acceptance-continuation)
retains its own source snapshot and results.

The suite runs actual application binaries with disposable PostgreSQL, AGE,
Milvus, Cerbos and an OpenTelemetry collector. Ollama responses are deterministic
fixtures; this proves application behavior, not model quality or production
readiness. `make agent-ready-functional` reproduces that acceptance boundary.

Six broader requirements remain deferred: retained live-pilot definition
publication, live cutover, representative adoption cohorts, deployment ownership,
production retention/storage, and target enterprise identity/HA/recovery.
The [checklist](agent-ready-gap-checklist.md) preserves its original **22/28**
rollout count alongside the completed functional scope. A deferral never turns
a failed functional test into a pass.

Assigned local work proceeds under the operator's existing authority. Validated
domain/capability/metric definitions may be published under standing task
authorization. Generated KB entries remain pending until the user explicitly
approves their exact version. See [ADR-0011](adr/0011-scoped-autonomy-and-functional-acceptance.md).

## Start and develop

| Read | Use it for |
|---|---|
| [Project overview](../README.md) | Purpose, quick start and public tool examples |
| [Developer guide](developer-guide.md) | What, why, setup, first change, tests and troubleshooting |
| [Implementation guide](implementation-guide.md) | Packages, state transitions, data ownership and configuration |
| [Current technology stack](hybrid-ai-platform-tech-stack.md) | Checked-in choices and their rationale |
| [Local setup and indexing](local-setup-and-indexing.md) | Workstation setup and multi-repository ingestion |
| [Contributing](../CONTRIBUTING.md) | Change and review requirements |
| [Repository agent guidance](../AGENTS.md) | Autonomous work and publication boundaries |
| [Glossary](glossary.md) | Plain-language definitions |

## Operate and integrate

| Read | Use it for |
|---|---|
| [Operations](operations.md) | Credentials, startup, commands, recovery and autonomy |
| [Role workflows](role-workflows.md) | Development, QA, Product Owner and Operations responsibilities |
| [Knowledge operations](agent-ready-data-operations.md) | Validation, publication, freshness, metrics, traces and the local pilot |
| [Task delegation](task-delegation.md) | Expiring task credentials and revocation |
| [Remote review and local learning](remote-review-learning.md) | Optional cloud lane, exact evidence and pending KB entries |
| [OpenClaw integration plan](openclaw-agentic-automation-plan.md) | Implemented controller contracts and remaining automation proposals |
| [Controller plugin](../automation/openclaw-plugin/README.md) | Build and integrate the TypeScript adapter |
| [Code graph component](../components/codegraph/README.md) | Deterministic language analysis and licensing boundary |
| [Cerbos policies](../policies/cerbos/README.md) | Trusted authorization context and policy tests |
| [Backup and restore](manual-backup-restore-postgres-milvus-google-drive.md) | Encrypted backup, download, recovery and key handling |
| [Security model](security.md) and [reporting policy](../SECURITY.md) | Implemented controls, deployment requirements and vulnerability reporting |
| [Enterprise deployment](enterprise-deployment.md) | Reference target and outstanding acceptance requirements |
| [Kubernetes base](../deploy/kubernetes/README.md) and [source-verification overlay](../deploy/kubernetes/overlays/source-verification/README.md) | Application manifests, read-only source snapshots and target-cluster prerequisites |

## Validate and plan

| Read | Use it for |
|---|---|
| [E2E guide](agent-ready-e2e.md) and [coverage map](../tests/agent-ready-coverage.json) | Reproduce acceptance and map every checklist item to executed tests |
| [Gap checklist](agent-ready-gap-checklist.md) | Functional completion and deferred rollout requirements |
| [Operating decisions](agent-ready-operating-decisions.md) | Adoption proposals, ownership, retention and deployment inputs |
| [Cost and routing evaluation](cost-routing-evaluation.md) | Evidence limits and a future representative benchmark |
| [Agent-ready data plan](agent-ready-data-plan.md) | Design rationale and the dated implementation checkpoint |
| [ADRs](adr/README.md) | Decision history and superseding clarifications |
| [Diagrams](diagrams/README.md) | Editable Mermaid sources, SVG/PNG exports and rendering commands |

## Dated evidence and historical design

These documents retain the observations, commands and counts from their own
dates. Read the current runbooks before repeating an old procedure.

| Record | What it preserves |
|---|---|
| [September 12 documentation refresh](documentation-validation-20260912.md) | Final guide/diagram checks and repeated 132-result functional run |
| [September 12 validation](agent-ready-validation-20260912.md) | Successive 130-, 131- and 132-result runs with separate immutable receipts |
| [September 10 pilot](agent-ready-pilot-20260910.md) | Real local-model pilot, approval and Task B evidence |
| [September 10 release preparation](agent-ready-release-20260910.md) | Recovery/build evidence and the outstanding vault-dependent cutover |
| [September 6 pilot](agent-ready-pilot-20260906.md) | Earlier retained local-model experiment |
| [Hybrid routing verification](hybrid-routing-verification.md) | Version-specific client/provider experiment |
| [Flowable BPMN case study](blog/local-hybrid-flowable-bpmn-designer.md) | Historical worked example and original screenshots |
| [Original hybrid architecture](hybrid-openclaw-ollama-kimi-architecture.md) | Archived design, including proposals that were not implemented |
| [Third-party notices](../THIRD_PARTY_NOTICES.md) | Attribution and license boundaries |

Documentation checks and reproducible diagram builds are described in the
[developer guide](developer-guide.md#maintain-documentation-and-diagrams).
