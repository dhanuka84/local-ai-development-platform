# Go readability adoption

Date: September 15, 2026. Guide: [Go readability style guide, edition 1.0](go-readability-style-guide.md).
Base revision: `9fba87f23f0e733a6bba0528b505f647bbac5aef`.

This receipt records the original adoption snapshot. Reconciliation with upstream
`3fa9603` preserves that work alongside the dependency and AI-native SDLC updates.
The file counts and validation evidence below describe the original snapshot;
new upstream SDLC files are outside that readability review's inventory.

## Scope and policy

Apply the guide to all maintained Go implementation and tests under `cmd`,
`components`, `contracts`, `internal`, and `migrations`. The starting inventory
contained 136 Go files. Generated source and source-code strings used as analyzer
fixtures retain their generator or test-specific formatting. Other languages
continue to use their native checks.

At adoption, the module declared **Go 1.25.8** and suggested **Go 1.26.8**, matching
the then-current CI and build images. The readability changes kept that boundary.
Upstream now declares **Go 1.26.7** and selects **Go 1.27.1**; use the current
`go.mod` and CI configuration when validating the reconciled code. Newer syntax
remains optional: `new(expression)`, `errors.AsType`, generic methods, and JSON v2
are not required merely for consistency. Existing `errors.As`, pointer
construction, and `encoding/json` v1 remain intentional. No persisted schema,
public method signature, or JSON tag migration is needed for this adoption.

`make check` remains the automated formatting, vet, race-test, and script gate.
Review the remaining readability requirements with the guide's checklist and
specific GOR IDs. Tools cannot establish all 60 rules automatically.

## Changes by rule

| Rules | Application |
| --- | --- |
| GOR-001–006 | Organized imports with `goimports` v0.48.0 (the existing x/tools version), formatted sources, expanded compressed statement bodies, clarified registry variables, and documented package responsibilities and ownership contracts. |
| GOR-007–012 | Replaced related `else if` choices with ordered switches or early error handling; retained independent validation guards and binary conditions. Counted test loops use integer range. |
| GOR-013–016 | Separated command error reporting from resource-owning execution, scoped each migration transaction to one helper, and made scanner results explicit. Named error results are used where deferred cleanup adds failures. |
| GOR-017–027 | Preserved established constructors, interfaces, domain constants, embedding and public signatures. Named fields in positional struct/test literals. Dynamic contract and protocol maps remain at their existing boundaries. |
| GOR-028–035 | Checked serialization, SQL lookup, failure-body reads, CLI output and fixture errors; retained inspectable Git error causes; report meaningful close failures. Centralized startup failure cleanup and completed each migration's cleanup before the next iteration. |
| GOR-036–041 | Propagated cancellation through workspace validation and command signals. HTTP serving now joins its goroutine and graceful shutdown, reports timeout failures, and closes remaining connections. Request handlers must still observe cancellation. |
| GOR-042–052 | Used standard membership, sorting and joining operations; retained nil/empty JSON distinctions and canonical digest bytes. Sorted project IDs and OTLP attribute keys. Telemetry records own independent reference slices. |
| GOR-053–057 | Used test-lifetime contexts for test operations while preserving separate cleanup contexts. Named test-case fields and checked fixture setup before assertions. New lifecycle tests use explicit synchronization; external readiness polling remains bounded. |
| GOR-058–060 | Retained module and CI versions, reviewed modernizer suggestions, tested both toolchains, and linked the guide from agent and contribution instructions. This receipt separates behavior fixes from mechanical edits. |

## Intentional behavior fixes

- Gateway/worker execution unwinds deferred cleanup before a fatal process exit.
  HTTP cancellation waits for active requests within the shutdown deadline;
  timeouts return errors and force connection closure.
- CLI operations receive SIGINT/SIGTERM cancellation. Workspace Git validation
  retains cancellation in the error chain instead of flattening it to text.
- Artifact and credential-file writes check close errors. An incomplete credential
  write is revoked using a bounded cleanup context. Artifact rename fallback
  verifies an already-present destination before treating it as success.
- Registry/pilot serialization, diagnostic output, and error-response reads no
  longer hide errors. A failed OTLP response close remains retryable.
- Telemetry intent/outcome records no longer share mutable evidence slices with
  callers or one another. Project IDs and exported attribute keys have stable order.

Regression coverage includes HTTP drain/timeout/listener failure, Git cancellation,
evidence-slice independence, stable output order, and the existing disposable
functional suites for artifacts, governance, credentials, projections and recovery.

The broader `make check-all` gate also identified three production dependency
advisories in the OpenClaw plugin. Its lockfile now resolves compatible versions
of `fast-uri` (3.1.8), `hono` (4.13.8), and `qs` (6.16.0). Direct dependencies and
the pinned OpenClaw version are unchanged. The plugin is reinstalled from the
lockfile and checked with its production-only audit gate.

## Deliberate exceptions

- **GOR-028/034:** PostgreSQL and AGE deferred rollbacks preserve the operation's
  primary error. pgx permits rollback after commit and closes a real transaction's
  connection on other rollback failures. Input-only file/body closes release
  resources; successful reads and integrity checks determine the input result.
  Output closes have an explicit failure policy.
- **GOR-028:** `bytes.Buffer.Write`, string/integer-only digest envelopes, and
  `domain.NewID` have documented no-error contracts on this baseline. Their
  deliberately ignored error results are retained. Compatibility keeps NewID's
  existing two-result signature.
- **GOR-014/018/022:** Existing boolean parameters, string-valued wire states and
  embedded interchange records retain their API contracts. This pass does not
  force breaking options/type migrations for cosmetic consistency.
- **GOR-043/051/054:** Canonical JSON comparisons bind exact serialized definitions
  and preserve nilness and ordering. They are intentional digest checks. JSON v1,
  nil versus empty collections, and omitted fields remain compatibility choices.
- **GOR-039/055:** No WaitGroup, iterator framework, parallel-test conversion or
  benchmark is introduced without a use case. External-service readiness uses
  bounded polling; it is outside `testing/synctest`'s in-process model.
- **GOR-046:** Existing compound sort comparators retain their tie-breaking,
  stability and floating-point behavior. Standard helpers are used where their
  semantics match directly.

## Validation

All required checks passed on the original adoption's final Go source:

| Check | Observed result | Local evidence |
| --- | --- | --- |
| `make check-all` (includes `make check`) | Pass: formatting, vet, Go race tests, script checks, 61 policy tests, contract fixtures, 7 plugin tests, metadata/config checks and verifier build checks | `.local/go-readability/check-all-final.log` |
| `GOTOOLCHAIN=go1.25.8 go test ./...` | Pass on the declared minimum | `.local/go-readability/minimum-go-tests-final.log` |
| `make agent-ready-functional` | 28/28 requirements; 11 suites; 135 passing test/subtest results, no failures or skips | `.local/agent-ready-e2e/run-20260915T214915Z-5vCz2u/summary.json` |
| `npm ci --ignore-scripts`; `npm audit --omit=dev` | Clean lockfile install; zero production vulnerabilities | `.local/go-readability/npm-ci.log`, `.local/go-readability/npm-audit-final.json` |
| `make docs-check` and `git diff --check` | Pass | `.local/go-readability/docs-check-final.log` |
| Selected compatible `go fix -diff` analyzers | No remaining proposed changes | `.local/go-readability/modernize-final.diff` |

The functional run's source snapshot is
`7c44b8db31fb50312e7298ea7b60ee8253ea4da74c51ba39988ad12037136896`.
Its source manifest identifies that tested snapshot. The separate
`.local/go-readability/source-manifest.json` hashes all 138 final Go files.
The first cancellation regression run failed because a Git helper flattened its
error; the helper was corrected and both toolchains and functional acceptance
were rerun successfully. The failed run is retained separately.

A full npm audit at adoption reported 13 findings in the then-pinned development
dependency tree; its required production-only audit passed. Upstream subsequently
updated OpenClaw/Vitest and made the full audit part of `make check-all`. Use that
gate for the reconciled tree rather than treating this historical audit as current.

Disposable functional acceptance does not establish real-model quality, production
rollout, representative adoption, or target-specific enterprise integration.
Those acceptance prerequisites remain in the [existing checklist](agent-ready-gap-checklist.md).

## Knowledge and rollback

The pre-change `knowledge_search` returned no approved matches, and
`repository_graph_get` returned no recorded relationships for this repository.
The validated outcome is captured as pending knowledge; capture does not validate
or approve a KB candidate for publication.

- Exact evidence candidate: `f766641d-d431-4136-a60e-4470009d4b48`, version **1**,
  status **pending**; content SHA256
  `c71cf206bd448f3e80b616c1d44c11d8a815187d3d65d8209c250d5af944985d`.
- The existing capture API trims surrounding whitespace. A lossless JSON envelope
  preserves the original generated output and context manifest byte-for-byte;
  both were read back through MCP and verified. Receipt:
  `.local/go-readability/exact-candidate-receipt.json`.
- The earlier normalized capture `830fde6f-2c84-4936-b6cd-16a0b7237fe6` remains
  pending and is explicitly superseded by the exact-evidence candidate. Neither
  candidate was approved, rejected, or embedded.


Rollback requires reverting this source change and rebuilding the affected
binaries. There are no database migrations, index rebuilds, or data rewrites.
Preserve the supplied style guide when undoing generated code changes.
