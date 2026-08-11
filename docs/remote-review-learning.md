# Remote Review and Local Learning

## Outcome

The normal path uses a local model for implementation. Codex, Kimi, or both may
provide an explicit independent review when policy allows cloud use. Useful
improvements can then move through review and approval so future local-model
sessions can find them.

The platform does **not** put raw reviewer output into Milvus. It saves the exact
response as evidence. A recommendation becomes searchable knowledge only after
someone applies it locally, runs checks, rewrites it as a reusable lesson, and
explicitly approves it.

![Remote review and local learning explainer](diagrams/hybrid-ai-review-learning-explainer.png)

The exact technical flow is available as a
[high-resolution PNG](diagrams/hybrid-ai-review-learning-loop.png). Its
editable source is retained at
`diagrams/hybrid-ai-review-learning-loop.mmd`. The poster's
[generation prompt](diagrams/hybrid-ai-review-learning-explainer.prompt.md) is
retained for reproducibility.

## Responsibility boundaries

| Component | Owns | Must not own |
|---|---|---|
| OpenClaw | Task classification, model routing, context minimization, reviewer selection. | Knowledge approval or vector truth. |
| Ollama worker | Local implementation, local regeneration, and maintenance inference. | Cloud escalation policy. |
| Work-packet verifier | Deterministic scope, patch, and validation enforcement in a disposable clone. | Model invocation or human approval. |
| Codex CLI | Direct cloud-backed development or repository/diff-focused advisory review, with MCP retrieval and capture. | Direct knowledge promotion or local-only maintenance. |
| Kimi | Architecture, design, optimization, and long-context advisory review. | Direct knowledge promotion. |
| MCP gateway | Typed retrieval, capture, evidence, review, approval, and graph operations. | Autonomous task routing. |
| PostgreSQL | Canonical workflow, provenance, review decisions, graph edges, stable IDs, and outbox. | Approximate semantic ranking. |
| Artifact CAS | Exact prompt, output, raw review, and context-manifest bytes by SHA-256. | Approval state. |
| Milvus | Semantic discovery of approved knowledge and selected graph projections. | Canonical records or pending reviewer prose. |

## Development workflow

1. OpenClaw classifies the task and creates a
   `hybrid-ai/work-packet/v1` document.
2. The policy evaluator rejects forbidden disclosure, missing approvals, or an
   invalid maintenance/cloud combination.
3. Ollama produces a local implementation or unified patch using approved
   knowledge retrieved through MCP.
4. The verifier applies the patch to a disposable Git clone, enforces its file
   and diff budget, and runs the declared exact-argv checks.
5. OpenClaw creates the smallest useful cloud package. It includes a task,
   acceptance criteria, sanitized diff/snippets, relevant approved rules, test
   evidence, and a manifest of disclosed context.
6. Codex, Kimi, or both return advisory findings. Use Codex for repository and
   diff correctness; use Kimi for architecture and optimization; use both only
   when independent opinions justify the disclosure and cost.
7. The local worker accepts or rejects each recommendation against current
   source. Accepted changes are reproduced locally and all checks are rerun.
8. `review_record` stores reviewer provenance and decision in PostgreSQL. The
   exact reviewer response and sanitized context manifest are written to the
   artifact CAS and referenced by SHA-256 from the same review transaction. A
   `revise` verdict requires the reproduced `improved_content` plus fresh local
   `validation_evidence` and replaces both fields on the pending candidate.
9. The improved, generalized result stays a pending knowledge candidate until
   an accountable actor uses `knowledge_candidate_decide`.
10. Approval and a `knowledge.upsert` outbox event commit atomically in
    PostgreSQL. The worker embeds the authoritative approved item with Ollama
    and upserts it into Milvus under the PostgreSQL UUID.

## Codex-first development workflow

Codex CLI may also perform the implementation instead of serving only as the
second reviewer:

1. Start the separate HTTP MCP platform in Terminal 1 and Codex in Terminal 2
   with `make codex` or `make codex-repo REPO=/absolute/path`.
2. Codex retrieves approved lessons, repository relationships, and exact code
   graph context through MCP, then inspects the current repository directly.
3. Codex implements the task and runs repository-local deterministic checks.
   The work-packet contract is required when OpenClaw delegates a patch; it is
   optional policy for an interactive Codex session unless the organization
   mandates it for every change.
4. Capture the validated Codex result with `generation_capture`, including the
   provider/model, repository revision, ordered procedure, and validation
   evidence. The exact prompt/output become immutable artifacts and the
   reusable solution remains pending.
5. Optionally request an independent Kimi architecture review or a separate
   Codex review pass, using the same sanitized evidence lifecycle.
6. After accountable approval, the outbox embeds the generalized result. A
   future Ollama session can retrieve it and adapt the learned procedure to
   current source.

Running the CLI locally does not make Codex inference local. Treat source,
diffs, prompts, and MCP results visible to Codex as disclosures to OpenAI. Do
not use this workflow for the local-only maintenance identity.

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
  "verdict": "revise",
  "comments": "One correctness issue and one reusable transaction rule.",
  "improved_content": "Generalized, locally reproduced solution text.",
  "validation_evidence": ["go test ./internal/service passed after applying the recommendation"],
  "raw_output": "Exact reviewer response, unchanged.",
  "context_manifest": "{\"schema\":\"hybrid-ai/review-context/v1\",\"revision\":\"abc123\",\"files\":[{\"path\":\"internal/service/service.go\",\"sha256\":\"...\"}],\"checks\":[\"go test ./internal/service\"],\"data_classification\":\"internal\"}"
}
```

The result returns the SHA-256 and URI for each created artifact. A `revise`
verdict requires `improved_content` and fresh local `validation_evidence`, can
change only a pending candidate, and does not approve it. Record an unaccepted
or not-yet-reproduced recommendation with `comment` instead.

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

- If cloud review is unavailable, the development task may remain locally
  verified and explicitly marked unreviewed; it must not silently switch
  provider.
- If artifact storage fails, `review_record` fails before the database review
  is committed.
- If the review transaction fails after artifact publication, the immutable
  object may be orphaned and can be garbage-collected only after a database
  reference scan and retention window.
- If Milvus is unavailable, approval remains committed in PostgreSQL and the
  outbox retries. Exact/lexical retrieval continues where supported.
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
