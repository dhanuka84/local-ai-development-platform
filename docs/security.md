# Security Model

## Trust boundaries

- Local Ollama, PostgreSQL, Milvus, artifacts, gateway, and worker are one machine trust zone in the Compose deployment.
- OpenClaw and Codex are authenticated MCP clients.
- Kimi and OpenAI are external processing zones. Any context given to their models leaves the local boundary.
- A generated or reviewed answer is untrusted until source inspection and validation prove it.

## Data policy

Never send secrets, tokens, private keys, credential stores, personal data, production database dumps, raw customer payloads, or unrestricted private repositories to a cloud model. Cloud review packages should contain only the problem, minimal sanitized diff/snippets, relevant approved guidance, validation results, and the precise review question.

Maintenance is fail-closed local: the OpenClaw `maintenance` agent uses an `ollama/*` allowlist and empty fallbacks. Do not place Kimi/OpenAI credentials in that agent's auth store or environment.

## Implemented controls

- All Compose ports bind to loopback.
- HTTP MCP requires a bearer token; unauthenticated mode is restricted to `APP_ENV=local`.
- Tokens come from environment variables and are not stored in example configuration.
- Origin protection, body limits, server timeouts, and constant-time token comparison are enabled.
- Worker/admin and production gateway images are static and non-root. The local analyzer-enabled gateway is non-root but includes Git and Go because `go/packages` needs the toolchain.
- Artifacts use SHA-256 addressing, atomic publication, and mode `0600`.
- SQL constraints enforce workflow states, relation types, confidence bounds, and no self-edges between repository records; recursive code calls remain valid graph facts.
- Only approved knowledge is vectorized and returned.
- MCP tool hints and Codex approval settings distinguish reads from writes.
- PostgreSQL outbox transactions prevent an approval without a durable indexing request.
- Code analysis resolves symlinks, enforces explicit filesystem roots and size caps, checks revisions, and rejects dirty worktrees by default.

## Production requirements

Before internet or enterprise exposure, add:

- TLS and an OAuth 2.1/OIDC-aware API gateway.
- Per-user/workload identities, scopes, audit subject propagation, and short-lived credentials.
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
| Stale/deleted vector result | PostgreSQL hydration and approved-status check. |
| Agent self-publishes its output | Write-tool prompts and accountable actor; organizational policy must enforce actor identity at the gateway. |
| Repository graph poisoning | Evidence required, constrained edge vocabulary, prompted upsert, SQL authority. |
| Repository prompt injection changes graph truth | Compiler-aware extraction is authoritative; OpenClaw/LLM interpretations are stored only as reviewable knowledge candidates. |
| Analyzer reads unrelated host files | Read-only narrow bind mount plus canonical-path allowlist; never mount a home directory or filesystem root. |
| Malicious or oversized repository exhausts resources | File/entity/relation caps locally; enterprise analyzers require sandbox CPU, memory, process, network, and deadline controls. |
| Token disclosure in Git | Environment references, `.env` ignored, examples contain placeholders only. |
| DNS rebinding/cross-origin local attack | SDK localhost protection plus Go cross-origin protection. |
| Worker crash loses indexing | Durable outbox, reclaimable locks, retries, idempotent Milvus upsert. |
| Cloud fallback leaks maintenance data | Strict per-agent local model and empty fallbacks. |

Report vulnerabilities according to [SECURITY.md](../SECURITY.md).
