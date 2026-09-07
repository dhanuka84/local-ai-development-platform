# Real local pilot — 2026-09-06

Status: paused at Task A `validation_required`, task version 3. The generated
diff failed verification, and its local-model hunk-count correction timed out.
Candidate version 1 is pending; no passing validation report exists. Task B
reuse, human publication and live rollout have not run.

## Scope and reproducibility

- Platform revision: `e44cbb4b0eeec517d819892fb85e9fdad0ca5e45`, plus this
  worktree's pilot setup, request-format fix, bounded hunk-count recovery,
  regression tests and documentation.
- Provider: local Ollama `0.32.6`; model: `qwen3.6:35b`
  (installed digest prefix `07d35212591f`). Embeddings are configured for local
  `embeddinggemma:latest`, 768 dimensions. This is not cloud inference.
- Project: `pilot-agent-ready-20260906`; isolated database and Milvus collection:
  `pilot_agent_ready_20260906`. Fresh database migrations through `000017`.
- Synthetic source revision: `a6178ec106d9b1d4142d08ac83a62b51ad86ce43` on `main`.
  Task A adds `labels.py`; Task B will add `keys.py`. Each allows one file,
  at most 60 diff lines/12,000 patch bytes, and two bounded local checks.
- Runtime: `.local/agent-ready-pilot-20260906`; source and binaries are mounted
  read-only, state is private and writable. Generated credentials are not
  committed. The workload has only project-scoped `controller`, `development`
  and `validation_executor` roles; no human identities were provisioned.
- The pilot container runs commands as UID/GID `1000:1000`, with all
  capabilities dropped, no-new-privileges, no published ports, and only the
  internal disposable network. A public connection attempt returned network
  unreachable. The live Ollama service was temporarily attached to that network;
  no existing application or model service was restarted.

Ordered procedure: inspect authoritative knowledge and repository relationships;
prepare a private synthetic source/workload; initialize a new disposable
database/collection; run local generation; preserve exact output and disclosed
inputs; execute the unchanged bounded work packet; inspect evidence before any
accountable human publication decision; only then index and attempt Task B reuse.

## Durable evidence

Workflow: `01fa6e15-9b1b-44bb-9aef-16b621e49aae`.
Task A: `fe5156c4-37e4-4b10-897e-bcaaadab9750`.
Receipts are in the isolated PostgreSQL database; raw artifacts are under
`.local/agent-ready-pilot-20260906/state/artifacts/<first-two>/<next-two>/<sha256>`.
Use authorized `workflow_trace_get` and candidate/report tools for review.

The first generation (21:20–21:21 UTC) returned an empty final response and
failed before candidate capture. Its exact response is retained as
`97dcc6b2b24e49df0abfe2b30dad44b92bfdab337c7b7dbc04e6b77355a6b91d`.
The failed outcome remains immutable. The runner now requests an explicit
patch/summary/lesson schema with `think: false`, temperature `0.2` and a
2,048-token generation limit; these settings are in its disclosed manifest.
Empty final responses still fail closed. No raw thinking is published as a
lesson or used as a patch.

The next request (21:26–21:31 UTC) reached its five-minute HTTP timeout on the
shared local inference service. Its client operation is recorded as failed;
no complete response body was received, so no exact output artifact is claimed
for that attempt. No candidate or validation was created. A further bounded
retry uses the same workflow/task, preserving both previous failures.

The third request returned a real final patch at 21:36 UTC. Candidate
`8ccd770c-9b3c-4c57-8966-3019811abb32`, version 1, is pending. Its diff declared
seven added lines but contained six, so the verifier refused to apply it and
executed no acceptance commands. No passing validation report was produced.
Exact model response:
`08c69161d6e4ed87cbf14a66bac16b950399885dde9d406b81321a6401bf3ca8`.
Exact failed verifier result:
`dc297f7fd5b3f3facca77b4b012d823ed97c847dd9ab491af9328d99b6e51655`.
The subsequent recovery is explicitly bound to those artifacts and task version
3. It asks the local model to fix hunk counts only; source lines, the summary
and the proposed lesson cannot change. The work packet and all checks remain
mandatory. The helper has not hand-edited the model's patch into a passing one.

The correction request (21:41–21:46 UTC) reached the same five-minute HTTP
timeout; no complete response was received. Its input/disclosure and failed
client operation are retained. The shared Ollama service was observed processing
a much larger concurrent prompt and reloading its model. No existing inference
request was cancelled and no global inference settings were changed.

Authorized MCP `workflow_trace_get` returned 53 records with no missing
referenced artifacts/outcomes at inspection. This means the recorded attempt
history is inspectable, **not** that the task passed. All four model attempts
have explicit outcomes: empty final response, timeout, final patch generated,
and correction timeout. Patch generation success is separate from the failed
work-packet verification.

The exact pending lesson, hydrated through MCP, is:

> Normalization functions should be idempotent: applying the transformation
> multiple times must yield the same result as applying it once. This property
> ensures stability in data pipelines, simplifies testing (you can verify
> against normalized ground truth without worrying about input state), and
> prevents compounding errors. Always design normalization to collapse redundant
> states (e.g., multiple spaces) into a single canonical form.

Do not approve this proposal based on model generation or this document. First
obtain passing local execution evidence, then use an accountable human identity
for the exact candidate/version decision.

## Live deployment boundary

The live database was checked read-only: migration
`000007_rag_first_task_checkpoints.sql`, four knowledge candidates, all pending.
No live database migrations, candidate decisions, embedding publication, or
gateway/worker rollout have been performed by this pilot. The optional trace
collector and production-specific infrastructure remain deployment work.

`make check` passed in the local build container, including race tests and
the new private-setup tests. `make contracts-check` and `git diff --check` also
passed. The generator regression tests preserve exact
failed responses and require the exact prompt plus disclosed request settings.
Recovery tests reject stale checkpoints, cross-task evidence, source-line,
filename, hunk-position and newline changes. These checks are distinct from
real-model work-packet validation, whose six behavior tests have not yet run.

## Resume the retained runtime

Do not run the setup helper again on this directory, create replacement human
identities, or reset the existing workflow. When local inference capacity is
available, restart only the retained disposable dependencies:

```sh
docker start hybrid-ai-agent-ready-db-20260906 hybrid-ai-agent-ready-etcd-20260906 hybrid-ai-agent-ready-minio-20260906 hybrid-ai-agent-ready-milvus-20260906 hybrid-ai-agent-ready-cerbos-20260906
docker network connect --alias pilot-ollama hybrid-ai-agent-ready-test-20260906 hybrid-ai-platform-ollama-1
docker start hybrid-ai-agent-ready-pilot-20260906
```

Wait for PostgreSQL, Cerbos and Milvus readiness. If the temporary Ollama network
attachment already exists, skip that connect command. The retained runtime has
the migrated database and scoped workload already initialized. Then run:

```sh
docker exec --user 1000:1000 hybrid-ai-agent-ready-pilot-20260906 /pilot/bin/agent-ready-pilot /pilot/state/pilot.json
```

While Task A remains `local_execution`, use the original spec without explicit
workflow/task resume fields: its stable run key resolves the existing task.
For this run's corrupt-hunk failure, the retained spec now has the exact
workflow/task IDs and bound `repair_task_a` fields described in the
[runbook](agent-ready-data-operations.md). This is an explicit repair path from
`validation_required`, not an automatic regeneration loop. After a passing Task
A report and an actual human publication/checkpoint decision, the same IDs are
used for the Task B resume path; the repair field can then be removed.
The worker has not been started; pending knowledge must not be indexed to make
the demonstration pass. Human approval is not yet the active blocker if no
candidate/validation has been produced.

For authenticated MCP inspection inside the pilot container, start its retained
gateway binary as the same UID/GID; it binds only the container's loopback port:

```sh
docker exec --user 1000:1000 -d hybrid-ai-agent-ready-pilot-20260906 /pilot/bin/gateway
```

Use the protected workload token file without printing it. At handoff the
pilot, dev, PostgreSQL, Cerbos, Milvus, etcd and MinIO test containers were
stopped, and the temporary Ollama network attachment was removed. Their volumes
and private runtime were retained. The live platform and unrelated application
containers were left running.

Optional `generation_capture`: the local setup/recovery findings can be
recorded as a separate pending operational lesson using the ordered procedure,
check evidence and platform revision above. Inference provider/model:
`ollama` / `qwen3.6:35b`; explicitly retain the failed pilot outcome and do not
claim a passing real-model acceptance test. Capturing is not approval.
