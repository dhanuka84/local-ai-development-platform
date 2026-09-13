# Developer guide: what, why and how

Updated September 12, 2026. This guide describes the implementation on
`fix/agent-ready-gaps-20260910`. Use the [documentation index](README.md) for
runbooks, design history and dated evidence.

## What this project does

The platform gives software agents a shared, governed knowledge base and a
durable way to coordinate work. An agent can find a previously validated
procedure, inspect the exact current code and repository relationships, perform
a bounded task, run checks, and save a proposed KB entry with evidence.

For example, Task A fixes a parser and records the patch, checks and procedure.
The proposed entry stays pending. After local validation and the user's explicit
approval of that version, a worker indexes it. Task B can retrieve the approved
procedure, adapt it to its own repository revision and validate the result.
Task B's newly generated entry stays pending even when Task B succeeds.

The executable platform includes:

- A Go MCP gateway for authenticated search, capture, workflows, validation,
  definitions, metrics and graph tools.
- PostgreSQL for authoritative records; Apache AGE and Milvus for rebuildable
  graph and semantic projections.
- A worker for indexing, source checks, evidence retention alerts and trace
  export; an admin CLI and a local two-task pilot executable.
- Ollama integration for local coding and embeddings, plus a separate OpenClaw
  controller plugin and bounded work-packet verifier.

![Implemented local services and their data paths](diagrams/hybrid-ai-local-architecture.png)

OpenClaw is optional for a direct MCP client session. Model invocation belongs
to the client/controller or pilot; the gateway enforces contracts and records
state. An automatic task classifier, general isolated agent runner and automatic
cloud disclosure packager are still proposals. Enterprise identity, object
storage and HA require target-specific implementation and deployment evidence.

## Why it is designed this way

| Decision | Why it matters when you write code |
|---|---|
| PostgreSQL is authoritative | Approval, versions, audit and outbox changes commit together. A vector hit cannot override a newer SQL record. |
| AGE and Milvus are projections | Rebuild indexes from canonical records. Preserve PostgreSQL UUIDs and hydrate results before use. |
| Capture first creates a pending entry | A successful model response or review is insufficient publication evidence. Preserve the exact output without making it reusable automatically. |
| Work packets bind a revision, file scope and checks | A patch must apply to the declared source and pass executed validation in a disposable clone. |
| Provider routes are explicit | Maintenance stays local. A cloud review is conditional, read-only and recorded; there is no silent fallback. |
| Principals and roles are separate from client prompts | An unattended client still authenticates and passes Cerbos, service and database checks. |
| Exact artifacts and append-only events | Later inspection can establish what ran, what was disclosed and why a decision was made. |
| Functional tests have an explicit boundary | Disposable service tests can prove behavior without pretending to certify a production deployment or a model's coding quality. |

The [ADRs](adr/README.md) explain these choices in more detail.

## How the code is organized

| Location | Change it when you need to… |
|---|---|
| `cmd/gateway`, `internal/httpserver`, `internal/mcpserver` | Add a transport/tool boundary or adjust authentication and input/output handling |
| `internal/service` | Change application rules, task transitions, capture, validation or publication |
| `internal/domain` | Define shared records, ports and invariants |
| `internal/postgres`, `migrations` | Change authoritative persistence and transactional behavior |
| `internal/age`, `internal/graphrag`, `internal/milvus` | Change graph projections, bounded retrieval or semantic indexing |
| `internal/ollama`, `cmd/worker` | Change local embedding calls and asynchronous processing |
| `internal/artifacts`, `internal/telemetry` | Change immutable evidence, durable traces and export |
| `internal/contextregistry` | Change source-controlled domain, capability and fixed metric definitions |
| `internal/authorization`, `policies/cerbos` | Change trusted policy context and role/resource permissions |
| `components/codegraph` | Change deterministic source extraction; preserve its MPL-2.0 boundary |
| `components/workpacket`, `cmd/workpacket` | Change bounded patch evaluation and actual validation execution |
| `automation/openclaw-plugin`, `contracts`, `examples/openclaw` | Change controller integration, schemas and workflow examples |
| `cmd/admin`, `cmd/agent-ready-pilot` | Change operator commands or the resumable local-model pilot |
| `internal/e2e`, `tests/agent-ready-coverage.json`, `scripts/agent_ready_e2e.py` | Add runtime scenarios and requirement-to-test evidence |
| `deploy`, `Makefile`, `.github/workflows` | Change local/deployment profiles and reproducible checks |

Read [Implementation Guide](implementation-guide.md) for detailed state and
configuration contracts. Public MCP inputs and outputs come from typed Go
structs. Check the actual tool registration before copying an older example.

## Set up a development checkout

Use Git, Make, Docker Engine with Compose v2, Python 3 and the Go toolchain
selected by [go.mod](../go.mod). The module declares Go 1.25.8 compatibility and
selects toolchain Go 1.26.8. Node/npm is needed for the controller, contract
checks and diagram rendering. A GPU is needed only for your chosen local-model
workload; the deterministic functional suite does not invoke a model.

From the checkout you intend to change:

```bash
git status --short
git branch --show-current
make help
make build
make check
```

Preserve existing work and create a focused branch or worktree when needed.
If Go is not on the host PATH, `make check-container` builds and uses the pinned
check image. Running the full functional suite is also independent of a live
platform vault:

```bash
make agent-ready-functional
```

This builds the actual binaries, creates disposable services on an isolated
Compose network, runs the mapped tests, retains receipts under
`.local/agent-ready-e2e/`, and removes only that invocation's services/volumes.
Dependency builds may need network access. See the [E2E guide](agent-ready-e2e.md).

## Start a persistent local stack

For a new workstation, install the vault dependency into a local environment
and initialize credentials once:

```bash
python3 -m venv .local/dev-venv
. .local/dev-venv/bin/activate
python3 -m pip install -r requirements/vault.txt
make env-init
make up
make mcp-status
```

Use `make up-gpu` instead of `make up` for an NVIDIA-equipped host. Initial
startup pulls the embedding model; `make pull-local-model` pulls the configured
coding model. Existing installations keep their `.env` and encrypted vault;
follow [Operations](operations.md) for unlock, migration or recovery.

`make mcp-status` checks the MCP stack. `make platform-status` additionally
requires the OpenClaw integration. Install/configure that separately with
`make openclaw-setup`, then run `make openclaw-start` in its own terminal.

Runtime commands materialize credential files in private tmpfs. A missing
vault passphrase/token is an access prerequisite, not a reason to replace a
vault or disable authentication. Continue code and disposable-test work while
live access is unavailable.

For an authenticated, read-only MCP smoke request:

```bash
make mcp-call MCP_TOOL=knowledge_search \
  MCP_ARGUMENTS='{"project_id":"local-development","query":"transactional outbox","limit":3}'
```

An empty search on a new installation is expected. It does not justify creating
or approving synthetic knowledge in the persistent database.

## Choose the client and authority deliberately

`make dev-session` launches the configured cloud Codex session with the local
MCP server. `make dev-session-local` selects Ollama explicitly. The local client
has a historically observed deferred-MCP-tool limitation; the current launcher
regression verifies arguments and configuration, not model tool-use reliability.
Use the OpenClaw local route for governed local inference and consult the
[routing evidence](hybrid-routing-verification.md) before relying on a specific
client/model combination. Maintenance must use local models only.

The default `human:local-developer` has Development, QA, Product Owner and
Operations roles. The `development` role alone has fewer permissions. OpenClaw
uses a different controller credential, and delegated task credentials are
expiring workload identities with narrower authority.

| Action | Authority and evidence |
|---|---|
| Implement, test, inspect or index within the assigned task | Standing task authorization; normal authentication, scope and policy checks still apply |
| Validate and publish a domain/capability/metric definition | Existing operator QA/Product Owner authority; exact version/hash, registry validation and a reason recording delegated task authorization |
| Capture a useful outcome | Save a pending KB entry with procedure, evidence, repository revision, provider and model |
| Publish a generated KB entry | The user's explicit decision on the exact candidate version, backed by trusted local validation and source freshness |
| Complete a validated reuse task | Record eligible context and a passing trusted validation report; the new candidate remains pending |

Unattended client settings do not grant server roles. `AUTO_APPROVE_LOCAL=true`
is rejected at startup. Never turn off validation, use direct SQL to publish,
or label delegated execution as a manual per-item user review.

## Follow one task through the system

1. Search approved knowledge before substantial design/debugging when MCP is
   available. Use `repository_graph_get` if another repository may be affected.
   Hydrate semantic hits from PostgreSQL and inspect current source.
2. Queue governed work with `workflow_task_begin`. Only the FIFO head activates;
   activation stores a fresh RAG lookup and selects the permitted route.
3. Prepare a `hybrid-ai/work-packet/v1` for delegated patches. Evaluate it, make
   the bounded change, then run the actual verifier:

   ```bash
   make workpacket-evaluate PACKET=/absolute/path/to/work-packet.json
   make workpacket-verify \
     PACKET=/absolute/path/to/work-packet.json \
     PATCH=/absolute/path/to/candidate.patch
   ```

4. Capture exact output and record the local result. A strong approved RAG hit
   skips cloud review. An allowed development miss uses the read-only review
   lane; maintenance/protected-data work stays local. Retain the exact review
   and disclosed-context manifest; reproduce accepted findings locally.
5. Run trusted local validation against the bound version and source. Validation
   failure returns to revision. Source changes require renewed validation.
6. For reuse, record exact context use and `VALIDATED_REUSE_COMPLETED` with
   `payload.validation_id`; the service rechecks eligibility and completes the
   task while leaving its new KB candidate pending. For new knowledge awaiting
   publication, preserve `promotion_required` until the explicit user decision.
7. After an approved candidate is indexed, `RAG_READBACK_VERIFIED` requires its
   exact PostgreSQL UUID from Milvus. Successful completion activates the next
   queued task. Keep independent work moving when a publication gate is pending.

![Knowledge entries and definitions have separate publication rules](diagrams/hybrid-ai-review-learning-explainer.png)

The [knowledge operations guide](agent-ready-data-operations.md) has executable
validation/decision examples. Never substitute handwritten success text for an
executed work-packet report.

## Implement a feature from boundary to persistence

For a new tool or changed behavior, start with its observable success and failure
conditions. Read the relevant service, repository implementation and existing
tests before choosing a design.

1. Define typed inputs/outputs and domain behavior. Identify the project, caller,
   expected version and retry identity for mutations.
2. Implement service validation and trusted authorization. Derive actor and
   resource attributes from authenticated/database state, not model arguments.
3. Add transactional persistence and a new numbered migration if necessary.
   Preserve immutable evidence and emit projection intent through the outbox.
4. Register the MCP tool with accurate read/write annotations. Update schemas,
   fixtures, client examples and relevant policy tests.
5. Add an E2E case through the actual public boundary, including a meaningful
   denial/failure case. Assert durable state and retrieval/export behavior where
   relevant. Keep component tests for concurrency or edge cases the E2E cannot
   isolate clearly.
6. Update the guide, affected diagram and coverage map, then run the applicable
   checks below. Describe migrations, compatibility and rollback in the PR.

Do not edit a migration already used outside a disposable test. Do not write
canonical knowledge directly into Milvus, infer approval from model confidence,
or return a stale index row without checking current PostgreSQL eligibility.

## Test each requirement end to end

| Change or question | Command and evidence |
|---|---|
| Every repository handoff | `make check` — formatting, vet, race-enabled Go tests and repository script regressions |
| Agent-ready functional behavior | `make agent-ready-functional` — all mapped requirements against actual disposable services |
| PostgreSQL migration/adapter change | `make integration-test-fresh` — fresh disposable PostgreSQL integration |
| Authorization policy | `make authz-policy-test` — real Cerbos policy compilation and allow/deny fixtures |
| Contract schema/example | `make contracts-check` |
| OpenClaw integration | `make openclaw-plugin-check` and `make openclaw-config-check` |
| Vault or Drive client | `make vault-test` or `make gdrive-test` |
| Backup/restore behavior | `make backup-restore-test` — disposable volumes and a fake Drive client |
| Documentation/diagram edit | `make diagrams` after Mermaid changes, then `make docs-check` |

For a new checklist requirement, update both the
[checklist](agent-ready-gap-checklist.md) and the
[coverage map](../tests/agent-ready-coverage.json). Give it a concrete
`functional_requirement`, exact test names and an evidence boundary. If full
rollout is still open, record its `deferred_acceptance` prerequisites separately.
Missing, skipped or failed mapped tests fail acceptance; any failed suite fails
the overall run even if all mapped test names passed.

Review `summary.json` and `functional-acceptance.md` from the new run, not an old
passing receipt. Retain exact JSONL, stderr, source manifest and SHA-256 inventory.
CI runs the same functional command and uploads receipts, including failures.
`make check` alone skips service-dependent tests and cannot replace this suite.

The recorded September 12 result is 28/28 functional requirements, 11 suites and
132 passing test/subtest results. It uses synthetic Ollama responses and approval
fixtures in disposable databases. Real-model quality, timed adoption cohorts,
live cutover and target-cluster recovery need their own evidence; see the
[current scope](agent-ready-gap-checklist.md).

## Diagnose common problems

| Symptom | Inspect and act |
|---|---|
| MCP 401 or missing token file | Use the existing vault unlock/materialization procedure; verify the selected credential without printing it. Keep disposable tests independent. |
| MCP 403 | Inspect authenticated roles, project/task scope and Cerbos evidence. Client approval settings cannot fix a denied role. |
| A validation or decision is stale | Fetch the current candidate/source version and run the trusted validator again. Use its exact validation ID. |
| Approved item is absent from search | Check source/validation eligibility, outbox errors, embedding model/dimension and Milvus projection metadata. A SQL fallback does not satisfy Milvus read-back. |
| Task remains queued | Inspect the earlier active task and its required checkpoint. Waiting does not mean rejection. |
| Task waits at publication | Leave the generated KB entry pending for the user; continue independent work. Validated definitions follow their separate rule. |
| Local client does not call MCP | Distinguish launcher configuration from model tool-use behavior; inspect the retained routing experiment and use the tested controller/tool route. |
| Worker restarted | Inspect durable outbox/export state and traces. Runtime E2E checks restart recovery; do not delete evidence to clear a queue. |

## Maintain documentation and diagrams

Update living guides when commands, roles, states or evidence boundaries change.
Keep dated receipts and historical designs clearly labeled; preserve their
original measurements. Link new results instead of rewriting an earlier run.

Edit Mermaid sources under `docs/diagrams/`, then run:

```bash
make diagrams
make docs-check
make check
git diff --check
```

Rendering uses pinned Mermaid CLI 11.16.0 and local Chrome. The diagram guide
documents browser configuration, individual targets and source/export checks.
`docs-check` checks local Markdown links/anchors, documentation coverage and
the source/export manifest. It does not contact external websites or certify
the behavior of example cloud services.

Before handing off, state what changed, why, checks actually run, the tested
revision/source snapshot and any remaining prerequisites. Capture a useful
validated procedure as pending through `generation_capture` when MCP is
available. If access is unavailable, retain an explicitly unsubmitted local
draft; do not claim that it is in the KB.
