# ADR-0008: Cerbos for contextual authorization

**Status:** Proposed

## Context

The agentic platform needs to authorize humans, OpenClaw controllers, local
agents, remote reviewers, and Operations workloads across project-scoped
workflow states. It must support one person holding every role in a `solo`
project while enforcing selected distinct-principal or two-person gates in
`team` and `regulated` projects.

Hard-coding this matrix in OpenClaw prompts or Go handlers would mix
orchestration, authorization, and business-state validation. OpenClaw tool
filters are useful defense in depth but are not the authoritative enforcement
point.

## Proposed decision

Use the open-source Cerbos PDP as a stateless, internal authorization service.
The Go MCP gateway is the policy enforcement point:

1. authenticate the caller and derive the principal ID;
2. load principal bindings and resource facts from PostgreSQL;
3. ask Cerbos whether the action is allowed;
4. validate the workflow state, evidence, version, and idempotency key in Go;
5. commit the transition and authorization correlation atomically in
   PostgreSQL.

Cerbos policies and tests live in Git. Protected writes fail closed if the PDP
is unavailable. The PDP is internal-only and its image is version/digest
pinned. Decision logs are retained, while PostgreSQL remains the canonical
workflow audit trail.

The project governance profile is passed as trusted resource context:

- `solo`: one human principal may possess all four role bindings and perform
  separate role transitions;
- `team`: role overlap is allowed, with optional distinct-principal gates;
- `regulated`: configured separation-of-duty and two-person gates are
  mandatory.

## Boundaries

Cerbos does not:

- authenticate MCP clients or validate OIDC tokens;
- orchestrate agents or resume OpenClaw workflows;
- store project, workflow, graph, or knowledge truth;
- prove that QA evidence is correct;
- commit state transitions or count unique approvals;
- replace PostgreSQL row/tenant isolation where that is required.

## Reference implementation

The [local-secure-rag-invoice](https://github.com/dhanuka84/local-secure-rag-invoice)
project demonstrates a workflow invoking Cerbos before promoting a learned
template. We retain that conductor → policy check → guarded promotion seam.
This platform replaces client-supplied roles, optional fail-open behavior,
floating images, public PDP ports, and cache mutation with authenticated
principals, fail-closed decisions, pinned internal deployment, policy tests,
and versioned PostgreSQL transactions.

## Consequences

- The authorization matrix can evolve independently and be reviewed/tested as
  policy-as-code.
- Solo operation and enterprise separation use the same application path.
- Every protected gateway action adds a local PDP call; timeout, health, and
  outage behavior require explicit tests and metrics.
- Trusted context construction becomes security-critical and must never accept
  principal roles or governance facts from model output.
- Cerbos Hub may later manage policy build/distribution and unified decision
  logs, but the self-hosted open-source PDP is sufficient for the local phase.

Relevant upstream contracts are the [Cerbos evaluation
model](https://docs.cerbos.dev/cerbos/latest/policies/evaluation.html),
[policy tests](https://docs.cerbos.dev/cerbos/latest/tutorial/04_testing-policies.html),
[decision logging](https://docs.cerbos.dev/cerbos/latest/policies/debugging.html),
and [CheckResources/Go SDK API](https://docs.cerbos.dev/cerbos/latest/api/index.html).

See [OpenClaw Agentic Automation Design and Implementation Plan](../openclaw-agentic-automation-plan.md).
