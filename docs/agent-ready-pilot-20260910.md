# Retained pilot recovery — 2026-09-10

Status: Task A passed local validation and is awaiting an accountable human
publication decision. Task B and live rollout have not run.

Baseline platform revision: `ce8b7d3`; recovery changes are on
`fix/agent-ready-gaps-20260910`. Original evidence remains in the
[September 6 receipt](agent-ready-pilot-20260906.md). Follow the
[gap checklist](agent-ready-gap-checklist.md) for completion status.

## Exact candidate for review

- Project: `pilot-agent-ready-20260906`.
- Workflow: `01fa6e15-9b1b-44bb-9aef-16b621e49aae`.
- Task A: `fe5156c4-37e4-4b10-897e-bcaaadab9750`.
- Candidate: `8ccd770c-9b3c-4c57-8966-3019811abb32`, version **1**, still pending.
- Validation: `92bce3e3-6eda-422f-8b59-a9f196dd2fb4`, method `workpacket`, verdict
  `pass`, completed `2026-09-10T21:35:33Z`, expires `2026-09-11T21:35:33Z`.
- Source: the unchanged synthetic repository at
  `a6178ec106d9b1d4142d08ac83a62b51ad86ce43`, branch `main`.
- Generation provider/model: local `ollama` / `qwen3.6:35b`.

The exact pending lesson is unchanged:

> Normalization functions should be idempotent: applying the transformation
> multiple times must yield the same result as applying it once. This property
> ensures stability in data pipelines, simplifies testing (you can verify
> against normalized ground truth without worrying about input state), and
> prevents compounding errors. Always design normalization to collapse redundant
> states (e.g., multiple spaces) into a single canonical form.

The tests support the specific synthetic whitespace/case normalization
implementation. They do not demonstrate a production benefit or establish that
every possible normalization operation must be idempotent. A reviewer should
consider the lesson's intended applicability before approving its exact text.

## Recovery and validation evidence

1. Restored the retained disposable dependencies and scoped workload. Existing
   workflow, source, candidate, original model outputs and failed verifier
   artifacts were preserved. No replacement workflow or candidate was created.
2. Repeated the bounded local-model correction. It returned the unchanged
   malformed patch. Added an explicit unchanged-output diagnosis and durable
   repair-operation outcomes.
3. Added an opt-in `repair_task_a.method: recount` path. `hunk-recount-v1`
   derives correct header counts without changing any source, summary, lesson,
   filename, hunk position or final-newline bytes. Its derived patch is separate
   evidence and is not represented as an exact model response. Original
   candidate/artifact/checkpoint bindings still apply.
4. The verifier then applied the patch and ran all six behavior tests, but
   rejected Python's generated `__pycache__` files. The failed result remains
   `15f0ec4a3a79224e223c3463b6f88bbf3d86e08c2014551f84beeae999978345`.
5. Set `PYTHONDONTWRITEBYTECODE=1` in the verifier's restricted environment.
   The packet commands and file allowlist were unchanged. A regression test
   verifies that imports succeed while unrelated file writes remain rejected.
6. Reran the same explicit recovery. `git diff --cached --check` and
   `python3 -m unittest -v test_labels.py` both exited zero. The six tests cover
   outer whitespace, internal whitespace, Unicode casefold, empty input,
   idempotence and punctuation preservation. The verifier's scope and
   side-effect checks also passed. The task advanced to `promotion_required`.

Immutable evidence in the retained local CAS:

| Artifact | SHA-256 |
|---|---|
| Original exact model output | `08c69161d6e4ed87cbf14a66bac16b950399885dde9d406b81321a6401bf3ca8` |
| First September 10 unchanged model correction | `09ca44ff9d48d874e893497bf03a3cf7c9afc15b27cee49f2573a813cfe1c918` |
| Validated derived patch | `c7dc1fb0b1e23782abb454be91c17901f8f8b7f3f27465d61ca0a727da2b5dc9` |
| Source manifest | `ee82b8e09b0b59a6ae89f093cdc9b6dadbdd4fc1c2b5582c01fe776a2c0ab952` |
| Validation report | `3aa9f463fad9a8163fcefcfb69fbc0d8eeffacd0604cc196d6ce4794942f3772` |
| Six-test output | `7df1f3849b457734bf5882d0aaf4d26a9b1c9119e9bc606164ad98212a901d0d` |

## Remaining gate and deployment boundary

An accountable human must review this exact candidate and report before a
version-bound approval and `LEARNING_PROMOTED` checkpoint. Then the worker can
publish and verify Milvus indexing, and Task B can demonstrate actual reuse.
This receipt is not approval. The workload has no human QA/Product Owner roles.

Use the retained `/pilot/state/pilot-recount-20260910.json` spec and
`/pilot/bin/agent-ready-pilot-20260910` binary for this recovery. The original
spec and binary remain available. Remove the repair field after publication
when resuming Task B. Revalidation is required if the report expires.

The live database was inspected read-only on September 10: migration `000007`,
five pending candidates, no approved candidates. No live migration, candidate
decision, gateway/worker rollout or collector deployment was performed.
