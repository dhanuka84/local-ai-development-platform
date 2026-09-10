# Remaining agent-ready operating decisions

These decisions keep broader article coverage explicit while the first release
remains a governed local software-development platform. They do not claim that
a Kubernetes cluster, enterprise identity provider or production data operating
model has been deployed.

## Adaptive Gold applicability

The current data product is reusable software knowledge. Its adaptive output is
a pending lesson, with immutable source/generation evidence, local validation,
human approval and verified index publication. That lifecycle is the applicable
agent-curation mechanism for this release. Agent-created warehouse tables or
views are outside the current source and consumer contracts.

Revisit that boundary when a named consumer needs agent-curated datasets or
views and can supply authoritative source contracts, a measured evaluation set,
an accountable owner, retention rules and a withdrawal/rollback procedure. A
successful two-task lesson pilot alone does not justify a new warehouse layer.

## Staged autonomy

Human publication and QA-attestation boundaries continue at every stage.
The following are proposed adoption criteria; they are not observed results or
automatic permission grants. A responsible operator must approve and record a
project/model/capability-specific stage decision against the exact evidence.

| Stage | Permitted operating pattern | Evidence before broader operation |
|---|---|---|
| Shadow | Retrieve approved context and produce proposals for comparison | Representative expected outcomes, denied-action cases and complete trace collection |
| Supervised | Execute explicitly authorized bounded local work packets; review changes and failures | At least 20 representative tasks over 7 days, ≥95% task success, complete owned traces, zero escaped file/project scopes, and a successful rollback exercise |
| Guarded | Repeat the reviewed class of reversible tasks within its approved scope and expiring credentials | At least 100 representative tasks over 14 days, ≥99% task success, complete traces, zero authorization escapes, current source/validation evidence, tested revocation and rollback, and explicit owner/QA stage approval |

These initial thresholds are engineering proposals for evaluation, not values
taken from the article. Count failures and retries; exclude invented runs and
unobserved external work. Re-evaluate after model, policy, capability or source
contract changes. A scope escape, missing mandatory evidence or failed rollback
returns the affected capability to supervised operation while it is corrected.

The current pilot supplies two tasks and cannot certify a broader autonomy
stage. There is no automatic stage-promotion endpoint. Existing work-packet,
credential and human decision gates remain the executable authority.

## Unstructured source boundaries

Supported inputs are explicit software-knowledge text, retained generation
artifacts and repository snapshots. Exact duplicate publication, empty content,
source revision, validation age, vector dimensions and projection freshness have
executable checks. OCR/PDF extraction is not an ingestion capability certified
by this release.

`knowledge_candidate_quality` now provides bounded token-bigram similarity
against eligible PostgreSQL candidates, plus Unicode/control-character and
possible-truncation flags. Tests cover lexical duplicates, unrelated text, a
contradictory high-similarity pair, exact versions, project boundaries and
quarantined sources. Scores produce review candidates, never automatic merges
or approval. The 0.85 lexical threshold and 100-candidate/64-KiB limits are
explicit heuristics; their recall must be evaluated on a representative consumer
corpus before any stronger quality claim. Before adding document extraction,
define coverage/page counts, extraction version, truncation and OCR error
handling, quarantine behavior and source refresh tests. Before enabling a drift
threshold, baseline it on the actual consumer corpus and retain false positives
and false negatives as evaluation evidence.

## Ownership and maintenance

The local pilot's accountable actor is the existing `human:local-developer`
principal. That observation does not assign enterprise responsibilities or
assert that another person accepted an owner role.

For each deployed data product, record the active owner principal, QA reviewer,
operations contact and replacement/escalation route. Review failed source/index
queues daily, model/capability changes before release, and definition validity
before the enforced 30-day limit. On deprecation, stop new use, identify
dependent tasks and definitions, preserve prior decisions and evidence, and
provide a versioned replacement with its own validation and approval. Complete
the named ownership register before claiming an adopted operating model.

## Retention and enterprise deployment

The current retention setting triggers review/hold alerts; it does not delete
evidence. A local backup and complete restore/upgrade rehearsal are available.
Production still needs a chosen storage service, retention periods by data type,
hold authority, immutable archival policy, encrypted backup verification and a
tested deletion process that handles PostgreSQL references and derived indexes.
No destructive retention job is introduced without those concrete requirements.

Enterprise completion additionally requires a target cluster/account, identity
issuer and audience, trusted tenant/project mapping, policy distribution,
storage/PVC choices, private model endpoints, availability targets and measured
recovery objectives. The checked-in Kubernetes source-verification overlay is
ready to adapt and test there. Offline rendering and a local read-only mount
test do not prove tenant isolation, failover or a production recovery objective.
