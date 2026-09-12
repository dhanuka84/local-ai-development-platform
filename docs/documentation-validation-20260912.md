# Documentation refresh validation — September 12, 2026

The project documentation was refreshed on `fix/agent-ready-gaps-20260910`
against revision `4f7e16bf6faf349adfc0d5b28182d6b0caed98db` plus the working-tree
changes. No live rollout or KB publication was performed.

## Deliverables

- A [developer guide](developer-guide.md) explaining what the platform does,
  why its boundaries exist, setup, the task lifecycle, feature implementation,
  E2E coverage and troubleshooting.
- A [documentation index](README.md), current stack reference, corrected
  implementation/runbooks, terminology and deployment acceptance guidance.
- [ADR-0011](adr/0011-scoped-autonomy-and-functional-acceptance.md), documenting
  delegated definition authority, explicit KB-entry publication and functional
  acceptance. Earlier controller/policy ADRs now distinguish implemented local
  foundations from proposed enterprise and orchestration work.
- Six [Mermaid diagrams](diagrams/README.md), each with SVG and PNG exports.
  The former generated explainer is replaced by an editable diagram of the
  separate KB-entry and definition paths. Historical prompts, pilot evidence,
  screenshots, original measured counts and license notices remain identifiable.
- `make diagrams`, `make docs-check`, source/export digests and a documentation
  CI job. MCP client-facing instructions now describe validated reuse completing
  with its new KB candidate still pending.

## Executed validation

| Check | Result and boundary |
|---|---|
| `make check` | Passed after the final MCP instruction update: formatting, vet, race-enabled Go tests and script regressions |
| `make diagrams` and final local-diagram render | All six Mermaid sources rendered with pinned CLI 11.16.0 and local Chrome; all SVG/PNG pairs visually inspected |
| `make docs-check` | Local Markdown links/anchors, document reachability, source/export hashes and SVG/PNG structure/dimensions passed |
| Documentation checker integration scenarios | 10 passed in a temporary repository: valid baseline with fenced examples; missing link; missing anchor; orphan page; stale source; corrupt SVG; missing export; corrupt PNG; unrendered source; restored baseline |
| `make agent-ready-functional` | **28/28 mapped requirements**, **11 suites**, **132 passing test/subtest results**, zero failures/skips |
| `git diff --check` | Passed |

The functional suite runs actual gateway, worker, admin and pilot binaries
with disposable PostgreSQL/AGE, Milvus, Cerbos and an actual collector.
Ollama protocol responses and approval actors are synthetic fixtures. No model
was invoked by rendering or validation, and remote CI was updated rather than
executed by this session.

## Reproducible evidence

Functional run directory:

```text
.local/agent-ready-e2e/run-20260912T165003Z-gxXzEC/
```

| Evidence | SHA-256 |
|---|---|
| Functional source snapshot | `26cdaed24b8af912c7397002276b81ed5c1027fc0bb92a068579d00917fc8c0e` |
| `summary.json` | `e88a3a7b1896b99f10ca4f4657e735c1eacd8d4cecaf81f397e96a992a2a150b` |
| `functional-acceptance.md` | `58850123f8d3140bc5c16734eeb1b51a110295d76830c187b64ec06350265f29` |
| `source-manifest.json` | `1d755052cab8086f4f356be7b4a644a146f9db5fcdebda0f629d745921a0f413` |

Exact test JSONL/stderr, source manifests and sealed evidence hashes remain in
that run directory. Documentation/diagram hashes, check logs, and an unsubmitted
pending capture draft are retained separately under:

```text
.local/documentation-validation-20260912T165003Z/
```

The repository [diagram manifest](diagrams/manifest.json) independently binds all
six sources and their exports. The functional source snapshot is scoped to the
test runner's inputs; the documentation handoff manifest covers the complete
Markdown/diagram set and documentation tooling.

## Acceptance that remains deferred

The original full-rollout register remains **22/28**. L12, L18, E02, E06, E07
and E08 retain their live-pilot, cutover, cohort, ownership, retention/storage
and target enterprise prerequisites. Functional success does not supply those
inputs. Generated KB entries stay pending for explicit user decisions.

MCP knowledge lookup/capture could not authenticate during this documentation
session because the existing vault token was not materialized. No credential
was replaced and no approval check was bypassed. The local capture draft is
explicitly unsubmitted and is not a published KB entry.
