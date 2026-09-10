# Retained pilot recovery — 2026-09-10

Status: the real local-model pilot completed at **2026-09-10T22:11:14Z**.
Task A was explicitly approved by the user, published and verified in Milvus.
Task B reused it and passed local verification. Task B's revised lesson and the
six context definitions remain pending. Live rollout has not run.

Baseline platform revision: `ce8b7d3`; recovery changes are on
`fix/agent-ready-gaps-20260910`. Original evidence remains in the
[September 6 receipt](agent-ready-pilot-20260906.md). Follow the
[gap checklist](agent-ready-gap-checklist.md) for completion status.

## Exact candidate for review

- Project: `pilot-agent-ready-20260906`.
- Workflow: `01fa6e15-9b1b-44bb-9aef-16b621e49aae`.
- Task A: `fe5156c4-37e4-4b10-897e-bcaaadab9750`.
- Candidate: `8ccd770c-9b3c-4c57-8966-3019811abb32`, version **1**, now approved.
- Validation: `92bce3e3-6eda-422f-8b59-a9f196dd2fb4`, method `workpacket`, verdict
  `pass`, completed `2026-09-10T21:35:33Z`, expires `2026-09-11T21:35:33Z`.
- Source: the unchanged synthetic repository at
  `a6178ec106d9b1d4142d08ac83a62b51ad86ce43`, branch `main`.
- Generation provider/model: local `ollama` / `qwen3.6:35b`.

The exact approved lesson is unchanged:

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

## Explicit approval and successful reuse

The user answered **“yes”** to the concrete question about approving this exact
Task A candidate/version in the isolated pilot, then directed continuation.
The decision was submitted through the normal CLI under the existing
authenticated `human:local-developer` identity, with a temporary credential and
project-scoped roles derived from that operator's actual live authority. No
human role was assigned to the pilot workload.

- Decision: `ca313497-de19-4d33-b1b8-4c17bdfddd20`, approved at
  `2026-09-10T22:00:27.799878Z`.
- Idempotency key: `user-approval-20260910-task-a-v1`.
- `LEARNING_PROMOTED` event: `06a65366-31e4-4a7e-8fde-58bd90ca7312`.
- Exact ID/version/digest Milvus read-back verified at
  `2026-09-10T22:01:28.46826Z`; Task A then completed at checkpoint version 6.
- Task B: `44f1010e-39e1-4409-96dd-b1f429404e65`, completed at checkpoint
  version 6. Its Milvus lookup found Task A, then PostgreSQL hydrated the record.
- Explicit `used_context` receipt at `2026-09-10T22:02:09.399415Z` binds Task A
  version 1 and its validation/source digest to `/pilot/source`, branch `main`,
  revision `a6178ec106d9b1d4142d08ac83a62b51ad86ce43`.

Task B's first output added a blank EOF line and repeated Task A's filename and
function. The verifier rejected it. Its exact output remains
`096c6a5fdc55d60641428e83dc27aee90d145fa900447979dc116c4be01b0742`, and the
failed verifier result remains
`cde6dd1759226a9e7eca0273bbb194b2db118d7e24a4b5017daa01cd63d5f208`.

The runner now passes only the approved general lesson into a separately
labelled reference field; the previous task's prompt and summary are excluded.
An explicit `revise_task_b` request binds the failed output, failed verification,
candidate version and checkpoint. A fresh local Ollama generation passed the
unchanged packet precheck. The normal local-revision path updated the pending
candidate to version 2, including its corrected summary, and reran verification
against that exact version. Optimistic version checks reject stale revisions.

`git diff --cached --check` and `python3 -m unittest -v test_keys.py` exited zero.
All six behavior tests, filename/size limits and side-effect checks passed.
`VALIDATED_REUSE_COMPLETED` completed Task B without publishing its lesson.

| Evidence | ID / SHA-256 |
|---|---|
| Task B candidate, still pending | `c93fa6f3-2cde-427a-81c6-3494b02cdf02`, version 2 |
| Exact successful local output | `a89c6b039f308eff820909b133692048728db843d3e35254f162a8fc44cbd42e` |
| Validated Task B patch | `2d7770d1b38acd98729956edd0a36b665d5d9c116ed3e05d5b543e790820a4e4` |
| Task B validation | `9ebc86be-08b1-42a0-8906-b044cdd0c1ac` |
| Task B validation report | `dcad3b84effc4047b37c505f0c8965d01346c2b1a0ae5ccbe8cbc6eec1f0d9dd` |
| Task B source manifest | `f6222122717c1b5d63e5b66ead475e83a5c5fe27bce2d91f2c88d939914c2f50` |
| Task B six-test output | `dbbadc87645eb98dda659675cea7c008a8178b5c26c5495d71ffda4bd82577c5` |
| First complete pilot report | `2d80f8835c4172df19925b53f6a6823a50e0dc7705946704e67c216aa0544d50` |

The report has `status=completed`, `trace.complete=true`, and no missing owned
evidence. Coverage is explicitly limited to platform-owned boundaries. Original
failures remain in the trace; this is not a claim that every attempt passed.

The new `knowledge_candidate_quality` endpoint was also exercised against Task B
version 2 in this retained pilot. It returned
`possible_duplicate_requires_review` with Task A version 1 at lexical similarity
1.0. Both lessons have content digest
`cc84fdb527bc7e64fc31b59aadf859b278db4e7ff3c88f24cb97c2c5ba7d8638`.
The exact response digest is
`339466ece57bf308d5d9a4c2158fc79af6e66b79b4158b1ee74c576910edb4f2`,
retained as private immutable evidence. Task A remains approved and Task B
remains pending. The advisory result does not merge, reject or approve either
record; publishing the duplicate is unnecessary for this completed reuse pilot.

## Metric definitions prepared for a separate decision

The deterministic contract and SQL-preparation validation ran under the same
operator's existing QA authority, scoped to this pilot. Validation ID:
`731aba1e-9d1d-4f34-9bad-f547a9cf8d0a`, completed
`2026-09-10T22:34:34.771233Z`; evidence:
`f7aee4651d648bcb4c75d612fe7cb73bddb8dd5ddc0dc03b5bb19d88ad959701`.
Registry digest:
`3873d38f2eea052d779c9616981550df38711ea44123c76ddff398a576fe2ea4`.

All definitions below are version 1 and **pending**. Their exact source is
[`registry.valid.json`](../contracts/context/v1/registry.valid.json); executable
formulas are bound into the registry digest by
[`registry.go`](../internal/contextregistry/registry.go).

| Definition | Exact definition SHA-256 |
|---|---|
| `candidate_approval_rate` | `b208e06b272e99996aaf110363f18f05c2bc075c39315a5d6a9983d10266f97e` |
| `candidate_validation_rate` | `8bd2f3f779fd12735c0332d713d2ff647aba67c3cdf53b14a5b16cbd6ec887a9` |
| `knowledge_promotion` | `d6cdf3e8dbab1d54e4e5edaf2f87eef3346ee59d285e1a45c9a8fd2be67cf70b` |
| `pending_index_age_seconds` | `2290301574b96fdb37bd81093cd454c6ab4c00d2bc755c0d584a653de7fbcbd8` |
| `software_knowledge` | `80b5803401c7b6f7d2ce5466fa0e43d2b997d90b53bb79b0d5040474e081a20b` |
| `validated_reuse_rate` | `9b1ca95387d2e0003af37233fb725238312db9498f009accb71beb5a03b9a0c9` |

The governed metric API was checked and correctly denied the request before
definition approval. The user's approval of Task A is not approval of these
definitions. A later decision needs each exact definition/version/hash and the
registry validation above; do not fabricate that decision to finish a report.

Raw pilot observations are two completed locally validated tasks, one task with
recorded reuse, two passing candidate/version validation reports, and one
approval decision. These are diagnostic counts, not approved metric outputs.
Failed patch attempts without a validation report are retained in the trace and
are not included in the registry's candidate-validation denominator.

## Runtime and deployment boundary

Use the retained `/pilot/state/pilot-recount-20260910.json` spec and
`/pilot/bin/agent-ready-pilot-20260910` binary for this recovery. The original
spec and binary remain available. Remove the repair field after publication
when resuming Task B. Revalidation is required if the report expires.

Task B used `/pilot/state/pilot-revision-20260910.json` and
`/pilot/bin/agent-ready-pilot-revision-20260910`. Keep the original artifacts and
binary; recovery does not replace exact model output with a derived answer.

Live release recovery was rehearsed separately: a protected local dump restored
on the same PostgreSQL/AGE image and upgraded from migration 7 to migration 18.
All five pending candidates and 130,516 code entities were preserved, with an
unchanged knowledge content/status digest. All 14 referenced CAS objects were
verified in the local archive. See the [release preparation](agent-ready-release-20260910.md).
No live candidate decision or live application/database rollout was performed.
