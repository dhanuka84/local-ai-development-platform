# Role Workflows and Make Commands

Updated September 13, 2026. For the first development task, use the
[developer guide](developer-guide.md); current acceptance is tracked in the
[functional checklist](agent-ready-gap-checklist.md).

## Purpose

This guide groups commands by responsibility. The role prefix shows which
“hat” the user is wearing. It does not require four different employees or
accounts. Start with:

```bash
make help-operations
make help-development
make help-qa
make help-product-owner
```

For an assigned local task, these roles can be exercised under standing user
authorization without repeated confirmation. Validated domain/capability/metric
definitions may be published using the existing operator roles and exact
validation evidence. Generated KB entries require the user's explicit decision
on the exact candidate version. See
[autonomous local operation](operations.md#autonomous-local-operation).

The aliases do not grant permission by themselves. In the default `solo`
profile, one authenticated person may perform Operations, Development, QA, and
Product Owner actions in sequence. The platform still records the role,
evidence, and decision for every gate.

The local profile creates one human identity with all four roles and a separate
non-human OpenClaw controller. The credential determines the identity; a model
cannot claim another identity in an MCP field. Cerbos checks every protected
action. `team` projects may require different people at selected gates, and
`regulated` projects can require them at sensitive gates. The API for a true
two-person approval is still planned.

| Role | Plain-English responsibility |
|---|---|
| Operations | Own service outcomes, authorized deployment/recovery, platform health, data and credentials. |
| Development | Ground intent in product/code knowledge, design and implement the change, run checks and save evidence. |
| QA | Verify the change independently and record findings. |
| Product Owner | Own product intent and acceptance; separately decide whether an exact validated generated KB entry may be published. |

These are current operator roles. The
[agent responsibility matrix](ai-native-sdlc-expectations.md#accountable-roles-with-different-responsibilities)
defines the target product/BRS, architecture, implementation, evaluation,
security, release, diagnosis, maintenance, curation and coordination agents.
Each needs workload identity, delegated task/scope and an accountable owner;
selecting a prompt or Make alias does not provision those capabilities.
The existing task delegation remains scoped to local Development. The separate
[SDLC runtime](sdlc-runtime.md#execution-and-responsibility) provisions five
distinct workload roles with per-run, product, source, field, purpose and
environment scope, total budgets and correlated audit.

Use the [15-stage KB map](sdlc-guide.md#the-shared-kb-throughout-the-sdlc) for each
role's inputs and evaluated outputs. The commands below operate the existing
platform contracts, not the complete target lifecycle. Replace uppercase
placeholder values with the inspected IDs and exact versions before use.

## Operations

Operations owns platform availability, data safety, credentials, migrations,
and projection health.

### One-time host setup

```bash
make env-init
# Enter the vault passphrase, then review non-secret .env settings.
make openclaw-setup
make mcp-preflight
make ops-start-gpu       # or: make ops-start
make pull-local-model
# In a separate terminal (blocking): make openclaw-start
# From a third terminal after OpenClaw starts:
make platform-status
```

Compose startup applies PostgreSQL migrations, initializes Apache AGE, and initializes Milvus before the
gateway and worker become ready.

### Normal platform session

```bash
make ops-start-gpu       # or: make ops-start
# In a separate terminal (blocking): make openclaw-start
make platform-status
make ops-logs            # optional; follows logs until interrupted
make ops-stop            # retains all named volumes
```

### Administration and recovery

```bash
make migrate
make age-rebuild
make milvus-init
make ops-doctor
make ops-reindex
```

### Organization repository indexing

Operations owns checkout cleanliness, catalog completeness, projection lag,
and worker capacity. Development owns the meaning of an intentional
non-default branch override.

```bash
make repository-org-index-all \
  GITHUB_ORG=example-organization \
  REPOSITORY_PROJECT=local-development \
  REPOSITORY_BRANCH_OVERRIDES='large-engine=java-25'
make repository-org-wait
make repository-org-verify
```

The workflow catalogs documentation-only repositories without creating empty
code graphs, retains snapshots already at the exact branch/revision, and
indexes only stale supported source. If a corrected scan leaves superseded
vector events, Operations runs:

```bash
make compact-code-outbox
make worker-scale-postgres-fallback WORKER_REPLICAS=3
make repository-org-wait
make worker-scale-postgres-fallback WORKER_REPLICAS=1
```

Scaling is temporary. The final handoff requires zero pending and zero failed
code-index events plus a semantic result hydrated from the intended active
branch and revision.

For a single repository, Operations can use the same guarded path without
enumerating an organization:

```bash
make repository-sync-one \
  REPO=/absolute/path/to/source-repositories/large-engine \
  REPOSITORY_URL=https://github.com/example-organization/large-engine.git \
  REPOSITORY_BRANCH=java-25
make repository-index-one-all \
  REPO=/absolute/path/to/source-repositories/large-engine \
  REPOSITORY_PROJECT=local-development
```

The single-repository workflow refuses a dirty checkout, validates its origin
and active branch, derives the exact revision, waits for semantic projection,
and reports the forge default separately from the analyzed branch.

`migrate`, `age-rebuild`, `milvus-init`, `doctor`, candidate commands, and `reindex` execute
through the Compose admin image. They therefore use the same database password
and service-network addresses as the running platform without exporting a
database URL into the developer shell.

While acting as Operations, the person must not approve technical or product
knowledge. The same person may later make that decision only through the
explicit Product Owner gate in `solo` mode. After Product Owner approval,
Operations monitors the outbox worker and projection freshness.

## Development

Development owns FIFO task submission, local Ollama implementation, local
checks, cloud-context minimization for a policy-allowed RAG miss, review
disposition, and candidate capture.

### One-time Codex setup

```bash
make mcp-preflight

# Required only for the cloud-backed Codex route:
make codex-login
make preflight
```

### Per-task workflow

```bash
make codex-route
make dev-session
# Or work in another checkout:
make dev-session-repo REPO=/absolute/path/to/repository

# Local-Qwen alternative for tasks that do not require MCP tool calls:
make codex-local-smoke
make dev-session-local-repo REPO=/absolute/path/to/repository
```

The local targets explicitly select `ollama/$LOCAL_CHAT_MODEL` and print the
route before Codex starts. The startup banner (`provider: ollama`) and the
inference smoke test prove local model inference. The retained experiment with Codex CLI `0.147.0` found deferred MCP
tool schemas that local Qwen did not reliably invoke. The September 12 launcher
checks cover configuration and arguments, not a new model tool-use experiment. Use the standard Codex session or the OpenClaw local route when the
task requires `hybrid_knowledge` tools.

Inside the governed OpenClaw flow, Development calls `workflow_task_begin` for
each atomic task. Extra tasks remain queued instead of being rejected. Only the
head is activated, and its RAG lookup is performed at activation. The route is
then enforced as `rag_hit`, `rag_miss_cloud_review`, or
`rag_miss_local_only`. For an OpenClaw-delegated patch:

```bash
make dev-policy-check PACKET=/path/to/work-packet.json
make dev-patch-verify \
  PACKET=/path/to/work-packet.json \
  PATCH=/path/to/candidate.patch
make dev-check
# Required for changed agent-ready functional behavior:
make agent-ready-functional
# Required when `policies/cerbos` changes:
make dev-authz-policy-test
```

After the initial local result, Development uses `generation_capture` through
MCP and records `LOCAL_RESULT_RECORDED` with `provider=ollama`. A required cloud
review runs read-only; its raw output and context manifest are evidence only.
Development then asks Ollama to apply accepted findings, records a local
`revise`, and reruns validation. Development passes the pending candidate UUID,
work packet, patch/diff, and evidence to QA. In `team` or
`regulated` mode, policy can prevent the implementing principal from performing
later decisions. In `solo` mode, the same person may continue only by entering
the separate QA and Product Owner transitions with the required evidence.

For validated reuse, record eligible context use and a trusted validation ID,
then complete with `VALIDATED_REUSE_COMPLETED`; the new KB entry stays pending.
For new KB publication, after the explicit user decision and outbox indexing, Development records
`RAG_READBACK_VERIFIED`. The next queued task activates automatically. Product
Owner or Operations may explicitly reject a queued task with `TASK_REJECTED`;
normal waiting never uses rejection.

When Development requests a branch that differs from the forge default, it
must state the repository, intended branch, reason, and expected commit. The
Make workflow stores the forge default in the catalog and the analyzed branch
on the code run, so QA can verify both independently.

## QA

QA independently verifies implementation scope, tests, review dispositions,
and reusable-knowledge quality.

### Candidate and source inspection

```bash
make qa-candidates PROJECT=my-product LIMIT=25
make qa-candidate-get ID=CANDIDATE_UUID
make qa-session-repo REPO=/absolute/path/to/repository
# Local alternative only when this session does not need MCP tool calls:
make qa-session-local-repo REPO=/absolute/path/to/repository
```

### Independent verification

```bash
make qa-patch-verify \
  PACKET=/path/to/work-packet.json \
  PATCH=/path/to/candidate.patch
make qa-check
# Required when `policies/cerbos` changes:
make qa-authz-policy-test
```

QA records its independent verdict and findings with `review_record` through
MCP. Only the local Ollama path may submit `revise`, with its actual
`provider=ollama`, improved content and fresh local validation evidence. A
Codex QA review records `approve`, `reject` or `comment` and requests local
revision when needed. A review does not create a publication validation report
or approve knowledge. Record exact-version validation through the
[QA validation procedure](agent-ready-data-operations.md#validate-and-decide):
use the authenticated CLI for executed work-packet checks, or
`knowledge_validation_record` for an appropriate human attestation. Preserve
the returned `validation_id`; an attestation cannot claim command execution.

When validation passes, QA hands the candidate UUID/version, validation ID and
review evidence to the Product Owner. When it fails, QA records `reject` or
`comment` feedback and returns the work to Development for local revision.

## Product Owner

The Product Owner owns business acceptance and the final decision to publish a
validated candidate as reusable organizational knowledge.

### Review the pending candidate

```bash
make po-candidates PROJECT=my-product LIMIT=25
make po-candidate-get ID=CANDIDATE_UUID
```

Before deciding, confirm:

- acceptance criteria and intended product behavior;
- QA's independent review and validation evidence;
- applicability, exclusions, and rollback guidance;
- data-classification and cloud-disclosure compliance;
- that the candidate is generalized and not merely raw model output.

### Make the explicit decision

After the user's explicit decision on that exact version, use the inspected
values, a stable decision key and an accurate reason. Version `1` below is an
example; a later candidate version needs its own matching report.

```bash
make po-approve ID=CANDIDATE_UUID VERSION=1 VALIDATION_ID=VALIDATION_UUID \
  DECISION_KEY=UNIQUE_DECISION_KEY REASON='User approved this exact validated version'
# or
make po-reject ID=CANDIDATE_UUID VERSION=1 \
  DECISION_KEY=UNIQUE_REJECTION_KEY REASON='Rejected this candidate after review'
```

Approval commits the knowledge decision and outbox intent in PostgreSQL.
Embedding and Milvus publication remain asynchronous. Always use the real
authenticated identity. A solo operator reuses their own identity with a new
`acting_role`; they never invent or borrow another person's identity.

## Solo operator sequence

One person can run the full local workflow without creating four accounts:

```bash
# Acting as Operations
make ops-start-gpu       # or: make ops-start
make ops-status

# Acting as Development
make dev-session-repo REPO=/absolute/path/to/repository
make dev-policy-check PACKET=/path/to/work-packet.json
make dev-patch-verify PACKET=/path/to/work-packet.json PATCH=/path/to/candidate.patch
make dev-check
# Required for changed agent-ready functional behavior:
make agent-ready-functional

# Acting as QA in a clean checkout/session
make qa-patch-verify PACKET=/path/to/work-packet.json PATCH=/path/to/candidate.patch
make qa-check
make qa-candidate-get ID=CANDIDATE_UUID

# Acting as Product Owner after QA evidence exists
make po-candidate-get ID=CANDIDATE_UUID
# Only after the explicit decision and matching version-bound validation report:
make po-approve ID=CANDIDATE_UUID VERSION=1 VALIDATION_ID=VALIDATION_UUID \
  DECISION_KEY=UNIQUE_DECISION_KEY REASON='User approved this exact validated version'

# Acting as Operations again
make ops-doctor
```

Changing role is an explicit workflow transition, not a logout/login exercise.
The OpenClaw flow pauses at human gates and the same person can confirm the
next acting role in `solo` mode through their human MCP credential. The
non-human controller credential cannot cross those gates. Cerbos evaluates that action
against the project profile; PostgreSQL will validate and commit the state
transition.

## Handoff sequence

```text
Operations
  platform ready
      ↓
Development
  work packet → implementation → local checks → generation_capture
      ↓ candidate UUID + patch + evidence
QA
  independent verification → exact-version validation report + review_record
      ↓ candidate UUID/version + validation ID + evidence
Product Owner
  explicit generated-KB decision → versioned po-approve / po-reject
      ↓ committed outbox event
Operations
  monitor worker and semantic projection freshness
```

This is the knowledge-publication handoff. Product acceptance, artifact release,
deployment authorization and verified business recovery require their own
records in the [lifecycle guide](sdlc-guide.md); KB approval does not perform them.

## Command ownership summary

| Role | Workflow commands | Canonical/supporting commands |
|---|---|---|
| Operations | `ops-start`, `ops-start-gpu`, `ops-status`, `ops-logs`, `ops-stop`, `ops-doctor`, `ops-reindex` | `env-init`, `vault-init`, `vault-import-env`, `vault-set`, `vault-check`, `vault-list`, `vault-materialize`, `vault-clean`, `vault-test`, `gdrive-install`, `gdrive-auth`, `gdrive-check`, `gdrive-test`, `backup-gdrive`, `download-gdrive`, `restore-gdrive`, `backup-restore-test`, `mcp-preflight`, `migrate`, `migrate-postgres-fallback`, `age-rebuild`, `milvus-init`, `repository-org-sync`, `repository-org-catalog`, `repository-org-index`, `repository-org-wait`, `repository-org-verify`, `compact-code-outbox`, `worker-scale-postgres-fallback`, `up`, `up-gpu`, `rebuild-fresh`, `rebuild-fresh-gpu`, `down`, `logs`, `mcp-start`, `mcp-start-gpu`, `mcp-status`, `mcp-logs`, `mcp-stop`, `doctor`, `reindex`, `pull-local-model`, `models-list`, `openclaw-config-check`, `openclaw-config-plan`, `openclaw-config-apply`, `openclaw-plugin-install`, `openclaw-setup`, `openclaw-start`, `openclaw-status`, `platform-status` |
| Development | `dev-session`, `dev-session-repo`, `dev-policy-check`, `dev-patch-verify`, `dev-check`, `dev-authz-policy-test` | `codex-login`, `codex-check`, `codex`, `codex-repo`, `preflight`, `fmt`, `test`, `check`, `build`, `workpacket-build`, `workpacket-evaluate`, `workpacket-verify`, `authz-policy-test`, `diagram-review-loop`, `diagram-agentic-workflow`, `clean`, plus MCP retrieval/capture/review tools |
| QA | `qa-session`, `qa-session-repo`, `qa-patch-verify`, `qa-check`, `qa-authz-policy-test`, `qa-candidates`, `qa-candidate-get` | `candidate-list`, `candidate-get`, `codex-check`, `test`, `check`, `workpacket-verify`, `authz-policy-test`, plus MCP `review_record` |
| Product Owner | `po-candidates`, `po-candidate-get`, `po-approve`, `po-reject` | `candidate-list`, `candidate-get`, `candidate-approve`, `candidate-reject` |

The four `help-*` commands are shared discovery commands. `fmt`, `test`,
`check`, and `build` are Development-owned repository operations that QA may
rerun independently. Canonical infrastructure names remain available for
backward compatibility, while role aliases should be preferred in runbooks and
audit evidence.

Shared read commands do not transfer accountability. In particular, QA reading
a candidate does not approve it, and Product Owner approval does not replace
QA's technical validation.
