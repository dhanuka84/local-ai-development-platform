# Project documentation

Updated September 14, 2026. Start with the [developer guide](developer-guide.md)
for what the platform does, why it is built this way, and how to change it.
The [implementation guide](implementation-guide.md) describes the code and
contracts; the runbooks describe operating an installed stack.

For the product direction, read the
[expectations and scope diagram](ai-native-sdlc-expectations.md). The
[lifecycle guide](sdlc-guide.md) maps KB use and responsibility across all 15 stages.
The [runtime guide](sdlc-runtime.md) explains the implemented feature and incident
profiles, configuration, operator commands and package governance.

## Current status

The [completion checklist](sdlc-completion-checklist.md) is the current A01–A12
functional closure record. The [assessment](sdlc-gap-assessment.md) maps those
proofs to the ten product expectations and preserves the original findings.
`make agent-ready-functional` runs both the existing 28-item suite and the new
native-service suite. The optional explicit local-model profile records real
inference separately from protocol fixtures.

The [September 13 product-KB receipt](ai-native-sdlc-implementation-20260913.md)
and [September 12 receipt](documentation-validation-20260912.md) preserve their
own snapshots, counts and narrower acceptance boundaries. Their historical
results are not a claim that later source was tested. The
[new documentation validation](documentation-scope-validation-20260914.md)
records the current scope review.

Live rollout, representative adoption/model cohorts, named enterprise ownership,
identity, retention, storage and HA/recovery remain target acceptance. The
[older checklist](agent-ready-gap-checklist.md) preserves its 22/28 rollout count
and 28/28 local functional boundary. A deferral never turns a failing test into a pass.

Assigned local work proceeds under the operator's existing authority. Validated
domain/capability/metric definitions may be published under standing task
authorization. Generated KB entries remain pending until the user explicitly
approves their exact version. See [ADR-0011](adr/0011-scoped-autonomy-and-functional-acceptance.md).

## Start and develop

| Read | Use it for |
|---|---|
| [Project overview](../README.md) | Purpose, quick start and public tool examples |
| [Developer guide](developer-guide.md) | What, why, setup, first change, tests and troubleshooting |
| [AI-native SDLC expectations and scope](ai-native-sdlc-expectations.md) | Product expectations, architecture diagram, two-dimensional KB, MCP sources, agent accountability and incident walkthrough |
| [AI-native lifecycle guide](sdlc-guide.md) | Agent responsibilities and KB use through 15 stages, current commands and a worked feature example |
| [Runtime and operator guide](sdlc-runtime.md) | Configure local roles, execute feature and incident work, inspect evidence and qualify/roll back agent packages |
| [Product KB and evaluated sources](product-knowledge-and-evaluated-sources.md) | Versioned records, accepted intent, source setup, field permissions and business reconciliation |
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
| [AI-native SDLC gap assessment](sdlc-gap-assessment.md) | 12 primary capability gaps, source evidence, owners, acceptance tests and a KB-first delivery order |
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
