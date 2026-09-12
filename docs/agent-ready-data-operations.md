# Agent-ready knowledge: operations

This runbook covers validation, quality controls, trace evidence, governed
semantic metrics, and the two-task acceptance/pilot implementation. The retained
real-Ollama pilot completed on September 10, including explicit Task A approval
and locally verified Task B reuse. Metric definitions still need their actual
validated decisions under the user's standing task authorization; live rollout
awaits vault unlock. See [autonomous local operation](operations.md#autonomous-local-operation).
Synthetic test
approvals are not human approval of real knowledge. See
[the design](agent-ready-data-plan.md) and the
[real-pilot receipt](agent-ready-pilot-20260906.md).
The [September 10 recovery receipt](agent-ready-pilot-20260910.md) and
[gap checklist](agent-ready-gap-checklist.md) carry the current status.

For full regression validation, run `make agent-ready-e2e`. The
[E2E guide](agent-ready-e2e.md) maps every checklist row to exact tests, records
the required manual/deployment evidence, and explains the retained JSON test
receipts. The suite uses disposable services and deterministic local protocol
fixtures; it does not require the live vault or publish real knowledge.

## Upgrade behavior

Apply migrations `000008` through `000018` with the usual migration command,
then rebuild gateway and worker together. Test the upgrade in a disposable
database first. These changes have not been applied to the live platform during
development. The live Cerbos profile bind-mounts and watches `policies/cerbos`,
so editing policies can reload them even without rebuilding application images.
The live database was confirmed to remain on migration `000007` at handoff.
The [release preparation](agent-ready-release-20260910.md) records a successful
restore and upgrade of its actual PostgreSQL/AGE backup, plus compatible image
builds and the remaining credential prerequisite. The new
[task delegation](task-delegation.md) boundary is included in migration 18.

## Candidate text inspection

Before reviewing a candidate, call `knowledge_candidate_quality` with its
`knowledge_id` and `expected_version`. It returns content/version digests,
suspicious-character and possible-truncation flags, and possible duplicates
from up to 100 eligible authoritative PostgreSQL records. Comparison is limited
to 64 KiB per text and uses token-bigram Jaccard similarity with threshold 0.85.
The response identifies its bounded coverage. It does not search all historic
or quarantined content, and a high score can include opposite meanings.

Hydrate a reported duplicate with `knowledge_get` and inspect its exact version,
source and intended applicability. Findings do not merge records, execute
validation or grant publication. Missing flags are not proof of quality. Existing
exact-duplicate, source, validation and projection gates still control eligibility.
The endpoint is intentionally denied to task delegates until explicitly added
to their reviewed capability allowlist.

`AUTO_APPROVE_LOCAL=true` is now a configuration error. Capture always creates
pending knowledge; local model output is not approval. Existing approved items
without version-bound validation and source evidence remain historically
approved but are withheld from normal retrieval. Migrations do not invent
approvers, validations or source checks for them.

Milvus rows without `version`, `content_sha256` and `projection_sha256` metadata cannot satisfy vector
read-back. After reviewing and validating eligible knowledge, `make reindex`
queues fresh projections. It now queues only currently eligible items. The
worker verifies the exact ID, project, version and retrieval-text digest using
a strong-consistency primary-key query before recording projection success.

## Validate and decide

An authenticated human with the project's `qa` role can use
`knowledge_validation_record`. Supply `knowledge_id`, `expected_version`,
`source_manifest`, and acceptance `criteria`. The gateway derives the actor,
receipt times, report ID, digest and verdict. This is a human attestation; it
cannot claim command execution. A workload principal cannot use it to certify
itself.

For executed patch checks, run the local QA CLI in an environment containing
Go, Git and the exact tools declared by the work packet:

```sh
go run ./cmd/admin validate validation-input.json work-packet.json candidate.patch
```

Use the normal protected environment/credential files; do not place tokens in
JSON or command arguments. This CLI authenticates the same human QA role and
uses the same authorization policy as MCP. Executed verification also accepts
an explicitly provisioned workload with `validation_executor`; it cannot upload
a human attestation or approve anything. The minimal admin container is not
a general-purpose verifier environment.

The work packet must use `hybrid-ai/work-packet/v1`, be local-only, declare at
least one bounded check, and bind its workspace/base revision to an allowlisted
repository source. The verifier executes in a disposable checkout and stores
actual command outputs as content-addressed artifacts. No cloud review is
invoked. The QA environment must deny unapproved egress; the verifier does not
provide an OS/network sandbox by itself.
The verifier pins `PYTHONDONTWRITEBYTECODE=1` so Python imports do not write
bytecode into the checked-out source; all other untracked-file checks remain
mandatory.

Repository sources require a full commit ID and named local branch. An optional
`applicable_through_revision` explicitly bounds applicability: the current local
branch must descend from `revision` and be an ancestor of the upper bound. No
open-ended range is inferred, and changing bounds requires fresh validation and
approval. Task use also verifies the exact current target revision. Use source
paths consistently across the QA environment, gateway and worker; configure
`CODEGRAPH_ALLOWED_ROOTS` accordingly. Compose mounts the selected host root at
`/workspace:ro` for both gateway and worker. The worker image includes Git.
The Kubernetes profile mounts artifacts but requires an operator-defined
read-only repository snapshot mount for repository sources. Missing mounts or
Git refs fail closed; no network fetch is performed.

Document, procedure, patch and work-packet sources require existing local
artifact SHA-256 digests. References are not URLs to fetch automatically. Reads
verify stored bytes and currently have an 8 MiB per-artifact integrity limit.

After reviewing the exact report, an authenticated human Product Owner uses
`knowledge_candidate_decide` with:

```json
{
  "knowledge_id": "<candidate UUID>",
  "expected_version": 1,
  "validation_id": "<validation UUID>",
  "decision": "approve",
  "reason": "<accountable acceptance rationale>",
  "idempotency_key": "<stable key for this exact decision>"
}
```

Equivalent CLI arguments are:

```sh
go run ./cmd/admin approve CANDIDATE_ID VERSION VALIDATION_ID DECISION_KEY REASON
go run ./cmd/admin reject CANDIDATE_ID VERSION - DECISION_KEY REASON
```

For Make aliases, approval requires `ID`, `VERSION`, `VALIDATION_ID`,
`DECISION_KEY`, and `REASON`; rejection omits `VALIDATION_ID`. Use an environment
with the operator's normal authentication. Reusing the same key with different
decision input conflicts. Legacy actor-only decisions are no longer supported.
Regulated/configured separation rules still require distinct people. A pending
candidate linked to a workflow must also satisfy its managed promotion gate.

## Freshness and recovery

The checked-in default policy is owned by `platform-maintainers`: daily source
and projection checks, a maximum 30-day manual validation age, and a maximum
24-hour work-packet validation age. A report's earlier expiry also applies.
Install project overrides as new rows in `data_quality_policies` with a named
owner, increasing version and effective time; policy history is append-only.
Policies additionally select `data_product` and `purpose`, with project and
more-specific scopes taking precedence over wildcard defaults. Tasks select
their purpose server-side. `software_knowledge` / `code_change` defaults require
a work-packet report no older than one day. Exact duplicate content in the same
project is rejected at publication; use the existing canonical lesson.

Source reconciliation reads the actual named local branch and rehashes source,
validation and command artifacts. An unchanged, successfully checked source
can refresh the source clock. A failed attempt is recorded separately and
cannot move the successful-verification clock. Neither operation extends
content validation. Projection reconciliation independently reads Milvus.

`knowledge_quality_reviews` lists approved but currently withheld knowledge in
the authorized project. `quality_blocked` is distinct from an ordinary empty
search; workflow activation fails closed instead of choosing the cloud-review
miss route. SQL fallback and graph traversal cannot restore stale content.
AGE uses a conservative PostgreSQL fallback when its project projection
contains ineligible knowledge, including potentially hidden intermediate nodes.

Correct a missing source mount or restore the referenced immutable artifact,
then allow the worker to verify it. Changed applicability or expired content
requires a new QA report and an explicit Product Owner decision. Revalidation
of unchanged approved content preserves old reports and decisions. Approved
content cannot be edited in place; capture a corrected pending candidate.

Outbox processing stops after ten attempted executions. A crash after the last
claim is reclaimed only to mark exhaustion, not to run the effect again.
Operators can inspect `outbox_events.failed_at`, attempts and last error. After
fixing the cause, `make reindex` creates new intents for eligible knowledge;
the failed rows remain as history. Automatic exactly-once execution is not
claimed. `knowledge_index_failures` exposes scoped exhausted knowledge intents.
An authenticated human Operations actor uses `knowledge_index_retry` with
`project_id`, exact `event_id`, `expected_attempts`, `reason`, and
`idempotency_key`. A retry creates one replacement intent and retains the failed
row and immutable retry decision. Reusing a key with different input conflicts.
`make reindex` also rebuilds approved, current-registry semantic definitions.
The worker independently rechecks their exact Milvus projection every six hours;
failed checks never extend their validation or verified-projection clocks.

## Traces and local export

`workflow_trace_get` requires authorization to the workflow's actual project.
It returns chronological sanitized receipts, exact artifact references, and an
explicit list of missing outcomes/artifacts. A completed task without trusted
local execution evidence is incomplete, not silently successful. Raw outputs,
prompts, and disclosed-context manifests remain in the local content-addressed
store; the trace exporter does not include them. Arbitrary client-side calls
cannot be observed and must supply correlated execution evidence.

Set `TRACE_EXPORT_ENDPOINT=http://otel-collector:4318/v1/traces` only when a
local collector is available. Loopback HTTP is also accepted; public/cloud
endpoints and redirects are rejected. PostgreSQL owns the export queue; HTTP
errors and OTLP partial rejection leave receipts retryable. The optional
[collector configuration](../deploy/otel-collector/config.yaml) is an operator
template, not an automatically deployed service. Keep its port private.

`platform_evidence_health` is Operations-only: missing outcomes older than five
minutes, repeated export failures, unexported receipts, and retention reviews.
`TRACE_RETENTION_DAYS` defaults to 30 (valid 1–3650). This triggers review/hold;
it does not erase immutable evidence. Choose archival and deletion controls
separately for the deployment. An unknown original outcome is never rewritten
as success just because a later retry or projection check succeeds.

The opt-in `deploy/compose/compose.telemetry.yaml` overlay connects gateway and
worker to a collector on an internal trace network without a published port.
It uses the pinned [Collector 0.160.0 release](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.160.0).
Render the combined configuration before a coordinated application rollout:

```sh
docker compose --env-file .env.example -f deploy/compose/compose.yaml -f deploy/compose/compose.telemetry.yaml config --quiet
```

The disposable `make agent-ready-integration` profile exercises the actual
receiver with synthetic receipts. The production overlay uses the same
configuration, but deploying it to the live platform remains a separate rollout
item. Its debug exporter is an operational view, not an archival backend.

## Governed semantic context and metrics

`context://registry/v1` publishes source-controlled definitions for inspection;
it does not authorize execution. Human QA runs `context_registry_validate` for
the project. The server executes contract fixtures and PostgreSQL query
preparation, stores evidence, and stages six pending definitions. A human
Product Owner then calls `context_definition_decide` with project, definition
ID, expected version/hash, validation ID, reason and idempotency key. No model
can approve the registry. Revalidation checks contracts/SQL preparation; it
does not claim an integration test just ran.

Approved definitions are embedded locally through Ollama and indexed in Milvus.
`context_definition_search` treats hits as candidates and hydrates current,
approved PostgreSQL definitions with matching registry and projection metadata.
Model, dimension, version, hash and freshness mismatches are withheld.

`platform_metric_query` accepts `project_id`, `metric_id`, `version: 1`, `start`
and `end` (half-open UTC interval, maximum 366 days), and optional
`dimensions: {"task_type": "maintenance"}`. It only executes the four fixed,
reviewed SQL statements in a repeatable-read snapshot, never model-generated SQL.
Definitions expire after 30 days without renewed validation. Empty rate
denominators return null. `pending_index_age_seconds` is a current snapshot and
ignores the window for its backlog; it accepts no dimensions.

Example meaning: `validated_reuse_rate` with numerator 8, denominator 20 and
value `0.4` means 40% of completed locally validated tasks explicitly recorded
eligible context use. It is not a count of search hits or a causal improvement
claim. The deterministic suite verifies that arithmetic using synthetic data.

During local execution, call `workflow_task_context_record` with exact knowledge
IDs/versions and target repository, branch and revision. Before completing a
task, `VALIDATION_PASSED` and `VALIDATED_REUSE_COMPLETED` require
`payload.validation_id` referring to a passing trusted local work-packet report.
`VALIDATED_REUSE_COMPLETED` rechecks context eligibility and completes reuse
without publishing the task's newly generated candidate.

## Local pilot: start, approve, resume

For the local disposable dependency profile, prepare a fresh private runtime:

```sh
python3 scripts/agent_ready_pilot_prepare.py .local/agent-ready-pilot-20260906
```

This refuses to overwrite an existing runtime. It creates six-behavior Python
fixtures for each task, a standalone synthetic Git source, full bounded work
packets, and a project-scoped non-human workload credential in mode-0600 files.
The fixture source intentionally has tests but no implementation. The generated
environment uses the dated disposable PostgreSQL/Milvus/Cerbos container names
and `pilot-ollama` alias; it is not a general deployment configuration. Review
those endpoints for another environment. The helper does not create databases,
start containers, invoke models, provision human identities, or approve anything.
Keep the generated environment, token and principal files out of logs and Git.

Initialize a disposable PostgreSQL/Milvus/Cerbos deployment, an allowlisted
synthetic Git repository and a private artifact directory. Never use production
data or an unrestricted repository prompt. Provision a real workload principal
with project-scoped `controller`, `development`, and `validation_executor`
roles, but **no** human QA or Product Owner roles. Supply its existing token
through protected environment configuration. The runner does not bootstrap
identities or apply migrations. A separate real human uses their own identity
for the publication decision and checkpoint.

The reviewed pilot JSON contains `project_id` beginning `pilot-`, `run_key`,
an explicit Ollama `model` tag, `branch`, and `task_a` / `task_b` full
`hybrid-ai/work-packet/v1` objects. Both packets must be local-only patch tasks
with bounded checks and allowlisted files. Use a stable base revision for this
two-task demonstration. The runner never modifies the source checkout: it
validates each generated patch in a disposable clone. Configure a `pilot_`
Milvus collection, Cerbos authorization, and a loopback/private Ollama URL.
The Ollama request uses an explicit patch/summary/lesson JSON schema and disables
separate thinking output. Both settings and generation limits are retained in
the disclosed-context manifest. An empty final response remains a failed
attempt with its exact raw response preserved; it is never substituted with
thinking text. These request fields follow the
[local generate API](https://docs.ollama.com/api/generate).

```sh
AGENT_READY_PILOT_ISOLATED=true make agent-ready-pilot PILOT_SPEC=pilot.json
```

Task A captures exact Ollama output and its disclosed-input manifest, executes
the patch verifier, and emits `awaiting_human_approval` with workflow/task,
candidate/version and validation IDs. A human reviews the evidence, approves
that exact candidate using `knowledge_candidate_decide`, and records the
`LEARNING_PROMOTED` task event. Run the local worker to verify indexing. Add the
returned `workflow_id` and `task_a_id` to the same pilot spec and rerun.

Resume verifies Task A's Milvus read-back, retrieves its lesson for Task B,
records actual context use, invokes the local model, executes validation, and
completes Task B without auto-publishing its candidate. The CAS report contains
provider/model, base revision and correlated trace. Failed/interrupted phases
retain evidence and require explicit checkpoint recovery; blindly regenerating
over an existing non-local-execution task is refused. Registry approvals and
metric queries remain separate human-governed steps.

For the narrow case of a corrupt Task A diff hunk count, an operator can supply
`workflow_id`, `task_a_id`, and `repair_task_a` in the reviewed spec. Its fields
are `expected_task_version`, `knowledge_id`, `expected_version`,
`model_output_sha256`, and `verifier_result_sha256`. The runner checks the exact
pending checkpoint/candidate and requires both artifacts in that task's durable
trace. The local model may change only hunk counts: the source lines, file
headers, hunk positions, summary and lesson must remain unchanged. All packet
checks still execute in a disposable clone. Original failures and correction
output remain separate immutable evidence. This does not revise the lesson or
approve it; other failure classes require their normal explicit recovery.

For a syntactically corrupt hunk count, `repair_task_a.method: "recount"` selects
the deterministic `hunk-recount-v1` recovery. This option counts actual context,
added and removed lines, preserves all other bytes, and refuses malformed hunk
content. The original exact model output remains unchanged; a separate
`pilot.patch_repair.derived` artifact identifies the method, input digests and
derived patch. It still requires the same checkpoint, candidate/version, bound
corrupt-patch evidence and every original verifier check. The default method
(`local_model`) retains model-assisted correction. Both routes record a durable
repair outcome; neither publishes knowledge.

For a failed Task B that needs a new patch, `revise_task_b` takes the same exact
checkpoint/candidate/version/output/verifier binding fields, with method
`local_model`. It accepts a failed bound verifier receipt, generates locally
from the current task and approved general lesson, executes a work-packet
precheck, records `VALIDATION_FAILED`, revises the pending candidate with an
optimistic version check, and records `LOCAL_REVISION_RECORDED`. It then runs
the normal verifier against the new version before `VALIDATED_REUSE_COMPLETED`.
The old output and failed checks stay immutable. A failed precheck leaves the
candidate unchanged. The new lesson remains pending even when the task completes.

Local HTTP fixture tests separately verify exact output/disclosure capture and
failed-response preservation. Those tests do not prove model quality or
substitute for the actual human approval checkpoint.

## Verification

Run `make check`, `make contracts-check`, and `make authz-policy-test`.
For a complete disposable environment, run `make agent-ready-integration`.
It builds a test image with dependencies, starts uniquely named PostgreSQL/AGE,
Milvus, etcd, MinIO and Cerbos containers on a private network without published
ports, waits for readiness, and runs all adapter suites plus the two-task
acceptance. It cleans up only its own containers and data on exit. No local
runtime credentials, live database, model provider or retained pilot is used.
CI runs this same command. Initial image/dependency downloads need network
access; test execution uses the private network.

Adapter integration suites require explicitly disposable dependencies:

```sh
TEST_DATABASE_URL=... go test -race -count=1 ./internal/postgres
TEST_AGE_DATABASE_URL=... go test -count=1 ./internal/age
TEST_MILVUS_ADDRESS=... go test -count=1 ./internal/milvus
TEST_DATABASE_URL=... TEST_MILVUS_ADDRESS=... TEST_CERBOS_ADDRESS=... make agent-ready-acceptance
```

Never point these variables at the live platform. Existing PostgreSQL/AGE
integration fixtures truncate test tables. The Milvus adapter test creates and
drops only its uniquely named synthetic collection; the acceptance scenario
retains its fixture collection inside the disposable Milvus deployment. These fixtures use synthetic
principals and no model calls; they do not approve reusable real-world knowledge
or prove the pending Ollama acceptance pilot.
