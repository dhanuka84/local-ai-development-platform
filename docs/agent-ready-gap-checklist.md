# Agent-ready data gap checklist

Updated: 2026-09-10. Baseline: `ce8b7d3`.
Implementation branch: `fix/agent-ready-gaps-20260910`.

This is the working completion register for the
[five-gap plan](agent-ready-data-plan.md),
[operations runbook](agent-ready-data-operations.md), and
[retained local pilot](agent-ready-pilot-20260906.md).
The comparison source is
[Making Your Data Ready for Agentic AI](https://martinfowler.com/articles/making-data-ready-for-agentic-ai.html).

A checked item has the evidence stated in its row. Implementation, deterministic
verification, real-model demonstration, human publication, and deployment are
separate outcomes. Role owners below are responsibilities to assign; they do
not record a person's acceptance or authorize publication. Keep pending
knowledge pending until its exact version has relevant validation and an
accountable human decision.

## Local release

| Done | ID | Requirement | Owner role | Evidence / remaining work |
|---|---|---|---|---|
| [x] | L01 | One version-bound promotion gate across service, MCP and CLI | Platform development / QA | Implemented in `e44cbb4`; current PostgreSQL integration tests pass, including concurrency and invalid evidence. |
| [x] | L02 | Compile contracts and enforce source/validation requirements | Platform development / QA | Versioned contracts and positive/negative fixtures exist; rerun contract checks at final handoff. |
| [x] | L03 | Enforce source, content and projection freshness; quarantine and recovery | Platform development / Operations | Current PostgreSQL and Milvus integration suites pass; policies apply to retrieval and recovery. |
| [x] | L04 | Reconstruct decisions and preserve immutable operation evidence | Platform development / QA | Trace persistence and mandatory-evidence tests pass; real pilot repair rejection needs a more explicit outcome receipt. |
| [x] | L05 | Govern domain, capability and metric definitions | Platform development / QA | Current deterministic acceptance verifies definitions, all four metrics, project isolation and negative cases. Real definitions still require human validation and approval. |
| [x] | L06 | Demonstrate two-task state transitions with deterministic dependencies | QA | `make agent-ready-acceptance` passed on 2026-09-10 with disposable PostgreSQL, Milvus and Cerbos; no real human approval or model quality is claimed. |
| [x] | L07 | Give every pilot repair attempt an accurate, durable outcome | Platform development | Distinct no-correction errors and durable repair outcomes verified in the retained pilot. Explicit syntax-only recount preserves source bytes and records derived evidence. |
| [x] | L08 | Pass Task A using actual local generation and bounded verification | Local workload / QA | Passed 2026-09-10 after deterministic hunk recount and bytecode suppression; six behavior tests, diff and scope checks passed. See the [recovery receipt](agent-ready-pilot-20260910.md). |
| [ ] | L09 | Review and publish the exact validated Task A candidate | Accountable human Product Owner | Wait for L08; inspect candidate, report and source manifest before a version-bound decision. Implementation authorization is not knowledge approval. |
| [ ] | L10 | Verify Task A publication in Milvus | Operations / local worker | Requires L09 and exact ID/version/digest read-back. |
| [ ] | L11 | Complete actual Task B reuse and its local verification | Local workload / QA | Requires L10; record eligible `used_context`, retain exact local output, and leave Task B's new lesson pending. |
| [ ] | L12 | Produce a complete real-pilot report and inspect governed metrics | QA / Product Owner | Requires L08–L11; approve metric definitions separately, report actual values and trace completeness. |
| [x] | L13 | Make the full deterministic acceptance environment reproducible | Platform development | `make agent-ready-integration` provisions its own private dependencies, runs the adapters and acceptance, and removes only its own resources. The full command passed locally; the checked-in CI job uses that command. |
| [x] | L14 | Rehearse upgrade from migration 000007 and inventory legacy evidence | Operations / QA | `TestUpgradeFromV7PreservesLegacyEvidenceIntegration` passes in a separate disposable database, including repeat migration, preserved statuses/content and withheld historical approval without invented evidence. Read-only live inventory: 000007 and five pending candidates. |
| [ ] | L15 | Prepare coordinated live gateway, worker, CLI and policy rollout | Operations | Build and verify compatible artifacts, backup/recovery plan and read-only inventory before a concrete rollout decision. Historical live baseline was 000007; recheck current state. |
| [x] | L16 | Verify local collector integration and prepare rollout configuration | Operations | Actual Collector 0.160.0 accepted synthetic receipts in the disposable integration run. An opt-in internal-network Compose overlay is ready; live activation remains in L15. |
| [x] | L17 | Document and check the September 10 implementation slice | Platform development / QA | Checklist, recovery receipt and runbook updated. `make check`, contracts, policy tests and full disposable integration passed. Remaining publication/deployment rows are still open. |

## Broader article and deployment coverage

These items keep previously deferred or incompletely mapped topics visible.
They are not prerequisites invented for the bounded local pilot. Deployment
requirements need environment-specific decisions and evidence.

| Done | ID | Requirement | Owner role | Evidence / remaining work |
|---|---|---|---|---|
| [ ] | E01 | Delegated per-user access and short-lived task credentials | Security / Platform development | Explicitly deferred in the plan; design delegation, scope intersection, expiry/revocation and actor attribution, then test bypass cases. |
| [ ] | E02 | Evidence-based staged autonomy | Product Owner / QA | Human gates and reversibility declarations exist; define shadow/supervised/guarded stages and measured promotion criteria. |
| [x] | E03 | Explicit raw/validated/certified tier mapping | Data owner / Platform development | See the tier mapping below; artifact/review access is separate from normal eligible knowledge retrieval. |
| [ ] | E04 | Adaptive Gold or an explicit applicability decision | Data owner / Product Owner | The current system proposes reusable lessons; no measured agent-curated dataset/view lifecycle is documented. Evaluate need before adding one. |
| [ ] | E05 | Extended unstructured quality checks | Data owner / QA | Empty/truncated content, exact duplicates and invalid vectors are covered; define evaluated near-duplicate, extraction/OCR and drift handling where relevant to supported sources. |
| [ ] | E06 | Named ownership, review cadence and deprecation lifecycle | Product Owner / Operations | Definitions and policies have ownership fields; assign accountable owners and publish maintenance/deprecation procedures. |
| [ ] | E07 | Production evidence retention, archival, holds and deletion | Operations / data owner | Configurable retention currently triggers review/hold; choose and validate deployment-specific storage and deletion controls. |
| [ ] | E08 | Enterprise tenant isolation and availability | Platform / Operations | Separate deployment work in the plan; validate identity, data isolation, backups, recovery, scaling and failure behavior for the target environment. |
| [ ] | E09 | Kubernetes repository snapshot mounts | Operations | Gateway/worker need matching read-only immutable snapshots and allowed roots; provide and validate a deployment overlay. |
| [x] | E10 | Complete article-to-capability evidence mapping | Platform development / QA | The coverage map below connects the article's areas to concrete checklist items, including explicit deferred deployment and exploratory work. Individual open rows remain open. |

## Coverage and tier mapping

| Article area | Checklist / platform evidence |
|---|---|
| Contracts, CI quality gates and per-consumer freshness | L02, L03, L13; versioned schema fixtures and purpose-scoped quality policies |
| Quarantine and hard data-quality gates | L01, L03; shared PostgreSQL eligibility and review/recovery queues |
| Unstructured source/version/scope metadata and index freshness | L02, L03, E05; source and projection manifests, separate successful-verification clocks |
| Certified access and agent-curated outputs | E03, E04; tiers below and pending generalized lesson capture |
| Decision lineage, instrumentation and retained evidence | L04, L07, L12, L16, E07; durable operations, CAS artifacts and local export |
| Staged autonomy and authorization | L01, E01, E02; human publication gates, Cerbos and declared reversibility |
| Domain, metric and capability definitions | L05, E06; source-controlled context registry and fixed parameterized metric queries |
| Knowledge graphs | L03, L05; authoritative topology with AGE/Milvus discovery and PostgreSQL hydration |
| Retrieval, live queries and controlled writes | L05, L06, L08–L12; knowledge tools, metric queries and guarded workflow actions |
| Retrieved text cannot authorize actions | L01, L03, L05; executable live preconditions and explicit human decisions |
| Capability curation, ownership and maintenance | L05, E06; registry declarations, resource exposure and pending operating-model decisions |
| Deployment readiness | L14–L16, E07–E09; rehearsed migration and explicit remaining production requirements |

The platform's immutable raw generation/source artifacts correspond to the raw
tier. Exact-version validations and source checks form the validated tier.
Normal knowledge access is restricted to approved records that are currently
eligible, corresponding to the certified tier. Historical approval alone is
insufficient. Artifact and pending-candidate review requires explicit review
permissions; those routes are not ordinary development retrieval. AGE and
Milvus remain derived projections of this authoritative state.

Agent-generated improvements stay pending until validated and explicitly
approved. That is a governed proposal lifecycle, not a claim that the platform
implements agent-created warehouse views. E04 retains the separate decision on
whether that additional capability is useful here.

## Validation log

- 2026-09-10: `knowledge_search` through the live local MCP gateway returned no
  matching approved knowledge in `local-development`.
- 2026-09-10: `repository_graph_get` returned no recorded consumer relationships
  for this repository in that project; this does not prove all external clients
  are registered.
- 2026-09-10: baseline `make check` passed, including race tests.
- 2026-09-10: `go test -race -count=1 ./internal/postgres`,
  `go test -count=1 ./internal/milvus` and `make agent-ready-acceptance` passed
  against disposable dependencies. An initial attempt ran before the fresh
  database was ready and failed to connect; the ready-database run passed.
- 2026-09-10: real local `ollama` / `qwen3.6:35b` repair returned an unchanged
  malformed patch; it was rejected before validation. Exact output:
  `09ca44ff9d48d874e893497bf03a3cf7c9afc15b27cee49f2573a813cfe1c918`.
- 2026-09-10: Apache AGE integration tests, contract fixtures and all 61 Cerbos
  policy tests passed. Task A passed with validation
  `92bce3e3-6eda-422f-8b59-a9f196dd2fb4`; actual human publication is pending.
- 2026-09-10: the final `make agent-ready-integration` run passed PostgreSQL
  (including v7 upgrade), AGE, Milvus, real collector export and the two-task
  acceptance on its private network. Its disposable resources were removed.
- 2026-09-10: final `make check`, `make contracts-check` and `git diff --check`
  passed after the implementation changes. Tests used local execution and
  synthetic data; no cloud review or inference fallback was invoked.

Record each future completed item with command/report evidence, repository
revision, and actual provider/model if inference occurred. Proposed reusable
findings may be captured with `generation_capture` only as pending knowledge;
checklist marks do not approve them.
