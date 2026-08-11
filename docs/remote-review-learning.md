# Remote Review and Local Learning

## Outcome

The governed path is a FIFO queue of atomic tasks. When a task reaches the
head, it searches approved RAG first. A strong match guides local Ollama work
without another cloud call. An allowed RAG miss requires a read-only Codex
cloud review, followed by an Ollama revision. Maintenance, confidential, and
restricted work uses the local-only miss route. Every route converges on local
validation, accountable approval, and a Milvus read-back before the next task
is activated.

The platform does **not** put raw reviewer output into Milvus. It saves the exact
response as evidence. A recommendation becomes searchable knowledge only after
someone applies it locally, runs checks, rewrites it as a reusable lesson, and
explicitly approves it.

![RAG-first task queue, review, and learning loop](diagrams/hybrid-ai-review-learning-loop.png)

The diagram's
editable source is retained at
`diagrams/hybrid-ai-review-learning-loop.mmd`.

## Responsibility boundaries

| Component | Owns | Must not own |
|---|---|---|
| OpenClaw | FIFO task submission, model invocation, context minimization, and queue coordination. | Knowledge approval or vector truth. |
| Ollama worker | Local implementation, local regeneration, and maintenance inference. | Cloud escalation policy. |
| Work-packet verifier | Deterministic scope, patch, and validation enforcement in a disposable clone. | Model invocation or human approval. |
| Codex CLI | Read-only repository/diff review of a sanitized package after an allowed RAG miss. | Repository writes, candidate revision, direct knowledge promotion, or local-only maintenance. |
| Kimi | Architecture, design, optimization, and long-context advisory review. | Direct knowledge promotion. |
| MCP gateway | Typed retrieval, capture, evidence, review, approval, and graph operations. | Autonomous task routing. |
| PostgreSQL | Canonical workflow, provenance, review decisions, graph edges, stable IDs, and outbox. | Approximate semantic ranking. |
| Artifact CAS | Exact prompt, output, raw review, and context-manifest bytes by SHA-256. | Approval state. |
| Milvus | Semantic discovery of approved knowledge and selected graph projections. | Canonical records or pending reviewer prose. |

## Development workflow

1. OpenClaw calls `workflow_task_begin`. PostgreSQL accepts the task into its
   FIFO queue; active work does not cause rejection.
2. When no earlier task is active, the controller emits `TASK_ACTIVATED`. The
   service performs `knowledge_search` at this point and stores the exact
   result as an immutable artifact.
3. A strong Milvus hit selects `rag_hit`. A miss selects either
   `rag_miss_cloud_review` for policy-allowed development or
   `rag_miss_local_only` for maintenance or protected data.
4. Ollama produces the local result and `generation_capture` creates a pending
   candidate. `LOCAL_RESULT_RECORDED` accepts only `provider=ollama`.
5. A strong RAG hit or local-only miss moves directly to local validation. An
   allowed miss enters `cloud_review_required` and cannot silently fall back.
   Local-model tasks default to `execution_mode=auto`, so no human acceptance
   is requested before this review. An explicitly manual task pauses at
   `review_approval_required` instead.
6. Codex receives only a sanitized package and runs in a read-only sandbox
   against a read-only repository mount. It returns findings, not edits.
7. `review_record` stores reviewer provenance and decision in PostgreSQL. The
   exact reviewer response and sanitized context manifest are written to the
   artifact CAS and referenced by SHA-256. The cloud record uses a non-mutating
   verdict such as `comment`. `CLOUD_REVIEW_RECORDED` verifies the hashes
   against that same candidate, workflow, provider, and model before the task
   can enter local revision.
8. Ollama accepts or rejects each finding against current source, applies the
   accepted changes, reruns checks, and records `revise` with fresh local
   validation. Only `provider=ollama` may revise pending content.
9. The improved, generalized result stays a pending knowledge candidate until
   an accountable actor uses `knowledge_candidate_decide`.
10. Approval and a `knowledge.upsert` outbox event commit atomically in
    PostgreSQL. The worker embeds the authoritative approved item with Ollama
    and upserts it into Milvus under the PostgreSQL UUID.
11. `RAG_READBACK_VERIFIED` succeeds only when `knowledge_search` returns that
    UUID from the Milvus backend. The checkpoint completes and automatically
    activates the next queued task.

## Direct cloud sessions are outside the governed hybrid lane

The platform still exposes general `make codex` commands for explicitly chosen
cloud work, but those sessions are not evidence that the RAG-first hybrid loop
ran. A governed hybrid task must show the checkpoint sequence and uses Codex
only for read-only review. Running the CLI locally does not make Codex inference
local; every supplied prompt, diff, snippet, or MCP result is a disclosure to
OpenAI.

## Maintenance workflow

Maintenance uses the same approved knowledge and exact graphs but stops before
the cloud-review branch:

```text
maintenance request
  -> OpenClaw maintenance policy
  -> approved local knowledge + current system evidence
  -> local Ollama diagnosis/action proposal
  -> local verification and operator approval
  -> optional pending capture
```

The maintenance work packet must set `local_only=true` and
`cloud_review=false`. The maintenance OpenClaw identity has no cloud model
fallback or cloud credential. For a hard compliance boundary, also run it with
network egress denied.

## Evidence is not knowledge

The three states intentionally serve different purposes:

| State | Stored in | Retrieval behavior |
|---|---|---|
| Raw reviewer response and export manifest | Artifact CAS, referenced by PostgreSQL review row | Audit/evidence only; never automatically embedded. |
| Reviewer recommendation or revised solution | Pending PostgreSQL candidate | Visible only to review/approval workflows. |
| Locally validated, generalized, approved solution | PostgreSQL plus Milvus projection | Available to future local development and maintenance retrieval. |

This separation prevents a persuasive but incorrect reviewer response from
becoming self-reinforcing model memory.

## Recording a remote review through MCP

Call `review_record` after local verification. `raw_output` should contain the
unmodified model response. `comments` is the concise normalized finding set.
When `raw_output` is omitted, `comments` is preserved as the review artifact.
`context_manifest` is a JSON document passed as a string so its exact disclosed
form can be hashed and retained.

```json
{
  "knowledge_id": "7e126a91-5e76-4bb4-82ea-858004d5735d",
  "reviewer": "codex-review",
  "provider": "openai",
  "model": "organization-approved-codex-model",
  "verdict": "comment",
  "comments": "One correctness issue and one reusable transaction rule.",
  "raw_output": "Exact reviewer response, unchanged.",
  "context_manifest": "{\"schema\":\"hybrid-ai/review-context/v1\",\"revision\":\"abc123\",\"files\":[{\"path\":\"internal/service/service.go\",\"sha256\":\"...\"}],\"checks\":[\"go test ./internal/service\"],\"data_classification\":\"internal\"}"
}
```

The result returns the SHA-256 and URI for each created artifact. The cloud
review remains a `comment`. After Ollama applies accepted findings and reruns
checks, a second `review_record` with `provider=ollama`, verdict `revise`,
`improved_content`, and fresh `validation_evidence` may update the pending
candidate. Neither record approves it.

## Designing reusable improvements

A promoted item should capture the method that made the result reliable, not a
transcript-specific answer. Include:

- normalized problem and applicability conditions;
- repository or architecture constraints;
- ordered procedure and important decisions;
- a compact exemplar where useful;
- validation commands and expected evidence;
- failure modes, exclusions, and rollback;
- source revision plus reviewer/provider/model provenance.

At retrieval time, the local model receives the current task, current source,
exact graph context, and the top approved patterns. It is instructed to adapt
the procedure and revalidate it—not to copy an old patch. Byte-identical output
requires a versioned deterministic generator; retrieval-augmented generation
targets functionally equivalent, validated results.

## Failure and consistency rules

- If required cloud review is unavailable, the task remains
  `cloud_review_required`; it neither advances nor silently switches provider.
- If artifact storage fails, `review_record` fails before the database review
  is committed.
- If the review transaction fails after artifact publication, the immutable
  object may be orphaned and can be garbage-collected only after a database
  reference scan and retention window.
- If Milvus is unavailable, approval remains committed in PostgreSQL and the
  outbox retries, but the task stays `rag_readback_required` and the next task
  remains queued.
- Deleting Milvus never deletes canonical knowledge; reindex rebuilds it from
  PostgreSQL.

## Enterprise extension

The same contracts scale to a disclosure broker with DLP, short-lived workload
identity, tenant policy, immutable object storage, a durable review queue, and
metered provider routes. PostgreSQL remains authoritative; Milvus Distributed
remains a projection. Record export bytes, provider/model, latency, usage,
review dispositions, validation results, candidate ID, and approved knowledge
ID for every review trace.

See the [implementation guide](implementation-guide.md),
[operations runbook](operations.md), [security model](security.md), and
[capability evaluation](cost-routing-evaluation.md).
