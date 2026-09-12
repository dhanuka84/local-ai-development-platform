# ADR-0011: Scoped autonomy and local functional acceptance

- Status: Accepted for the local development workflow
- Date: 2026-09-12
- Clarifies: [ADR-0003](0003-reviewed-knowledge-promotion.md) and
  [ADR-0010](0010-rag-first-atomic-task-checkpoints.md)

## Context

Repeated client confirmations were stopping assigned local work even when the
operator already held the necessary roles. The user authorized autonomous
implementation, validation and definition publication, while explicitly keeping
generated KB entries pending. Some checklist items require live credentials,
measured adoption windows or a specified enterprise deployment.

## Decision

Standing task authorization covers necessary local implementation, fixes,
tests, recovery, indexing and validated domain/capability/metric definition
publication. Definition decisions still require the authenticated operator's
authority, exact version/hash, successful registry validation, freshness and an
immutable reason accurately attributing the delegated task.

Generated KB entries are different: a reusable fix, procedure or recommendation
is captured as pending. Publication requires trusted local validation, current
source evidence and the user's explicit decision on that exact candidate
version. A model review, general task instruction or client setting is not that
decision. `AUTO_APPROVE_LOCAL=true` is rejected in every environment.

The default local human has Development, QA, Product Owner and Operations roles.
The controller and short-lived task delegates retain their narrower workload
identities. Client unattended settings do not bypass authentication, Cerbos,
service invariants, database constraints or audit evidence.

`VALIDATED_REUSE_COMPLETED` allows a task with eligible recorded context and a
passing trusted validation report to complete while its new candidate remains
pending. The new-knowledge path retains promotion and exact Milvus UUID
read-back before completion. Provider and maintenance rules remain intact.

`make agent-ready-functional` is the current local acceptance target. Each
checklist row has a functional criterion and named automated evidence.
Broader rollout/adoption requirements stay visible as deferred acceptance.
Missing, skipped or failed required tests, and any failed suite, fail the run.
Deferral is never evidence of successful deployment or publication.

## Consequences and evidence

- Assigned work continues without repeated permission questions. Pending KB
  decisions and unavailable deployment inputs do not stop independent work.
- Original delivery checkboxes remain evidence-based; 28/28 functional coverage
  does not rewrite the original 22/28 rollout register.
- Runtime tests exercise the actual default operator, workload denials, trusted
  validation, exact-version decisions, stale-source withdrawal, task expiry and
  revocation, definition revalidation and restart recovery.
- Synthetic approvals remain confined to disposable tests. No real candidate
  or enterprise deployment is approved by this ADR.

See the [E2E guide](../agent-ready-e2e.md),
[validation receipt](../agent-ready-validation-20260912.md#functional-acceptance-continuation),
[operating decisions](../agent-ready-operating-decisions.md) and
[autonomous local operation](../operations.md#autonomous-local-operation).
