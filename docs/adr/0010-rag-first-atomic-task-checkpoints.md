# ADR-0010: RAG-first atomic task checkpoints

**Status:** Accepted

## Decision

Every governed development workflow is decomposed into atomic tasks and stored
in a PostgreSQL FIFO queue. Submitting another task never rejects it merely
because work is active. The controller activates only the queue head. A queued
task may be rejected only by an explicit, authorized `TASK_REJECTED` event with
immutable evidence.

RAG lookup happens when the task is activated, not when it is submitted. This
allows task N+1 to reuse a lesson promoted by task N. The lookup hydrates only
approved PostgreSQL knowledge discovered through Milvus. A score at or above
the configured threshold selects `rag_hit`; otherwise policy selects either
`rag_miss_cloud_review` or `rag_miss_local_only`.

The enforced task states are:

```text
queued
  -> local_execution
  -> cloud_review_required       only for an allowed RAG miss
  -> local_revision_required     after cloud findings
  -> validation_required
  -> promotion_required
  -> rag_readback_required
  -> completed
  -> activate next queued task
```

A strong RAG hit and a local-only miss both move directly from local execution
to local validation. Maintenance, confidential, and restricted work never
enters the cloud lane. An allowed RAG miss cannot bypass cloud review: OpenAI
unavailability leaves the task at `cloud_review_required`.

`execution_mode=auto` is the default for local-model tasks. In auto mode, a
policy-allowed cloud review starts without a human acceptance prompt. Setting
`execution_mode=manual` explicitly inserts `review_approval_required`; a human
Product Owner must then emit `CLOUD_REVIEW_APPROVED` or
`CLOUD_REVIEW_REJECTED`. This review-start decision is separate from the later
human knowledge-promotion gate, which remains accountable in both modes.

Provider identity is part of the state-machine contract. Local result,
revision, and validation events require `provider=ollama` and an explicit
model. Cloud review requires `provider=openai`, an explicit model, and the
content hashes of both the raw review and disclosed-context manifest. Cloud
review is read-only and cannot revise a candidate. The checkpoint accepts
those hashes only when they match a persisted PostgreSQL `review_record` for
the same candidate, workflow, provider, and model. Only a subsequent Ollama
`revise` record with fresh local validation can update pending content.

Promotion requires accountable approval of the generalized candidate. The
next task remains locked until the approved PostgreSQL record has been indexed
and the current task proves that its candidate UUID is retrievable from
Milvus. Review influence weight is retained as provenance; it is not a truth or
retrieval score.

## Authority boundaries

- OpenClaw queues work and invokes models; it does not approve knowledge.
- The MCP service validates transitions, providers, evidence, and retrieval.
- PostgreSQL is authoritative for queue order, checkpoint state, provenance,
  approvals, and immutable artifact references.
- The artifact CAS stores exact task, lookup, validation, raw-review, and
  context-manifest bytes.
- Milvus is an approved-only discovery projection and must pass read-back.
- The cloud reviewer has read-only repository access and no promotion tool.

## Consequences

- Work can be submitted ahead of time without running tasks concurrently or
  losing them.
- RAG routing decisions use the newest approved lesson available at execution
  time.
- Every cloud-reviewed result returns to Ollama for local application and
  deterministic validation.
- A failed cloud call, failed validation, unapproved candidate, delayed outbox,
  or failed Milvus read-back stops queue advancement.
- Operators can audit the exact route, provider/model, candidate, influence,
  evidence hashes, and state transition for each atomic task.
