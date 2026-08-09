# Role Workflows and Make Commands

## Purpose

The Makefile exposes one canonical command for each action and role-prefixed
aliases that make ownership visible. Start with:

```bash
make help-operations
make help-development
make help-qa
make help-product-owner
```

The aliases do not create authorization boundaries. The local profile still
uses one MCP bearer token and a PostgreSQL actor string. Role separation is a
required operating procedure until enterprise OIDC identities, scopes, and a
separate QA gate are implemented.

## Operations

Operations owns platform availability, data safety, credentials, migrations,
and projection health.

### One-time host setup

```bash
make env-init
# Review .env, especially CODEGRAPH_HOST_ROOT and model settings.
make mcp-preflight
make ops-start-gpu       # or: make ops-start
make pull-local-model
make ops-status
```

Compose startup applies PostgreSQL migrations and initializes Milvus before the
gateway and worker become ready.

### Normal platform session

```bash
make ops-start-gpu       # or: make ops-start
make ops-status
make ops-logs            # optional; follows logs until interrupted
make ops-stop            # retains all named volumes
```

### Administration and recovery

```bash
make migrate
make milvus-init
make ops-doctor
make ops-reindex
```

`migrate`, `milvus-init`, `doctor`, candidate commands, and `reindex` execute
through the Compose admin image. They therefore use the same database password
and service-network addresses as the running platform without exporting a
database URL into the developer shell.

Operations must not approve technical or product knowledge. After Product
Owner approval, Operations monitors the outbox worker and projection freshness.

## Development

Development owns task analysis, implementation, local checks, cloud-context
minimization, review disposition, and candidate capture.

### One-time Codex setup

```bash
make codex-login
make preflight
```

### Per-task workflow

```bash
make dev-session
# Or work in another checkout:
make dev-session-repo REPO=/absolute/path/to/repository
```

Inside Codex or OpenClaw, Development searches approved knowledge, repository
relationships, and code symbols before substantial work. For an
OpenClaw-delegated patch:

```bash
make dev-policy-check PACKET=/path/to/work-packet.json
make dev-patch-verify \
  PACKET=/path/to/work-packet.json \
  PATCH=/path/to/candidate.patch
make dev-check
```

After successful validation, Development uses `generation_capture` through MCP
and passes the pending candidate UUID, work packet, patch/diff, and evidence to
QA. Development may record Codex/Kimi output with `review_record`, but cannot
run `po-approve` on its own candidate.

## QA

QA independently verifies implementation scope, tests, review dispositions,
and reusable-knowledge quality.

### Candidate and source inspection

```bash
make qa-candidates PROJECT=my-product LIMIT=25
make qa-candidate-get ID=<candidate-uuid>
make qa-session-repo REPO=/absolute/path/to/repository
```

### Independent verification

```bash
make qa-patch-verify \
  PACKET=/path/to/work-packet.json \
  PATCH=/path/to/candidate.patch
make qa-check
```

QA records its independent verdict and findings with `review_record` through
MCP. A `revise` verdict requires locally reproduced improved content and fresh
validation evidence. The review record does not perform final approval.

When validation passes, QA hands the candidate UUID and review evidence to the
Product Owner. When it fails, QA records `reject`, `revise`, or `comment` review
feedback and returns the work to Development.

## Product Owner

The Product Owner owns business acceptance and the final decision to publish a
validated candidate as reusable organizational knowledge.

### Review the pending candidate

```bash
make po-candidates PROJECT=my-product LIMIT=25
make po-candidate-get ID=<candidate-uuid>
```

Before deciding, confirm:

- acceptance criteria and intended product behavior;
- QA's independent review and validation evidence;
- applicability, exclusions, and rollback guidance;
- data-classification and cloud-disclosure compliance;
- that the candidate is generalized and not merely raw model output.

### Make the explicit decision

```bash
make po-approve ID=<candidate-uuid> ACTOR=<accountable-identity>
# or
make po-reject ID=<candidate-uuid> ACTOR=<accountable-identity>
```

Approval commits the knowledge decision and outbox intent in PostgreSQL.
Embedding and Milvus publication remain asynchronous. Product Owner must never
share or reuse another person's actor identity.

## Handoff sequence

```text
Operations
  platform ready
      ↓
Development
  work packet → implementation → local checks → generation_capture
      ↓ candidate UUID + patch + evidence
QA
  independent verification → review_record
      ↓ technically validated candidate UUID
Product Owner
  business acceptance → po-approve / po-reject
      ↓ committed outbox event
Operations
  monitor worker and semantic projection freshness
```

## Command ownership summary

| Role | Workflow commands | Canonical/supporting commands |
|---|---|---|
| Operations | `ops-start`, `ops-start-gpu`, `ops-status`, `ops-logs`, `ops-stop`, `ops-doctor`, `ops-reindex` | `env-init`, `mcp-preflight`, `migrate`, `milvus-init`, `up`, `up-gpu`, `down`, `logs`, `mcp-start`, `mcp-start-gpu`, `mcp-status`, `mcp-logs`, `mcp-stop`, `doctor`, `reindex`, `pull-local-model` |
| Development | `dev-session`, `dev-session-repo`, `dev-policy-check`, `dev-patch-verify`, `dev-check` | `codex-login`, `codex-check`, `codex`, `codex-repo`, `preflight`, `fmt`, `test`, `check`, `build`, `workpacket-build`, `workpacket-evaluate`, `workpacket-verify`, `diagram-review-loop`, `clean`, plus MCP retrieval/capture/review tools |
| QA | `qa-session`, `qa-session-repo`, `qa-patch-verify`, `qa-check`, `qa-candidates`, `qa-candidate-get` | `candidate-list`, `candidate-get`, `codex-check`, `test`, `check`, `workpacket-verify`, plus MCP `review_record` |
| Product Owner | `po-candidates`, `po-candidate-get`, `po-approve`, `po-reject` | `candidate-list`, `candidate-get`, `candidate-approve`, `candidate-reject` |

The four `help-*` commands are shared discovery commands. `fmt`, `test`,
`check`, and `build` are Development-owned repository operations that QA may
rerun independently. Canonical infrastructure names remain available for
backward compatibility, while role aliases should be preferred in runbooks and
audit evidence.

Shared read commands do not transfer accountability. In particular, QA reading
a candidate does not approve it, and Product Owner approval does not replace
QA's technical validation.
