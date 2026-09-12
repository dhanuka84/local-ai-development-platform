# ADR-0003: Reviewed knowledge promotion

- Status: Accepted
- Date: 2026-08-08

## Decision

Capture model outputs as pending candidates. Preserve exact reviewer output and
the disclosed-context manifest as immutable artifacts referenced by the review
record. Permit model/human review to annotate or revise pending candidates, but
require local validation and a separate accountable approval before indexing
and retrieval.

## Rationale

Cloud-model quality does not make output authoritative. Capturing raw responses directly into retrieval creates self-reinforcing errors and prompt-injection persistence. A promotion state machine preserves useful output while requiring validation, provenance, and ownership.

## Consequences

- Local agents retrieve only approved records.
- Review and approval are distinct MCP operations.
- Raw review evidence remains auditable but is not embedded automatically.
- Revision of approved records requires a future new-version workflow; the current service only revises pending records.
- Updated 2026-09-12: `AUTO_APPROVE_LOCAL=true` is rejected in every environment.
  Generated KB entries require the user's explicit exact-version decision.
  Validated domain/capability/metric definitions use a separate delegated
  authorization rule; see [ADR-0011](0011-scoped-autonomy-and-functional-acceptance.md).
