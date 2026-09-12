# Agent-ready end-to-end regression tests

The [September 12 validation receipt](agent-ready-validation-20260912.md)
records the completed local run, exact evidence digests and remaining delivery
prerequisites.

Run the full local suite from the implementation checkout:

```sh
make agent-ready-e2e
```

`make agent-ready-integration` runs the same suite. Each invocation creates an
independent Compose project on an internal network, with disposable PostgreSQL,
AGE, Milvus, Cerbos and an actual OpenTelemetry collector. It builds and runs the
gateway, worker, admin CLI and pilot executable from the current source. No live
environment file, vault, model service, database volume or network is used.
Image builds may download pinned build dependencies before test isolation.

The gateway and pilot scenarios use real HTTP, authentication, policy decisions,
database transactions, file-backed evidence, Git, Python verification, indexing
and collector export. A loopback fixture supplies deterministic Ollama protocol
responses. Synthetic human identities exercise approval gates only inside each
test database. These tests measure implementation behavior, not model quality,
human adoption, production retention compliance or enterprise availability.

The gateway and pilot use the actual default local-developer bootstrap, with
its four operator roles. Explicit synthetic lesson decisions retain operator
attribution, and the subsequent reusable lesson stays pending. Publication still
rejects missing validation, stale versions and unauthorized workload/development
credentials. `make check` also executes all four Codex launcher paths with a
synthetic vault token, loopback health server and recording CLI fixture to check
the effective arguments, the lesson decision prompt, autonomous definition
decisions and local provider selection. That launcher regression
does not invoke a model or claim a live Codex-to-MCP round trip.

## Scenarios and checklist mapping

The machine-readable [coverage map](../tests/agent-ready-coverage.json) names
every required test and the evidence boundary for every checklist row. The
reporter rejects unmapped checklist IDs, and missing, skipped or failed required
tests fail the run. Companion contract/component tests supplement E2E cases;
they are not relabelled as complete deployment tests.

| Checklist | Automated scenario / acceptance boundary |
|---|---|
| L01 | Actual MCP/CLI version-bound publication, workload denial and PostgreSQL concurrency |
| L02 | Executed work-packet validation and positive/negative contract fixtures |
| L03 | Actual stale-source exclusion, authoritative hydration, projection and recovery adapters |
| L04 | Complete task trace, exact retained pilot outputs, failed repair receipts and immutable operations |
| L05 | All six synthetic definitions, exact decisions and governed metric formulas |
| L06 | Complete two-task gateway and pilot-executable scenarios |
| L07 | Pilot corrupt patch, unchanged correction rejection, syntax recount, stale binding and Task B revision |
| L08 | Actual Git/Python verifier using deterministic patch responses; real-model result remains in the retained pilot |
| L09 | Exact synthetic operator approval; real Task A decision remains separately recorded |
| L10 | Actual worker publication and exact Milvus read-back |
| L11 | Pilot Task B retrieval, explicit use, failed output and locally verified version-2 revision |
| L12 | Complete synthetic trace and approved fixture metrics; real definitions still need recorded validated decisions and queries |
| L13 | Self-provisioning services, required-test execution and retained result report |
| L14 | Migration-7 upgrade preservation and actual admin migration; protected backup restore remains separate evidence |
| L15 | Compatible built gateway, worker, admin and policies in the disposable stack |
| L16 | Worker durable export queue through the actual collector |
| L17 | Required checks, contract fixtures and complete coverage mapping |
| L18 | Disposable startup/export rehearsal; actual live cutover still requires vault unlock |
| E01 | CLI task credential issuance, HTTP revocation, expiry and live authority intersection |
| E02 | Existing authority and maintenance boundaries; representative timed cohorts and stage decisions remain external |
| E03 | Pending access, exact validated publication and currently eligible retrieval |
| E04 | Reuse creates a pending proposed lesson under the current applicability decision |
| E05 | Exact-version duplicate inspection, project denials and quarantined-source exclusions |
| E06 | Attributed test definition decisions and stale-definition withdrawal; deployment ownership acceptance remains external |
| E07 | Review/hold alerts, immutable evidence and actual collector export; production archival/deletion requirements remain external |
| E08 | Actual project authorization and enterprise configuration denials; target-cluster identity/HA/recovery tests remain external |
| E09 | Source/verifier regressions supplement the retained overlay and read-only-mount receipt; this suite does not deploy Kubernetes |
| E10 | Every checklist ID must have explicit tests and an acceptance boundary |

The six open delivery rows stay open: passing a synthetic gate test cannot
publish the real metric definitions, unlock a vault, provide a 7/14-day measured
cohort, assign an accountable owner or certify an unspecified production target.

## Retained evidence

Each run prints its unique directory under `.local/agent-ready-e2e/`. It retains:

- Exact `go test -json` output and stderr for every suite, including failures.
- `summary.json`: command exit codes, per-checklist test outcomes, source
  revision, source snapshot digest and explicit synthetic scope.
- `source-manifest.json`: hashes of the source files used by the check image,
  including uncommitted new tests; `working-tree.patch` records tracked edits.
  The host hashes its excluded CI configuration in `ci-workflow.sha256`.
- `evidence-sha256.json`: hashes of the completed evidence files. The runner
  seals those files read-only and refuses to overwrite an existing receipt.
- `services.log` on failure, retained separately during Compose cleanup.

Cleanup removes only the invocation's owned services and volumes; receipts
remain. CI uses the same command and uploads its synthetic test receipts even
when a check fails. An earlier failed run is never overwritten or counted as a
pass. `make check` alone deliberately skips externally dependent tests; it does
not replace `make agent-ready-e2e`.

For future real-pilot or deployment acceptance, attach the relevant exact
runtime report, operator decision and target-environment evidence to the open
checklist row. Never replace that evidence with this suite's synthetic approval.
