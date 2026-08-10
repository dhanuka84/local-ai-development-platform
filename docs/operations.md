# Operations Runbook

Use this document to set up, start, verify, and stop the local platform. The
commands are safe defaults for a single developer. Team and regulated identity
options are explained after the local setup. For unfamiliar terms, use the
[plain-English glossary](glossary.md).

Role-specific commands and the Development → QA → Product Owner handoff are
defined in [Role Workflows and Make Commands](role-workflows.md). Use
`make help-operations` for the concise Operations sequence.

The [OpenClaw automation plan](openclaw-agentic-automation-plan.md) explains
which automated stages are ready and which are still planned. The local stack
includes Cerbos. The Go gateway identifies the caller from its bearer token,
asks Cerbos whether the action is allowed, and records the policy decision with
the workflow event. `make authz-policy-test` checks both allow and deny cases.

## Operating model and authentication boundaries

Codex CLI, the local MCP gateway, Ollama, and Kimi have separate login and
security boundaries. Do not reuse one credential for another service.

| Boundary | Credential | Required for this deployment | Billing/data boundary |
|---|---|---:|---|
| Codex CLI -> OpenAI models | ChatGPT sign-in | Yes for the recommended Codex workflow | Uses the signed-in ChatGPT account/workspace entitlements and limits. It does not require OpenAI Platform API-key billing. |
| Codex CLI -> local MCP gateway | `HYBRID_AI_MCP_TOKEN` in the Codex process | Yes in HTTP mode | Local bearer secret; it must equal the gateway's `AUTH_TOKEN` in `.env`. It is not an OpenAI token and has no model-usage charge. |
| OpenClaw -> local MCP gateway | `CONTROLLER_AUTH_TOKEN` | Yes for orchestration | Separate non-human controller identity. It cannot perform QA/Product Owner human gates. |
| OpenClaw -> Kimi cloud | Moonshot/Kimi API key | Only for an explicitly selected cloud-review workflow | Moonshot cloud billing and disclosure boundary. Store it through OpenClaw's provider onboarding, not in this repository's `.env`. |
| OpenClaw -> Ollama | No real cloud credential | Yes for local inference | Local-machine inference. A provider may require a non-secret placeholder such as `OLLAMA_API_KEY=ollama-local`. |

This runbook uses ChatGPT sign-in for Codex, so `OPENAI_API_KEY` is not needed.
Codex still uses a cloud model even though its CLI runs locally and connects to
the local MCP server. Local-only maintenance must use OpenClaw and Ollama, not
Codex or Kimi.

See the official [Codex authentication](https://learn.chatgpt.com/docs/auth) and [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp) documentation.

### Default solo principal and multi-principal override

When `AUTH_PRINCIPALS_JSON` is not set, `AUTH_TOKEN` creates
`human:local-developer` with `development`, `qa`, `product_owner`, and
`operations` roles on `*` projects. `CONTROLLER_AUTH_TOKEN` separately
creates `agent:openclaw-controller` with only the non-human `controller` role.
This is the default single-developer setup.

For team or regulated deployments, set `AUTH_PRINCIPALS_JSON` to the complete
credential definition instead. It takes precedence over both legacy token
bootstraps, so include every required human and workload principal, including
the controller. Each entry has `id`, `display_name`, `token`, `human`, `roles`,
and `project_ids`; the controller entry's token must match the separately
supplied `CONTROLLER_AUTH_TOKEN` used by OpenClaw. Treat the entire JSON value as
a secret and supply it through the deployment secret manager rather than
committing it or entering it in shell history. Project governance remains `solo` unless its
`project_governance_policies` row is explicitly changed to `team` or
`regulated` through a reviewed administrative migration.

## First-time workstation setup

Prerequisites are Docker with Compose v2, Git, Codex CLI, and enough memory for the selected Ollama model. GPU mode additionally requires a supported NVIDIA driver and the NVIDIA Container Toolkit.

For the end-to-end workstation procedure—including Docker group repair,
multi-repository cloning, an explicitly destructive volume-free rebuild,
indexing requests, SQL verification, and measured timing—use the
[local setup and multi-repository indexing runbook](local-setup-and-indexing.md).

1. Create the local environment file:

   ```bash
   make env-init
   ```

   This generates separate random `AUTH_TOKEN`, `CONTROLLER_AUTH_TOKEN`, and
   `POSTGRES_PASSWORD` values,
   writes `.env` with mode `0600`, sets `CODEGRAPH_HOST_ROOT` to this checkout,
   and never prints the secrets. It refuses to overwrite an existing `.env`.
   Edit `CODEGRAPH_HOST_ROOT` if the analyzer needs a different narrow host
   repository or parent directory; it is mounted read-only at `/workspace`.
   Never commit `.env`.

2. Authenticate Codex with ChatGPT once:

   ```bash
   make codex-login
   make codex-check
   ```

   Complete the browser flow. The expected status is `Logged in using ChatGPT`. An OpenAI Platform API key is optional and is not part of this workflow. On a shared machine, use `codex logout` when access should be removed.

3. Validate the complete local configuration:

   ```bash
   make preflight
   ```

   This checks local tools, secrets, Compose configuration, Codex
   authentication, and MCP registration. Run it from the repository root. On
   the first interactive Codex launch, accept the repository trust prompt only
   after reviewing the checkout. A trusted project loads
   [`.codex/config.toml`](../.codex/config.toml), which registers
   `hybrid_knowledge` at `http://127.0.0.1:8080/mcp`. Seeing `enabled` confirms
   configuration, not a live connection.

## Separate-terminal startup

The MCP server is a long-running process in the Compose stack. Codex is a separate client process and can be started, stopped, or restarted independently.

Terminal 1 — start the local platform and MCP gateway:

```bash
# CPU fallback
make mcp-start

# Or, on the GBX100/GB10 host with NVIDIA Container Toolkit
make mcp-start-gpu

# Follow the gateway and indexing worker logs
make mcp-logs
```

Pressing `Ctrl-C` while following logs stops only the log command; the containers continue running in the background.

The initial stack start pulls the local embedding model. Before using an OpenClaw development or maintenance agent, pull the configured local chat model once:

```bash
make pull-local-model
```

Terminal 2 — start Codex from the repository root:

```bash
make codex-check
make codex
```

`make codex` reads only `AUTH_TOKEN` from `.env`, passes it to the child Codex process as `HYBRID_AI_MCP_TOKEN`, and does not print it. It does not set or require `OPENAI_API_KEY`; Codex continues to use its stored ChatGPT sign-in. Starting plain `codex` is also valid when `HYBRID_AI_MCP_TOKEN` is already exported in that shell.

Inside Codex, run:

```text
/mcp verbose
```

Confirm that `hybrid_knowledge` is connected and that its tools are visible. Then ask Codex to call `platform_status`; this validates a real tool round trip rather than only reading the local registration. The project configuration sets `required = true`, so Codex fails startup or resume when this enabled MCP server cannot initialize. Start the gateway before starting Codex.

The resulting connections are:

```text
Codex CLI -- stored ChatGPT sign-in --> OpenAI model
Codex CLI -- HYBRID_AI_MCP_TOKEN ---> local MCP gateway
local MCP gateway ------------------> PostgreSQL + Milvus + Ollama
```

Streamable HTTP is intentional for this two-process workflow. The [STDIO example](../examples/codex/config-stdio.toml) removes the HTTP bearer-token boundary, but Codex then launches and owns the MCP subprocess; it does not satisfy the requirement to keep the MCP server running independently in another terminal.

To use the same independently running MCP server while Codex works in another
repository, do not copy secrets or platform configuration into that repository:

```bash
make codex-repo REPO=/absolute/path/to/software-repository
```

The target resolves the repository path, loads the bearer token without
printing it, and supplies the HTTP MCP configuration as one-process Codex CLI
overrides. The target repository's trusted Codex configuration still applies.

## Make command reference

| Command | Purpose |
|---|---|
| `make env-init` | Create a protected `.env` with independent random local secrets; refuse overwrite. |
| `make mcp-preflight` | Validate Docker, Git, curl, secrets, and Compose configuration. |
| `make preflight` | Run MCP preflight plus Codex authentication/registration checks. |
| `make mcp-start` | Start the CPU platform and HTTP MCP gateway. |
| `make mcp-start-gpu` | Start the NVIDIA platform and HTTP MCP gateway. |
| `make mcp-status` | Show containers and call `healthz` and `readyz`. |
| `make mcp-logs` | Follow gateway and worker logs. |
| `make codex-login` | Start the interactive ChatGPT sign-in flow. |
| `make codex-check` | Show Codex login status and MCP registration. |
| `make codex` | Start Codex in this checkout with the local bearer token. |
| `make codex-repo REPO=/abs/path` | Start Codex in another repository with this HTTP MCP server. |
| `make workpacket-evaluate PACKET=...` | Evaluate classification, risk, disclosure, scope, and limits. |
| `make workpacket-verify PACKET=... PATCH=...` | Verify a candidate patch and its exact checks in a disposable clone. |
| `make openclaw-plugin-deps` | Install the controller plugin's pinned dependencies. |
| `make openclaw-plugin-check` | Type-check/test the plugin, validate its metadata, and audit production dependencies. |
| `make openclaw-config-check` | Validate the complete example against the pinned OpenClaw schema. |
| `make openclaw-plugin-install` | Build and install/update the local controller plugin. |
| `make openclaw-start` | Start the OpenClaw gateway with only the controller credential loaded. |
| `make pull-local-model` | Pull the configured Ollama coding model. |
| `make mcp-stop` | Stop the platform while retaining named volumes. |

## OpenClaw client operation

OpenClaw connects to the same loopback MCP endpoint with the non-human
`CONTROLLER_AUTH_TOKEN`. Install and validate the controller plugin, then start
the OpenClaw gateway without printing or manually exporting the token:

```bash
make openclaw-plugin-deps
make openclaw-plugin-check
make openclaw-plugin-install
make openclaw-start
```

`make openclaw-start` loads only `CONTROLLER_AUTH_TOKEN` into the OpenClaw
process. The controller can coordinate state and run bounded work, but it cannot
perform human QA or Product Owner decisions. Use `make codex` or another local
human approval surface with `AUTH_TOKEN` at those gates.

Use the `developer` agent for local development with explicit cloud-review escalation. Use the `maintenance` agent for local-only operation; its one-model per-agent catalog and empty fallbacks prevent Kimi or Codex model selection, including stored session overrides. Configure the Moonshot credential only in the `cloud-review` provider/agent through interactive OpenClaw onboarding. Do not put the Moonshot key in `.env`, Codex configuration, shell history, or the MCP service.

## Normal checks

```bash
make doctor
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
docker compose --env-file .env -f deploy/compose/compose.yaml ps
make logs
```

`healthz` proves that the gateway process can answer. `readyz` checks PostgreSQL,
Ollama, Milvus, and Cerbos with a three-second request budget.

## Start and stop

```bash
make mcp-start-gpu   # GBX100/GB10 with NVIDIA Container Toolkit
make mcp-start       # CPU-only fallback
make mcp-stop        # retains named volumes
```

`make up-gpu` and `make up` are equivalent general stack-start aliases. `make mcp-start-gpu` and `make mcp-start` make the two-terminal intent explicit.

Do not use `docker compose down -v` unless deletion of all local database, vector, model, and artifact data is explicitly intended and independently backed up.

## Candidate approval

Agents can use MCP with an approval prompt. Operators can also use the local CLI:

```bash
make po-candidate-get ID=<candidate-uuid>
make po-approve ID=<candidate-uuid>
# or: make po-reject ID=<candidate-uuid>
```

The CLI authenticates `AUTH_TOKEN`, derives the accountable human identity, and
asks Cerbos before writing the decision. Approval queues indexing. Search may
not return the item until the worker completes the event.

## Local implementation and remote review

Two development entry points are supported:

- **OpenClaw/Ollama-first:** local implementation, deterministic verification,
  then explicit Codex/Kimi review.
- **Codex-first:** start with `make codex` or `make codex-repo`, retrieve through
  MCP, implement and validate in Codex, then use `generation_capture` so the
  approved procedure can later be reused locally.

Codex-first development is still cloud inference and must satisfy the cloud
disclosure policy. It is not permitted for local-only maintenance.

For OpenClaw-delegated patch work:

1. Copy and specialize
   [`examples/openclaw/work-packet.example.json`](../examples/openclaw/work-packet.example.json).
2. Run `make workpacket-evaluate PACKET=...` before local worker execution.
3. Have the local Ollama worker return a unified patch instead of directly
   mutating the authoritative checkout.
4. Run `make workpacket-verify PACKET=... PATCH=...`.
5. For development work requiring independent review, construct a minimal,
   sanitized package and explicitly invoke Codex, Kimi, or both.
6. Store the exact response in `raw_output`, normalized findings in `comments`,
   and the sanitized disclosure manifest in `context_manifest` through
   `review_record`; reproduce accepted recommendations locally and rerun
   deterministic checks.
7. Capture the improved result as a pending candidate. Approve only generalized,
   validated content; only then does the outbox worker embed it in Milvus.

Raw remote-review text is evidence, not immediately reusable truth. Maintenance
packets are local-only and cannot request remote review, but may retrieve
previously approved review improvements through the local knowledge service.

The MCP request shape, evidence/knowledge distinction, and provider-selection
rules are in [Remote Review and Local Learning](remote-review-learning.md).
After a successful call, retain the returned artifact hashes in the run trace.
They prove which exact response and disclosure manifest belong to the review;
they are not Milvus document IDs.

Operationally verify a review learning cycle by checking:

1. The `review_record` result contains `review_artifact.sha256` and, when a
   manifest was supplied, `context_manifest_artifact.sha256`.
2. The review row references those hashes and the candidate remains `pending`.
3. Local reproduction and validation evidence are attached to the candidate.
4. An accountable actor approves the candidate separately.
5. The outbox worker completes `knowledge.upsert` before semantic search is
   expected to return the improvement.

## Rebuild Milvus

Milvus is derived. After restoring or replacing it:

```bash
make milvus-init
make ops-reindex
```

Monitor worker logs until the outbox drains. Reindex includes approved knowledge, Git-repository relationships, and selected first-party code entities from every active repository snapshot.

## Analyze a repository

Set `CODEGRAPH_HOST_ROOT` in `.env` to the host repository or a narrow parent directory exposed read-only to the Compose gateway, then rebuild/restart the gateway. Invoke `code_repository_index` through Codex/OpenClaw with `repository_path=/workspace` (or a child path) and an explicit write approval. Supply both the expected checked-out `branch` and commit `revision` whenever possible, and leave `allow_dirty=false` for reproducible snapshots. Every stored analysis and returned symbol reports its repository name, branch, and revision.

The [local indexing runbook](local-setup-and-indexing.md) provides a complete
request body, a safe order for sibling repositories, active-snapshot SQL, and
outbox monitoring commands.

For an organization cloned as sibling directories, mount its parent directory once and index one repository per call. For example, set `CODEGRAPH_HOST_ROOT=/home/user/projects/repositories`, restart with `make mcp-stop && make mcp-start-gpu`, then use child paths such as `/workspace/service-a` and `/workspace/web-app`. The router automatically selects and combines Go, Java/Kotlin, TypeScript/JavaScript, and Python providers. Large compiler-backed scans can take several minutes; the gateway allows 20 minutes for a response, so MCP clients should use a matching tool timeout while keeping ordinary network health checks short.

The source mount is read-only. Java/Kotlin, TypeScript/JavaScript, and Python analysis is performed in disposable copies inside the gateway. Maven and Gradle dependency resolution/build plugins can run in that container; npm lifecycle scripts are disabled; Python dependencies are not installed. Network access may therefore improve JVM/TypeScript resolution but should be disabled or proxied for restricted source. `scip-java` supports Kotlin through Gradle, not Maven-only Kotlin projects. Maven analysis activates a root `deploy` profile when one exists so source modules commonly omitted from the default reactor are included; it still runs `install`, never `deploy`, and disables release signing, source archives, and Javadoc generation. When local Maven projects depend on sibling artifacts, index the foundational/BOM repository first; its disposable Maven build installs artifacts into the dedicated `analyzer-cache` volume for later scans.

For native STDIO operation, set `CODEGRAPH_ALLOWED_ROOTS` to an OS path-list of permitted roots and install Git plus every required compiler/indexer. If dependency disclosure is prohibited, pre-populate language dependency caches and disable egress (for Go, set `GOPROXY=off`).

## Backup

Back up independently:

1. Git repositories and reviewed configuration.
2. PostgreSQL with `pg_dump` plus regular tested restore procedures.
3. Artifact storage, preserving paths and hashes.
4. OpenClaw/Codex configuration and credential stores using an encrypted secret backup.

Milvus backup is optional when PostgreSQL and the embedding model/version are preserved; rebuilding may be slower than restoring at enterprise scale. Ollama model files are also replaceable but expensive to download.

## Credential rotation and revocation

For the local MCP credentials:

1. Stop or quiesce MCP clients.
2. Generate new, different random values for `AUTH_TOKEN` and
   `CONTROLLER_AUTH_TOKEN` in `.env`.
3. Recreate the gateway with `make mcp-stop`, followed by `make mcp-start` or `make mcp-start-gpu`.
4. Restart Codex with `make codex` and OpenClaw with `make openclaw-start`.
5. Verify `/mcp verbose`, `platform_status`, and
   `make openclaw-plugin-doctor` as applicable.

The old bearer credentials become invalid after the gateway restarts. Rotate
the Moonshot key through its provider controls and OpenClaw onboarding
independently. Use `codex logout` to revoke the workstation's stored ChatGPT
session; do not delete or replace the local MCP token merely to sign Codex out
of OpenAI.

## Recovery order

1. Restore PostgreSQL and artifacts.
2. Apply any newer migrations.
3. Start Ollama and verify the exact embedding model.
4. Start Milvus and initialize the configured collection.
5. Reindex and start workers.
6. Start the MCP gateway and verify readiness.
7. Reconnect Codex/OpenClaw clients.

## Common failures

| Symptom | Likely cause | Action |
|---|---|---|
| `codex login status` reports signed out | No valid Codex session | Run `codex login` and complete ChatGPT browser sign-in; no OpenAI API key is needed. |
| Codex is billed through the OpenAI API | Codex was deliberately signed in using an API key | Run `codex logout`, then `codex login` and choose ChatGPT sign-in if subscription/workspace access is intended. |
| `codex mcp list` says `enabled`, but tools do not work | The command confirmed registration only | Start the gateway, launch with `make codex`, and inspect `/mcp verbose`. |
| Codex fails during startup/resume on `hybrid_knowledge` | The required MCP server is down, unreachable, or rejected its token | Start Terminal 1 first; check `curl http://127.0.0.1:8080/healthz`, then verify the token mapping and gateway logs. |
| `make codex` reports missing `.env` or `AUTH_TOKEN` | Local configuration is absent or still contains `CHANGE_ME` | Copy `.env.example`, set real secrets, and retry. |
| `AUTH_TOKEN is required` | HTTP mode without a token | Set a long random token in `.env`; export the matching client variable. |
| `CONTROLLER_AUTH_TOKEN is required` | OpenClaw controller credential is absent | Run `make env-init` or set a second random token that differs from `AUTH_TOKEN`, recreate the gateway, and restart OpenClaw. |
| MCP returns unauthorized | `HYBRID_AI_MCP_TOKEN` does not exactly match `.env` `AUTH_TOKEN` | Restart Codex with `make codex`; after rotation, recreate the gateway and restart every client. |
| OpenClaw MCP calls return unauthorized | Its controller credential is stale or missing | Restart with `make openclaw-start`; do not substitute the human `AUTH_TOKEN`. |
| OpenClaw is denied at QA/Product Owner gate | Expected human-only policy enforcement | Complete the gate through an authenticated human Codex/local client; do not grant the controller human roles. |
| Search reports lexical fallback | Ollama or Milvus unavailable | Run `make doctor`; inspect service logs; local exact search remains usable. |
| Embedding dimension error | Model and `EMBEDDING_DIMENSION` mismatch | Create a versioned collection with the detected dimension and reindex. |
| Approved item is absent | Outbox lag or worker failure | Inspect worker logs and `outbox_events.last_error`; do not write Milvus manually. |
| OpenClaw emits raw tool JSON | Ollama configured with `/v1` | Use native `baseUrl: http://host:11434` and `api: ollama`. |
| Codex MCP tools report a missing token | Token env not exported to the Codex process | Start with `make codex` or export `HYBRID_AI_MCP_TOKEN`; do not put the token value in TOML. |
| Repository graph is empty | Root does not exactly match UUID/URL/name | Use the canonical URL returned by relation upsert. |
| Code analysis path is rejected | Path is outside the resolved allowlist or its bind mount | Update `CODEGRAPH_HOST_ROOT`/`CODEGRAPH_ALLOWED_ROOTS`; never expose a broad filesystem root. |
| Code analysis reports package/build errors | A required toolchain, dependency, or build configuration is unavailable | Inspect gateway logs; populate the relevant cache/vendor tree and confirm Maven/Gradle, tsconfig/package metadata, or Python source roots are valid. |
| Symbol search uses lexical fallback | Symbol vectors are queued or Ollama/Milvus is down | Inspect `code_entity.upsert` events and worker logs; SQL graph traversal remains authoritative. |

## Cloud-disclosure checks

Before invoking Codex or Kimi, classify and minimize the context package. Never send credentials, personal data, production dumps, restricted source code, or unrestricted repository contents to a cloud model. Record which provider and model received the approved package. Validate all cloud-generated recommendations locally, capture useful results as pending knowledge, and require an accountable approval before promotion.

For maintenance tasks, verify that OpenClaw selects the local `maintenance` agent, the active model is `ollama/*`, fallbacks are empty, and no Moonshot credential is usable by that agent. Do not use Codex for that local-only maintenance session, because ChatGPT-authenticated Codex still performs inference in OpenAI's cloud.

## Upgrade policy

Pin Go modules and images. Upgrade one boundary at a time in a test environment:

1. Back up state and record current versions.
2. Run unit, race, vet, and MCP discovery tests.
3. Verify migration and rollback behavior.
4. Re-run a retrieval evaluation set.
5. Deploy locally/canary, then promote.

Milvus Standalone-to-major-version upgrades require the vendor procedure and a tested backup. A collection rebuild is often safer because the index is derived.
