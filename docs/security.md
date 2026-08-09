# Security Model

## What this means in practice

- Keep local service ports on `127.0.0.1`.
- Never put secrets or private customer data in a Codex or Kimi review package.
- Use the separate human and OpenClaw controller tokens created by
  `make env-init`.
- Treat all model output as untrusted until checks and human review pass.
- Keep maintenance on Ollama with no cloud fallback.
- Before any internet-facing deployment, add the controls in
  [Production requirements](#production-requirements).

See the [plain-English glossary](glossary.md) for security and architecture
terms used below.

## Trust boundaries

- Local Ollama, PostgreSQL, Milvus, artifacts, gateway, and worker are one machine trust zone in the Compose deployment.
- OpenClaw and Codex are authenticated MCP clients.
- Kimi and OpenAI are external processing zones. Any context given to their models leaves the local boundary.
- A generated or reviewed answer is untrusted until source inspection and
  validation support it. Saving an artifact proves what was reviewed; it does
  not prove that the answer is correct.

## Data policy

Never send secrets, tokens, private keys, credential stores, personal data,
production database dumps, raw customer payloads, or unrestricted private
repositories to a cloud model. A cloud review package should contain only the
problem, the smallest useful sanitized diff or snippets, relevant approved
guidance, check results, and a precise review question.

Maintenance is fail-closed local: the OpenClaw `maintenance` agent has a
one-model per-agent allowlist and empty fallbacks, so stored session overrides
cannot select a cloud model. Do not place Kimi/OpenAI credentials in that
agent's auth store or environment.

## Implemented controls

- All Compose ports bind to loopback.
- HTTP MCP requires a bearer token; unauthenticated mode is restricted to `APP_ENV=local`.
- The default local human principal has Development, QA, Product Owner, and
  Operations roles for all projects, so one developer can operate every gate.
- OpenClaw uses a separate non-human controller credential that cannot cross
  QA or Product Owner human gates. The two local tokens must differ.
- Tokens come from environment variables, are stored only as SHA-256 hashes in
  PostgreSQL, and are not stored as plaintext in example configuration.
- Origin protection, body limits, server timeouts, and constant-time token comparison are enabled.
- Worker/admin and production gateway images are static and non-root. The local analyzer-enabled gateway is non-root but includes Git and Go because `go/packages` needs the toolchain.
- Artifacts use SHA-256 addressing, atomic publication, and mode `0600`.
- Exact remote-review output and the sanitized disclosure manifest are stored
  as immutable artifacts referenced by the PostgreSQL review row. They are not
  vectorized automatically.
- SQL constraints enforce workflow states, relation types, confidence bounds, and no self-edges between repository records; recursive code calls remain valid graph facts.
- Only approved knowledge is vectorized and returned.
- MCP tool hints and Codex approval settings distinguish reads from writes.
- PostgreSQL outbox transactions prevent an approval without a durable indexing request.
- Code analysis resolves symlinks, enforces explicit filesystem roots and size caps, checks revisions, and rejects dirty worktrees by default.

Local bearer credentials authenticate distinct principals. Cerbos is the
internal contextual policy decision point. The Go gateway remains the
enforcement point, constructs trusted principal/resource context from
authentication and PostgreSQL, fails protected actions closed, and records the
Cerbos decision correlation with workflow events.

## Production requirements

Before internet or enterprise exposure, add:

- TLS and an OAuth 2.1/OIDC-aware API gateway.
- Per-user/workload identities, scopes, audit subject propagation, and short-lived credentials.
- Highly available internal Cerbos PDPs with pinned policy bundles, tested
  policy-as-code, decision-log retention, and fail-closed enforcement.
- Managed secrets/KMS; never plain Kubernetes Secrets as the only control.
- NetworkPolicies and egress denial, especially for maintenance workloads.
- PostgreSQL TLS, row-level/tenant isolation where needed, PITR, and encrypted storage.
- Milvus authentication/TLS and private endpoints.
- S3 object lock/versioning/encryption and malware/content scanning for artifacts.
- Central logs/metrics/traces with redaction and retention policies.
- Software bill of materials, image signing/verification, dependency review, and secret scanning.
- A data-loss-prevention gate before every cloud-model call.

## Threats and mitigations

| Threat | Mitigation |
|---|---|
| Prompt injection in stored knowledge | Approval gate, provenance, hydration from SQL, validation, project filters. |
| Prompt injection in remote-review output | Preserve it as evidence only; reproduce accepted findings locally, generalize them, and require approval before Milvus indexing. |
| Sensitive context hidden in a review artifact | Minimize and scan before cloud export; encrypt artifact storage, restrict evidence access, and apply retention/deletion policy independently of approved knowledge. |
| Reviewer response cannot be tied to disclosed input | Store the exact response and JSON context manifest by SHA-256 on the same PostgreSQL review record. |
| Stale/deleted vector result | PostgreSQL hydration and approved-status check. |
| Agent self-publishes its output | A separate non-human controller credential, human-only Cerbos gates, authenticated actor derivation, and explicit approval prompts. |
| Agent claims another role or changes governance context | Derive identity from authentication, load role/resource facts from PostgreSQL, evaluate Cerbos server-side, and never trust model-supplied authorization attributes. |
| Cerbos is unavailable or returns an unreadable decision | Fail protected writes closed; preserve workflow state and expose a retryable operational error. |
| Repository graph poisoning | Evidence required, constrained edge vocabulary, prompted upsert, SQL authority. |
| Repository prompt injection changes graph truth | Compiler-aware extraction is authoritative; OpenClaw/LLM interpretations are stored only as reviewable knowledge candidates. |
| Analyzer reads unrelated host files | Read-only narrow bind mount plus canonical-path allowlist; never mount a home directory or filesystem root. |
| Malicious or oversized repository exhausts resources | File/entity/relation caps locally; enterprise analyzers require sandbox CPU, memory, process, network, and deadline controls. |
| Token disclosure in Git | Environment references, `.env` ignored, examples contain placeholders only. |
| DNS rebinding/cross-origin local attack | SDK localhost protection plus Go cross-origin protection. |
| Worker crash loses indexing | Durable outbox, reclaimable locks, retries, idempotent Milvus upsert. |
| Cloud fallback leaks maintenance data | Strict per-agent local model and empty fallbacks. |

See [Remote Review and Local Learning](remote-review-learning.md) for the full
evidence and promotion state machine.

Report vulnerabilities according to [SECURITY.md](../SECURITY.md).
