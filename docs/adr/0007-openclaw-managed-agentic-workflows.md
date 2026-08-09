# ADR-0007: OpenClaw managed agentic workflows

**Status:** Proposed

## Context

The local platform provides role-oriented Make commands, bounded patch
verification, durable knowledge, review evidence, and explicit approval. Most
coordination is still manual. Automating it solely through agent prompts would
make state, retries, and role separation non-deterministic.

## Proposed decision

Use an OpenClaw TypeScript plugin as the orchestration adapter. Use managed Task
Flow for durable multi-step execution, Lobster for deterministic pipelines and
resumable gates, role-specific subagents/ACP tasks for model work, and
webhook/cron/heartbeat mechanisms only for their intended trigger classes.

PostgreSQL remains authoritative for workflow state, evidence, actor roles,
approval, and outbox events. The controller mirrors OpenClaw task/flow IDs into
PostgreSQL and every state transition is server-validated, versioned, and
idempotent. Milvus remains an approved-only projection.

Product Owner approval, confidential disclosure, high-risk QA, and material
Operations side effects remain human gates. Maintenance remains local-only.

Roles are responsibilities, not a fixed headcount. Each project selects a
`solo`, `team`, or `regulated` governance profile. A solo principal may hold all
four human role bindings and complete each gate in sequence. Team and regulated
profiles can require distinct principals or two-person approval at configured
gates. Every decision records both the authenticated principal and acting role.

Cerbos is the policy decision point for whether a principal may request an
action on a workflow resource. It does not replace authentication, OpenClaw
orchestration, the Go state machine, or PostgreSQL transactions. The Go service
fails closed when Cerbos is unavailable for a protected action and records the
decision correlation ID with the workflow event.

## Consequences

- A small TypeScript integration component is added while the production data
  plane remains Go.
- OpenClaw SQLite task/flow state is operational state, not canonical product
  workflow truth.
- Role prompts become usability aids; authenticated server-side scopes and SQL
  transition rules provide enforcement, with Cerbos externalizing contextual
  authorization policy.
- The platform can resume and audit long-running flows without allowing a model
  to improvise approval or retry semantics.
- Phase 1 identity and workflow contracts must ship before autonomous code
  changes are enabled.

See [OpenClaw Agentic Automation Design and Implementation Plan](../openclaw-agentic-automation-plan.md).
