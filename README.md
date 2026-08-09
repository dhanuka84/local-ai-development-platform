# Hybrid AI Software Engineering Platform

A runnable local-first platform that lets OpenClaw and Codex share reviewed software-engineering knowledge while keeping routine development and all maintenance inference on local Ollama models. Kimi K3 review and Codex cloud work/review are explicit lanes—not silent fallbacks.

The repository implements:

- A Go MCP server over Streamable HTTP or STDIO.
- PostgreSQL for durable workflow state, provenance, review gates, Git-repository graphs, and a transactional outbox.
- A headless, compiler-aware Go analyzer for revisioned symbols, calls, references, implementations, imports, and tests.
- Milvus for derived semantic indexes of approved knowledge, repository relationships, and selected code symbols.
- Ollama for local embeddings and local coding inference.
- Immutable SHA-256 prompt/output artifacts.
- An asynchronous indexing worker and administrative CLI.
- An independent bounded-work contract and disposable-clone patch verifier for OpenClaw local execution.
- Local Docker Compose deployment, Codex/OpenClaw examples, CI, and an enterprise migration path.

![Local-first implementation, remote review, and approved learning](docs/diagrams/hybrid-ai-review-learning-explainer.png)

See the [exact review-learning diagram](docs/diagrams/hybrid-ai-review-learning-loop.svg)
and the [full local deployment architecture](docs/diagrams/hybrid-ai-local-architecture.svg)
for implementation detail.

## The key design rule

PostgreSQL is authoritative. Milvus is a rebuildable projection.

Every generation is captured as a pending candidate with the original problem, reusable procedure, validation evidence, model/repository provenance, and immutable artifacts. Review feedback may revise a pending candidate, but only an explicit approval makes it searchable. PostgreSQL commits the approval and an outbox event atomically; the worker then embeds and upserts the approved item into Milvus.

Git-repository relationships use the same rule. Typed, evidence-backed edges are stored canonically in PostgreSQL and projected into Milvus so agents can use both exact graph traversal and semantic relationship search.

Source-code graphs follow a stricter dual-store rule: PostgreSQL owns every exact, revision-scoped entity and edge. Selected first-party symbols are embedded in Milvus under the same stable PostgreSQL entity UUID. A semantic hit is always hydrated from the active SQL snapshot before the agent traverses calls, references, implementations, imports, or tests.

## Technology choices

| Concern | Choice | Reason |
|---|---|---|
| MCP and data plane | Go 1.25 | Small static services, good concurrency, Tier-1 official MCP SDK. |
| Workflow and graph authority | PostgreSQL | Transactions, constraints, recursive queries, auditability, outbox. |
| Semantic/hybrid index | Milvus | Purpose-built vector scale and a direct standalone-to-distributed path. |
| Local inference | Ollama | Native OpenClaw integration and local `/api/embed`. |
| Local coding on GBX100/GB10 | `qwen3.6:35b` | Current open-weight agentic coding model with ample memory headroom on 128 GB. |
| Local embeddings | `embeddinggemma` | Small local embedding model; 768 dimensions by default. |
| Code analysis | Go packages, AST, and type information | Deterministic build-aware evidence without an LLM or editor bridge. |
| Cloud architecture review | `moonshot/kimi-k3` | Explicit, sanitized review subagent. |
| Cloud coding and independent code review | Codex/ChatGPT | MCP-connected implementation/review and reusable validated outcome capture. |

Go was selected over Python for the long-running production data plane and over Rust for faster team delivery. Python remains a good optional sidecar language for evaluation or ML experiments; Rust is appropriate only for a measured hot path. See [ADR-0001](docs/adr/0001-go-for-the-mcp-data-plane.md).

## Quick start

Prerequisites: Docker Compose v2, Git, and approximately 16 GB free RAM for the infrastructure. NVIDIA GPU use additionally requires the NVIDIA Container Toolkit. The application itself is multi-architecture; the intended host is the ASUS Ascent GX10/GB10.

1. Create protected local configuration with independent random secrets:

   ```bash
   make env-init
   ```

   The target never prints secrets and refuses to overwrite an existing `.env`.
   `CODEGRAPH_HOST_ROOT` selects the one host directory mounted read-only at
   `/workspace` for analysis. Use an absolute path to analyze another repository.

2. Start the stack. Use `up-gpu` on the GBX100/GB10 host:

   ```bash
   make up-gpu
   # or: make up
   ```

3. Optionally pull the recommended local coding model. The initial stack pulls only the embedding model:

   ```bash
   make pull-local-model
   ```

4. Verify it:

   ```bash
   make mcp-status
   ```

All published ports bind to `127.0.0.1`. Do not expose PostgreSQL, Milvus, Ollama, or the MCP endpoint directly to an untrusted network.

## Role-based commands

The same canonical Make targets are grouped into four operating workflows:

```bash
make help-operations
make help-development
make help-qa
make help-product-owner
```

Role-prefixed aliases such as `ops-start-gpu`, `dev-session-repo`,
`qa-candidates`, and `po-approve` expose who owns an action without duplicating
its implementation. Follow the complete [role workflows and handoffs](docs/role-workflows.md).

## Connect Codex

The current Codex configuration supports local STDIO and Streamable HTTP MCP servers. Codex can use a stored ChatGPT sign-in, so this workflow does not require `OPENAI_API_KEY`. The local MCP bearer token is a separate, non-billable secret used only between Codex and the gateway. Merge [examples/codex/config.toml](examples/codex/config.toml) into `~/.codex/config.toml` or use this repository's project-scoped `.codex/config.toml`, then export the token:

```bash
export HYBRID_AI_MCP_TOKEN='the AUTH_TOKEN value from .env'
codex mcp list
```

For two separate terminals, the repository provides a fail-fast workflow:

```bash
# Terminal 1: start the platform/MCP server, then follow its logs.
make mcp-start
# On the GBX100/GB10 host, use: make mcp-start-gpu
make mcp-logs

# Terminal 2: load AUTH_TOKEN from .env as HYBRID_AI_MCP_TOKEN and start Codex.
make codex
```

To work in another checkout while retaining this HTTP MCP server:

```bash
make codex-repo REPO=/absolute/path/to/software-repository
```

Inside Codex, `/mcp` should show `hybrid_knowledge` connected. The project configuration has `required = true`, so a new Codex session fails clearly if the server or token is unavailable.

Codex CLI can be the primary development session, not only a final reviewer.
Use MCP to retrieve approved knowledge and exact graphs, let Codex inspect and
change the selected repository, run local validation, and capture the validated
outcome with `generation_capture`. It remains pending until separately
approved; once indexed, Ollama can retrieve the generalized solution for a
similar future task. Because Codex inference is cloud-backed, this path follows
the same disclosure rules as any other cloud development or review request and
is never used by the maintenance profile.

For authentication boundaries, first-time setup, `/mcp verbose` verification, token rotation, and troubleshooting, follow the [operations runbook](docs/operations.md).

The STDIO alternative is in [examples/codex/config-stdio.toml](examples/codex/config-stdio.toml). Codex is configured to prompt for write tools, with an explicit prompt for the approval decision tool. See the official [Codex MCP documentation](https://learn.chatgpt.com/docs/extend/mcp) and OpenAI's [MCP server guide](https://developers.openai.com/plugins/build/mcp-server/).

## Connect OpenClaw, Ollama, and Kimi

This repository targets OpenClaw `2026.7.1` or newer.

1. Configure local Ollama using its native URL—never `/v1` for OpenClaw tool calling:

   ```bash
   export OLLAMA_API_KEY=ollama-local
   openclaw onboard --non-interactive \
     --auth-choice ollama \
     --custom-base-url http://127.0.0.1:11434 \
     --custom-model-id qwen3.6:35b \
     --accept-risk
   ```

2. Install the official Moonshot provider and enter the Kimi API key only in the onboarding wizard:

   ```bash
   openclaw plugins install @openclaw/moonshot-provider
   openclaw gateway restart
   openclaw onboard --auth-choice moonshot-api-key
   openclaw models set moonshot/kimi-k3
   ```

3. Merge [examples/openclaw/openclaw.hybrid.json5](examples/openclaw/openclaw.hybrid.json5) into `~/.openclaw/openclaw.json`. It defines:

   - `developer`: local primary model, permitted to delegate explicitly to cloud review.
   - `maintenance`: local model only, empty fallbacks, and an `ollama/*` allowlist.
   - `cloud-review`: Kimi K3 with a read-only sandbox.
   - The MCP server with secret interpolation and a bounded tool list.

4. Export the MCP token and verify live tool discovery:

   ```bash
   export HYBRID_AI_MCP_TOKEN='the AUTH_TOKEN value from .env'
   openclaw mcp doctor hybridKnowledge --probe
   ```

Current official references: [Kimi K3 in OpenClaw](https://platform.kimi.ai/docs/guide/use-kimi-in-openclaw), [OpenClaw Ollama provider](https://docs.openclaw.ai/providers/ollama), and [OpenClaw MCP](https://docs.openclaw.ai/cli/mcp).

## Bounded local work and review learning

OpenClaw remains the task router. Routine work goes to local Ollama; Codex and
Kimi are explicit advisory reviewers. Delegated patches use the independent
[`hybrid-ai/work-packet/v1`](examples/openclaw/work-packet.example.json)
contract:

```bash
make workpacket-evaluate PACKET=/path/to/work-packet.json
make workpacket-verify PACKET=/path/to/work-packet.json PATCH=/path/to/change.patch
```

Verified local results may receive a sanitized remote review. Exact review
output and the context manifest are stored as immutable evidence; a proposed
improvement remains pending. Only locally validated, generalized, explicitly
approved improvements are embedded in Milvus for later local reuse.
Maintenance remains local-only and can retrieve approved lessons without
invoking a cloud model. See [Remote Review and Local Learning](docs/remote-review-learning.md).

The [capability scorecard](docs/cost-routing-evaluation.md) distinguishes what
is implemented, configured, partial, and still unmeasured. No cost or quality
percentage is claimed before the documented benchmark gates pass.

## MCP workflow

The usual software-development loop is:

```text
knowledge_search + repository_graph_get + code_symbol_search
  -> local implementation and validation
  -> generation_capture (pending)
  -> optional Kimi/Codex review_record (may revise pending content)
  -> human/policy knowledge_candidate_decide
  -> PostgreSQL outbox
  -> Ollama embedding worker
  -> Milvus approved index
```

Available tools:

| Tool | Effect |
|---|---|
| `platform_status` | Check dependency health. |
| `knowledge_search` | Search approved project knowledge; lexical fallback is reported explicitly. |
| `knowledge_get` | Fetch one approved item. |
| `generation_capture` | Store a run, immutable artifacts, provenance, procedure, validation, and pending candidate. |
| `review_record` | Save review provenance, findings, immutable raw-output/context-manifest artifacts; `revise` requires fresh local validation and updates only a pending candidate. |
| `knowledge_candidates_list` | List the review queue. |
| `knowledge_candidate_decide` | Approve or reject; approval queues vector indexing. |
| `repository_relation_upsert` | Store a typed, approved Git-repository edge and queue its vector projection. |
| `repository_graph_get` | Traverse exact SQL relationships to depth 1–5. |
| `repository_relation_search` | Semantically search relationship evidence in Milvus. |
| `code_repository_index` | Analyze an allowlisted local Go repository and atomically publish its SQL graph snapshot. |
| `code_symbol_search` | Discover selected symbols through Milvus, then hydrate the active PostgreSQL entity. |
| `code_graph_get` | Traverse the exact active SQL graph around a symbol to depth 1–5. |

Supported repository edge types are `depends_on`, `provides_api_to`, `deploys_with`, `shares_contract`, `fork_of`, `upstream_of`, `successor_of`, `contains`, and `related_to`.

## Repository layout

```text
cmd/                 gateway, indexing worker, and admin CLI
components/          code graph analyzer plus bounded-work policy/verifier
internal/            domain, services, PostgreSQL, Milvus, Ollama, MCP, HTTP
migrations/          embedded transactional SQL migrations
deploy/compose/      complete local stack and NVIDIA GPU overlay
deploy/kubernetes/   enterprise application-layer Kustomize base
examples/            Codex and OpenClaw configurations
docs/                architecture, implementation, security, operations, ADRs
```

## Development

```bash
make check
make build
docker compose --env-file .env.example -f deploy/compose/compose.yaml config --quiet
```

Native and STDIO code analysis requires Git and the Go toolchain. The local Compose gateway uses the `gateway-analyzer` image and a read-only `/workspace` mount. The production gateway target remains distroless; enterprise deployments should run analyzers in isolated queued workers.

The module uses the official MCP Go SDK `v1.7.0`, which supports MCP protocol `2026-07-28`, and the Milvus Go client `v2.6.5`. Milvus Standalone is pinned to `v2.6.21`, matching the official 2.6 deployment line. Review dependency and image updates through CI rather than following floating `latest` tags.

## Enterprise path

The local and enterprise versions keep the same contracts. Replace only deployment implementations:

- One gateway becomes stateless replicas behind an OIDC-aware API gateway.
- PostgreSQL becomes managed HA PostgreSQL with PITR and read replicas.
- Milvus Standalone becomes Milvus Distributed; rebuild projections from PostgreSQL when necessary.
- The local artifact volume becomes versioned, encrypted S3-compatible storage.
- The in-process outbox poller can become CDC into Kafka/NATS when throughput justifies it.
- Static bearer tokens become workload identity and short-lived OAuth tokens.

See [enterprise-deployment.md](docs/enterprise-deployment.md) and the [enterprise architecture image](docs/diagrams/hybrid-ai-enterprise-architecture.png).

## Documentation

- [Implementation guide](docs/implementation-guide.md)
- [Remote review and local learning](docs/remote-review-learning.md)
- [Role workflows and Make commands](docs/role-workflows.md)
- [Routing capability and benchmark scorecard](docs/cost-routing-evaluation.md)
- [Operations runbook](docs/operations.md)
- [Security model](docs/security.md)
- [Enterprise deployment](docs/enterprise-deployment.md)
- [Architecture diagrams and Mermaid sources](docs/diagrams/README.md)
- [Architecture decisions](docs/adr/)

## License

The platform is MIT-licensed except for files under `components/codegraph`, which are MPL-2.0 as documented in that component's `LICENSE` and `NOTICE`. See [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
