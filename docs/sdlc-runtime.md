# Running the AI-native SDLC platform

[Expectations and scope](ai-native-sdlc-expectations.md) ·
[Lifecycle mapping](sdlc-guide.md) · [Completion evidence](sdlc-completion-checklist.md)

The platform executes accepted product intent through separately authenticated
local workers. PostgreSQL owns the run, current KB records, grants, leases,
package decisions and audit evidence. AGE and Milvus supply structural and
semantic discovery; retrieved candidates are hydrated and reauthorized before
use. A model proposes work. Independent checks determine whether it succeeded.

The supported functional profiles are feature delivery to a Gitea repository,
immutable artifact store and filesystem staging target, and incident diagnosis
using Kafka, S3, PostgreSQL audit views, Loki and Prometheus followed by a fixed
HTTP remedy and independent technical/business verification. These adapters
provide concrete examples of the broader scope. Live production cutover and
additional vendor integrations have separate acceptance requirements.

## Execution and responsibility

| Stage | Workload role | Responsibility and evidence |
|---|---|---|
| `build` | `sdlc_builder` | Use accepted BRS, exact source and both KB dimensions; propose a criteria-linked plan and bounded files/patch using a pinned local model |
| `evaluate` | `sdlc_evaluator` | Execute the protected packet in a separate pinned container; retain exact verifier output; reject or return the candidate for bounded repair |
| `deliver` | `sdlc_delivery` | Publish the exact commit and PR, store the immutable archive, select the staging release, and read back each effect |
| `verify_delivery` | `sdlc_evaluator` | Fetch the published commit, compare the staged archive, rerun protected tests, and publish/read back the CI result |
| `diagnose` | `sdlc_diagnosis` | Collect complete scoped observations, evaluate BRS criteria, compare at least two explanations and cite support/contradictions |
| `remediate` | `sdlc_remediation` | Execute only the selected operator-configured remedy with state preconditions and a stable action ID |
| `verify_recovery` | `sdlc_evaluator` | Read technical state independently and verify every business criterion using a new post-remedy observation window |

One authenticated human with Operations authority owns and grants each run.
The default local operator also holds Development, QA and Product Owner roles;
these do not transfer to its workers. All five workload identities must be
distinct and carry their assigned role. QA plus Operations controls package
activation. Product acceptance uses the existing QA/PO validation and exact
version decision. Generated KB entries require explicit user approval and
remain pending after a successful run.

The gateway exposes 60 tools when local indexing is enabled, including 17 SDLC
execution/package tools. Without local indexing it exposes 59. Older deployed
images can expose fewer tools; discover the installed surface before use.

## Configure an owned target

Run `make build` for the operator CLI, workers and source adapter. Build the
isolated evaluator with the `sdlc-evaluator` Docker target and record its image
ID. Supply Python/Go or other required offline test dependencies in your
reviewed evaluator image before pinning it; execution never pulls an image.

[Example configuration](../examples/agents/registry.example.json) includes five
role packages and one disposable qualification target. Copy it to an owned
configuration directory, replace the model name, model digest and evaluator
image digest, and set the actual repository, principal IDs and budget. The
all-zero example pins deliberately cannot authorize inference or evaluation.
Use the installed local Ollama inventory to select the exact model digest.

Set `SDLC_RUNTIME_REGISTRY` for a directly launched gateway. Compose mounts an
empty registry by default; set `SDLC_RUNTIME_REGISTRY_HOST_PATH` to the reviewed
host file. Configure workload principals using the platform's existing
credential bootstrap. Each worker gets only its own private token file, with
permissions `0600` or stricter. Keep worker secrets out of the repository,
source checkout, model input and evaluator container.

Use the matching worker templates:

- [Builder](../examples/agents/sdlc_builder.example.json)
- [Independent evaluator](../examples/agents/sdlc_evaluator.example.json)
- [Delivery](../examples/agents/sdlc_delivery.example.json)
- [Diagnosis](../examples/agents/sdlc_diagnosis.example.json)
- [Remediation](../examples/agents/sdlc_remediation.example.json)

Only the trusted evaluator process needs the local Docker daemon. Its child
container receives a credential-free shallow source snapshot, a read-only
input/root, no network, no socket, no capabilities, an unprivileged UID, and
CPU, memory, process, storage and time limits. Builders receive only committed
allowlisted text at the accepted revision. The worker can construct a Git patch
from model-proposed complete file contents; the gateway independently verifies
that conversion against the disclosed base files. It never repairs the model's
proposal silently after evaluation.

Effect contracts bind the forge origin/repository/base branch and absolute
artifact/staging roots. Generate their digests without connecting or reading
credentials:

```sh
./bin/sdlc-worker --config .local/sdlc/delivery.json --contract-digests
```

Copy the reviewed `delivery_sha256` and `remediation_sha256` into the target.
Credential file paths are excluded from these effect digests so publisher and
read-only probe credentials can differ. A staging replacement also requires
`expected_release_sha256`, matching the previous `current.json` bytes. This
compare-and-swap contract permits an explicitly configured replacement or
restore while preserving previous content-addressed releases.

## Submit and steer feature work

Create and accept the product/BRS and protected work-packet records using the
[product KB procedures](product-knowledge-and-evaluated-sources.md). The packet
binds an exact Git commit, allowed and protected files, offline checks and
change limits. A feature intent binds those exact records and names each
criterion's packet digest. Every criterion must be independently testable.
Changing an accepted requirement, packet, code head or runtime contract stops
old execution; create a new accepted version and run for the changed scope.

`draft` takes an intake JSON file with `product_id`, `key`, `expected_version`,
`goal`, exact `bindings`, `answers`, a pinned local `package`, `ollama_url` and
`evidence_directory`. It returns missing decisions or a pending intent. Save
answers and call `draft` again, or use `clarify` with a versioned intent record.
It preserves exact model request, response and disclosure manifest. Inspect
the exact pending version before `review`; supply its ID, SHA256, validation
`evidence` and accountable `reason`. `review` is a product-record decision,
not a generated-knowledge publication command.

An accepted submission file contains:

```json
{
  "project_id": "example-project",
  "target_id": "orders-disposable",
  "kind": "feature",
  "intent_id": "EXACT_ACCEPTED_INTENT_ID",
  "expected_sha256": "EXACT_ACCEPTED_INTENT_SHA256",
  "idempotency_key": "orders-feature-001"
}
```

For example, after replacing the example IDs and configuring the private
operator token:

```sh
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token submit .local/sdlc/submission.json
./bin/sdlc-worker --config .local/sdlc/builder.json
```

Run the separately configured evaluator and delivery workers as supervised
processes as well. Without `--run`, each polls assigned eligible work. With
`--run RUN_ID`, a worker attempts one assigned stage. Use these operator commands
with an actual returned run ID:

```sh
run_id=REPLACE_WITH_RETURNED_RUN_ID
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token status "$run_id"
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token watch "$run_id"
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token pause "$run_id" 'Investigating changed requirements'
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token resume "$run_id" 'Original accepted context remains valid'
./bin/sdlc --project example-project --token-file .local/sdlc/operator.token evidence "$run_id" .local/sdlc/export
```

`status` includes blockers, owner/actors, stages, actual token use, conservative
reserved tokens, attempts, source query/row charges, deadline and exact evidence
references. `trace RUN_ID` pages through a stable snapshot of correlated audit
records; `evidence` also exports this trace alongside exact artifacts. `list`
shows owned/assigned runs. Retries reuse the request identity;
changed arguments under the same identity fail. Expired worker leases are
fenced. Repair attempts consume the original total budget.

`cancel` stops further authorized writes. An interrupted delivery/remedy remains
`reconciling` until its external effects are observed. Cancellation is not a
rollback. An explicit `reconcile RUN_ID REASON` decision by the current owner
can grant five minutes of read-back after an expired run grant. It fences the
old lease and cannot resume writes. Unavailable or incomplete effects remain
blocked. If the accepted context or configured target itself has changed,
restore the exact original read-back contract or handle the exception under a
separately reviewed target recovery procedure.

## Connect operational evidence

[The native adapter template](../examples/agents/source-adapter.example.json)
and [gateway registry](../examples/agents/source-registry.example.json) show
all five source kinds. Adjust reviewed source views and fields, compute backend
digests, then copy the exact digests into both contracts:

```sh
./bin/source-adapter --config .local/sdlc/sources.json --contract-digests
./bin/source-adapter --config .local/sdlc/sources.json
```

The adapter requires TLS except on a literal loopback listener. Gateway source
configuration uses the existing `PRODUCT_SOURCE_REGISTRY` setting. The example
loopback URL assumes gateway and adapter share the host network; a containerized
gateway needs the reviewed TLS origin reachable from its own network. MCP
worker connections similarly require HTTPS except on literal loopback.

| Native backend | Required contract and completeness evidence |
|---|---|
| Kafka | Fixed brokers/topic/partitions, bounded scans and private offsets. Producers emit `hybrid-ai-watermark` barrier headers. The reader never joins, resets or commits an application consumer group. Missing partition coverage remains partial. |
| S3-compatible lake | Fixed bucket/object, version/ETag read-back and a JSON envelope with dataset revision, watermark and rows. A late snapshot cannot prove current completeness. |
| PostgreSQL audit | A private DSN for a read-only user, fixed data and watermark views, a read-only repeatable-read transaction and bounded parameters. Arbitrary SQL is absent. |
| Loki | Fixed LogQL and expected stream identities, structured event rows and per-stream `_watermark` markers. Missing stream barriers remain partial. |
| Prometheus | Fixed range query plus a reviewed producer-watermark query; finite numeric samples, warnings and scan bounds are checked. Empty/NaN results cannot stand for zero or recovery. |
| Existing MCP source | A fixed tool advertised as read-only must return the same bounded, typed source envelope. Tool discovery alone is insufficient evidence. |

Every returned row includes `event_id` and `event_time`. The adapter validates
schema, permitted fields, window, revision and completeness before ingestion.
Observations have immutable receipts, retention and current access checks.
They can enter the product KB under the source policy; generated causal
interpretations remain separate. Cumulative run query/row budgets are reserved
before reads, including failed reads. Exact retries reuse their reservation.

Incident intents use `kind: incident` and BRS-bound
`observation_reconciliation` criteria. The native proof compares Kafka input
with processed audit and lake records, while logs and metrics constrain causal
explanations. A delayed lake blocks diagnosis. A historical database-outage
explanation is contradicted by current evidence. A failed remedy blocks the
run. Completion requires independently observed technical state and both
business criteria in a new window.

The remedy adapter supports fixed JSON GET/PUT state, strong SHA256 ETags,
`If-Match`, a stable `Idempotency-Key`, and `X-SDLC-Action` read-back. Supply a
separate evaluator credential restricted to GET at the target. MCP roles do
not replace native source/forge/operational permissions.

## Knowledge and agent improvement

After completion, `feedback RUN_ID` creates pending requirement, regression
test, runbook, package and generated-KB proposals. Repeated calls reuse the exact
proposal set. A completed run does not approve or embed them.

Packages use distinct IDs per version and freeze local model/image identity,
instructions, capabilities, input/output limits, timeout, concurrency and
regression identity. A disposable target with `qualification: true` permits
canary execution before activation. Regular targets require an active exact
package for every participating role. Qualification is forbidden in staging.

Use `package-evaluate FILE` with `project_id`, `package_id`,
`expected_sha256` and real qualification `run_ids`. The service derives positive
and negative results from those immutable runs; a caller cannot submit a
self-declared pass. `package-activate FILE` takes `project_id`, `target_id`,
`package_id`, `evaluation_id`, `expected_active_sha256`, `action` (`activate` or
`rollback`) and `reason`. Rollback requires a previously activated exact
package and its passed campaign. Update the operator registry to that package
and use the previous active digest as the concurrency precondition.
`package-status TARGET ROLE` returns the configured and active versions.

Fixture campaigns prove control behavior. Staging model-package activation
requires local-model evidence. A successful synthetic local-model task remains
a narrow trial; representative quality, latency, cost and adoption claims need
a defined cohort and observation period.

## Validate the implementation

`make check` runs formatting, vet, race and script checks.
`make authz-policy-test` checks the Cerbos boundaries. `make agent-ready-functional`
runs the existing 28-item acceptance suite followed by the A01–A12 native-service
suite. Docker resources, test repositories and approval decisions are disposable;
the live installation is unchanged.

To add an actual installed local model trial:

```sh
SDLC_LOCAL_MODEL=qwen3.6:35b SDLC_LOCAL_OLLAMA_URL=http://127.0.0.1:11434 make agent-ready-functional
```

Use your installed model name and explicit local origin. There is no cloud
fallback. `.local/sdlc-e2e/run-*/` retains the exact source manifest, pinned
evaluator image, per-test results, correlated run/model/effect artifacts,
backup/restore proof, scope coverage and separate fixture/local-model outcome
measures. The [completion checklist](sdlc-completion-checklist.md) names the
executed evidence and remaining target acceptance inputs.
