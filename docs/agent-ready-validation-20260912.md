# Agent-ready validation receipt — September 12, 2026

The [definitions-only autonomy continuation](#definitions-only-autonomy-continuation)
below records the latest configuration, test run and terminology clarification.
The initial run remains recorded separately here.

Continued on `fix/agent-ready-gaps-20260910` in the implementation worktree,
from revision `a999db06bcf54a62bfb79afc03a386682805172c`. The initial new test
file was transferred from `main` before further implementation; the main
worktree was verified clean. These changes are not a live deployment.

## Validated result

`make agent-ready-e2e` passed all 11 suites: 130 passing test/subtest results,
zero skipped tests, and passing regression checks for all 28 checklist rows.
The suite exercises the actual gateway, worker, admin and pilot binaries with
disposable PostgreSQL, AGE, Milvus, Cerbos and collector dependencies.

The gateway scenario verifies authenticated discovery, scoped task credentials
and revocation, actual CLI patch validation, version-bound publication, exact
Milvus read-back, explicit Task B reuse, all six governed fixture definitions,
metric arithmetic, stale-source withdrawal, authoritative duplicate inspection,
immutable evidence and worker-to-collector export. The pilot scenario covers
malformed Task A output, rejected unchanged repair, syntax recount, the human
publication boundary, Task B's wrong-file failure, stale revision rejection and
successful version-2 revision. Original fixture outputs and disclosure manifests
are checked byte-for-byte before disposable cleanup.

The test reporter requires every mapped test to execute successfully, rejects
missing/skipped cases, preserves failed attempts and records delivery status
independently of regression results. CI invokes the same suite and retains the
synthetic evidence directory. Remote CI itself has not been run by this session.

Final local checks also passed `make check`, `make contracts-check`, all 61
Cerbos policy tests, `make vault-test` (10 tests), and `git diff --check`.
`make check` includes five receipt-integrity regression tests.

Evidence retained in the implementation worktree:

| Evidence | Location / digest |
|---|---|
| Full E2E receipt | `.local/agent-ready-e2e/run-20260912T150518Z-vavCRD/summary.json` |
| Receipt SHA-256 | `5f7c667f12cffe8b0085c0a37e1bbdcc34870189192e1425256048213809cda5` |
| Tested source snapshot SHA-256 | `c415541b13e3d339a7e9aae6c272e0f3d91ba2d2bf3590594e2e675910331090` |
| Exact source file hashes | `source-manifest.json` beside the receipt, including uncommitted test files |
| Verified test evidence | All 26 entries in the adjacent `evidence-sha256.json` matched retained bytes |
| Full driver and required-check logs | `.local/agent-ready-e2e/handoff-20260912/`, with separate SHA-256 manifest |

Earlier failing development runs remain in their own directories. They exposed
test publication-wait assumptions, a required MCP argument, and an excluded
CI file in the receipt writer; none is relabelled as a passing run. The final
command completed successfully and removed its own disposable services.

No inference ran in this automated validation: provider **none**, model **none**.
The explicitly synthetic Ollama protocol fixture is named
`synthetic-e2e-fixture`; it is not an actual model execution. No cloud review or
model fallback was invoked. Real-model evidence remains the retained September
10 pilot using local `ollama` / `qwen3.6:35b`.

## Delivery requirements still open

Delivery completion remains **22/28**. The new tests do not replace these
specific prerequisites:

| Item | What is still required |
|---|---|
| L12 | Exact-version accountable approval of the six retained real metric definitions, followed by real governed queries |
| L18 | Unlock the existing encrypted vault, pass preflight, then execute the prepared live cutover and actual health/authorization/export checks |
| E02 | Representative measured cohorts over the stated 7/14-day windows and an accountable stage decision |
| E06 | Accepted named deployment ownership, QA, operations and escalation assignments |
| E07 | Production storage, retention periods, hold authority, archival and deletion requirements and their deployment tests |
| E08 | Target identity/tenant mapping, cluster/storage configuration and measured availability/recovery evidence |

The attempted live `knowledge_search` could not authenticate because the
runtime `AUTH_TOKEN` file was absent. `make mcp-preflight` stopped at vault
materialization because no passphrase was available noninteractively. No vault
generation, credential, live candidate or running application was replaced.

## Optional pending knowledge capture

When authenticated MCP access returns, this outcome can be offered through
`generation_capture` as pending knowledge. Attach the revision and tested source
digest above, exact test receipts, and the actual provider/model boundary.
The ordered reusable procedure is: inspect the authoritative completion register;
attempt approved knowledge retrieval; select the correct worktree; build the
shipped binaries; provision isolated dependencies; execute the complete positive
and negative paths; reject missing/skipped evidence; preserve failures; verify
receipt hashes; and report delivery prerequisites separately from test success.
Capture does not approve or embed the proposal.

## Definitions-only autonomy continuation

The user authorized autonomous local work and selected autonomous publication
for locally validated domain, capability and metric definitions only. Generated
KB entries remain pending for an explicit user decision. The pilot's term
"lesson" means such a KB entry, rather than another data product.

`AGENTS.md`, the project and example Codex configs, all four launchers, and the
operating guidance now express that scope. Command approval policy is `never`;
definition decisions, indexing and repository relationships use `approve`.
`knowledge_candidate_decide` retains `prompt`. Existing authentication,
Cerbos policy, local validation, source freshness and database audit checks
remain enforced. The missing-token guard is now consistent across all four
launchers, including `codex-repo`.

The actual default `human:local-developer` bootstrap passed the gateway and
pilot E2E paths with its existing development, QA, product-owner and operations
roles. The tests check exact operator/validation/reason attribution, denial for
controller-only and development-only callers, and a generated KB entry that
stays pending after all six fixture definitions are published.

Final validation passed `make check` and `make agent-ready-e2e`: **11 suites,
131 passing test/subtest results, zero failures/skips, and all 28 checklist
regression mappings**. The three new launcher regressions execute all four
launch paths with a recording CLI fixture, verify definitions-only approval
settings, and reject missing credentials. Codex CLI `0.154.0` also accepted the
configuration through strict `app-server` `config/read`, without a model turn.

| Evidence | Location / digest |
|---|---|
| Final E2E receipt | `.local/agent-ready-e2e/run-20260912T154538Z-zSSJ5J/summary.json` |
| Receipt SHA-256 | `006e6671a43e1053edd2813026e7f057702c2c4430460095fedeedb8b4fa2892` |
| Tested source snapshot SHA-256 | `0cea4ed30010cbb6623f202a726e4d84bb414268b701ce134e5b8ec133a7c266` |
| Required check, driver and effective config receipts | `.local/agent-ready-e2e/definitions-only-20260912T154538Z/`, with a SHA-256 manifest |
| Pending KB capture draft | `pending-kb-capture.json` in that evidence directory; not submitted to MCP |

All 26 E2E evidence hashes were verified. The source manifest contains 245
files; after the run, only `AGENTS.md` among those inputs changed to clarify
"lesson" as "generated KB entry", with no change in publication scope. The
same wording clarification in the README and operating guides is recorded in
`terminology-clarification.json`. Test execution used no model inference and
removed its disposable services. No live knowledge was published.

Delivery remains **22/28**. L12 has standing authorization for validated
definition decisions, but still needs authenticated runtime access, actual
decisions and governed query results. The other five open requirements remain
as listed above. Both `knowledge_search` and `repository_graph_get` returned
HTTP 401 because the materialized runtime `AUTH_TOKEN` was absent. The pending
KB capture is therefore retained locally for later submission, with its ordered
procedure, evidence, revision and disclosed provider/model limits. It is not a
published KB entry. All changes remain in `fix/agent-ready-gaps-20260910`; the
main checkout was verified clean.
