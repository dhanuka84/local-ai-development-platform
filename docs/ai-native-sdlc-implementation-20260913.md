# AI-native SDLC implementation and validation — September 13, 2026

Navigation: [Gap assessment](sdlc-gap-assessment.md#implementation-progress) ·
[Usage and source setup](product-knowledge-and-evaluated-sources.md) ·
[Documentation index](README.md).

This change implements a shared product-KB and evaluated-source dependency
slice on `feature/ai-native-sdlc-20260913`, based on
`9fba87f23f0e733a6bba0528b505f647bbac5aef`. The earlier
[documentation review](documentation-scope-validation-20260913.md) assessed the
pre-implementation source; its historical counts and findings remain intact.

## Delivered behavior

| Change | Implementation evidence | Observable result |
|---|---|---|
| Immutable product records and explicit decisions | [Domain](../internal/domain/product.go), [migration 19](../migrations/000019_product_knowledge.sql), [PostgreSQL transactions](../internal/postgres/product.go) | Pending versions do not replace accepted heads; acceptance binds QA evidence and the human actor |
| Both KB dimensions and authoritative context | [Product service](../internal/service/product.go), [Milvus](../internal/milvus/product.go), [AGE projection](../internal/age/product.go) | Current exact records and relationships survive restart; stale versions/edges are withheld; projection read-back is verified |
| Typed intent and protected criteria | [Intent contract](../internal/domain/intent.go), [context resolver](../internal/service/intent.go) | Accepted BRS/digest bindings resolve; changed requirements and uncertain assumptions block readiness |
| Governed source ingestion | [Registry/transport](../internal/sources/registry.go), [source service](../internal/service/source.go), [receipt transaction](../internal/postgres/source.go) | Bounded log/metric/event/lake/audit protocol fixtures yield complete, partial or failed receipts; invalid payloads never become eligible observations |
| Source access after retention | [Cerbos source policy](../policies/cerbos/resource_product_source.yaml), [field reauthorization](../internal/service/source.go) | A diagnosis principal cannot retrieve Operations-only fields through source, record or context tools |
| Business reconciliation | [Deterministic oracle](../internal/domain/product_evaluation.go), [evaluation service](../internal/service/product_evaluation.go) | Known failure is violated, complete matching evidence is satisfied, delayed lake evidence is inconclusive; changed BRS invalidates old evaluation reuse |
| Accountability and audit | [Trace fields](../internal/domain/trace.go), immutable receipts/evaluations, input/artifact digests | Actor, acting role, accountable source owner, source contract and query identity remain attributable |
| Exact generation preservation | [Capture implementation](../internal/service/service.go), [regression](../internal/service/service_test.go) | Original whitespace, CRLF and Unicode are preserved; blank input is still rejected |
| Client and deployment configuration | [Compose](../deploy/compose/compose.yaml), [source registry example](../examples/sources/registry.example.json), [Codex launch tests](../scripts/codex_launch_test.py) | Empty source registry by default; product decisions keep explicit prompts; assigned routine tasks retain standing authority |

## Executed verification

The successful complete disposable run is retained at:

```text
.local/agent-ready-e2e/run-20260913T190905Z-4pjJkh/
source_snapshot_sha256:
3973e858d1f5ed037b4106a46d3b4e0dac67ab6c9c8e3fd723e9716d0f426e80
```

`make agent-ready-functional` passed **28/28 existing mapped requirements,
11 suites and 139 test/subtest results, with zero failures and skips**. It ran
real application binaries with disposable PostgreSQL/AGE, Milvus, Cerbos and
collector services. Source responses, model protocol responses and human
decisions were synthetic fixtures. No real production observations or generated
KB entries were approved by these tests.

The retained `source-manifest.json` binds new and existing tested source files;
`summary.json`, per-package JSONL logs and `evidence-sha256.json` retain the
results. The source digest describes the copied implementation snapshot, not
the later documentation edits. It includes the HTTP inventory and Codex
approval checks plus the empty-result field/filter-metadata regression.
Final `make check` verifies the full checkout.

Additional verification includes `make check` (format, vet, race tests and
script checks), `make authz-policy-test` (**97 assertions**), `make docs-check`,
and the newly rendered product/source diagram. Exact command outputs are
retained in `.local/ai-native-implementation-20260913T180439Z/`.

Development evidence also retains failed runs. One early source test decoded
an omitted observation ID into a reused Go struct; the receipt now serializes
the empty ID and the test resets its result. A later run passed all suites but
failed while writing a duplicate evidence-manifest filename; the extra writer
was removed and the full command rerun successfully. Unit/script checks also
identified stale tool counts and approval expectations, which were corrected
without relaxing the new publication gate. Failed receipts are not relabeled
as completed acceptance runs.

## Scope of this result

The generation-byte defect is closed. The composite A01–A12 requirements remain
tracked with their explicit residual work in the
[progress table](sdlc-gap-assessment.md#implementation-progress). This change
does not supply a general agent execution supervisor, an independently isolated
product-test runner, native Kafka/lake/database bridges, delivery or remediation
actions, or a human outcome/exception interface. It supplies concrete contracts
and tested KB/source/evaluation boundaries for those next components.

The existing live installation is unchanged. Its previously observed tool
mismatch remains a rollout prerequisite; the successful disposable run proves
the new source behavior, not live cutover, model quality, enterprise readiness
or end-to-end autonomous feature delivery and incident resolution.
