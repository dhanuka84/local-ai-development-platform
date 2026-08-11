# ADR-0006: Bounded local execution with advisory cloud review

**Status:** Accepted

## Decision

Keep model routing and execution in OpenClaw. Keep the existing MCP gateway
focused on durable knowledge, evidence, review records, and graph retrieval.
Add an independently authored `components/workpacket` contract and verifier
that OpenClaw uses before accepting a local worker's patch.

The work packet records task class, data classification, risk categories,
local/cloud policy, base revision, allowed and forbidden files, rollback steps,
exact validation argv, and file/diff/patch limits. Patch verification happens
in a disposable clone and never modifies the source checkout. The verifier
rejects out-of-scope files, binary patches, excessive diffs, failed checks, and
check processes that mutate the candidate patch.

For governed atomic tasks, remote Codex review is a conditional, fail-closed
step after a RAG miss and local result. The exact response and disclosed-context manifest are stored as
content-addressed evidence referenced by the PostgreSQL review row. A proposed
improvement remains a pending candidate. It is embedded in Milvus only after
local validation, generalization, and an accountable PostgreSQL approval.
Maintenance and restricted-data packets fail closed without cloud review.
ADR-0010 adds the durable per-task queue, provider gates, and Milvus read-back
that enforce this sequencing.

## Why OpenClaw owns routing

OpenClaw already owns agent identity, local/cloud model selection, subagents,
workspace tools, and maintenance isolation. Adding a second production router
inside the knowledge MCP server would create conflicting decisions and mix
execution authority with durable knowledge authority.

The resulting boundary is:

```text
OpenClaw       classify, route, execute, request review
workpacket     deterministic execution policy and patch verification
MCP gateway    retrieve/capture/review/promote durable knowledge
PostgreSQL     authoritative workflow and graph truth
Milvus         approved semantic discovery
```

## External project evaluation and licensing

The public CodexSaver project was evaluated as a product/architecture example.
At the inspected revision it did not contain an explicit license. It is not a
dependency, and no source file, schema, test, or implementation is copied or
translated into this repository. The implementation here follows this
platform's pre-existing OpenClaw, approval, data-classification, PostgreSQL,
Milvus, and maintenance requirements using conventional policy-validation and
Git verification mechanisms.

Do not vendor, translate, or adapt CodexSaver code unless the copyright holder
publishes a compatible license or grants written permission.

## Consequences

- Routine execution can stay on local Ollama while remote models focus on
  independent judgment.
- Local and remote responsibilities remain visible and auditable.
- A verified patch is not automatically approved knowledge or an approved
  production change.
- The local disposable clone limits accidental source-tree mutation but is not
  a security sandbox. Hard isolation requires an egress-denied container/VM,
  resource limits, and a constrained OS identity.
- Cost and quality claims require platform-specific benchmarks; no external
  project's marketing percentage is inherited.
