# ADR-0012: Bounded AI-native SDLC execution

**Status:** Implemented and locally validated, September 14, 2026.

The [AI-native expectations](../ai-native-sdlc-expectations.md) require accepted
product intent and current shared KB context to drive delivery and incident
recovery. Mirroring a workflow or verifying an already supplied patch does not
execute that loop. This decision extends [ADR-0006](0006-bounded-local-execution-and-cloud-review.md)
and [ADR-0007](0007-openclaw-managed-agentic-workflows.md) for the new profiles.

## Decision

PostgreSQL owns serial run/step state, immutable grants, exact context bindings,
lease fences, total budgets and effect receipts. The gateway supervises allowed
transitions. Separate builder, evaluator, delivery, diagnosis and remediation
workloads execute their assigned stages; each has an accountable human owner.
Model invocation stays in local Ollama workers with a checked model digest and
no cloud fallback. Versioned packages limit tools, resources and output shape.

Each stage hydrates authorized current semantic and structural KB content.
Accepted code bindings include exact active revisions and symbols. Native
readers retain bounded, validated observations; generated interpretations stay
pending. An evaluator independent of the builder runs protected product checks
inside a pinned, resource-limited container without network access. The gateway
checks the exact result contract before deriving progress or completion.

Native actions use stable run/step identities, exact contracts and read-back.
An interrupted write enters reconciliation; cancellation is not a promise to
undo a committed effect. An explicit bounded reconciliation grant permits only
reads. Incident recovery requires both a technical probe and passing accepted
business criteria over a new observation window.

Regression campaigns bind package activation and rollback to exact digests and
real run evidence. Outcome feedback creates pending requirement, test, runbook,
package and KB proposals. Execution success never approves generated knowledge.

## Consequences and acceptance

The OpenClaw adapter retains its existing task/control integration. These new
profiles use the CLI and MCP execution contracts documented in the
[runtime guide](../sdlc-runtime.md); they do not introduce a second cloud router.
Trusted host workers retain their narrowly configured source/action credentials.
The model and product-check sandbox do not receive those credentials.

The [completion record](../sdlc-completion-checklist.md) maps all twelve scope
items to executed positive and negative tests. It separates native-service
protocol fixtures from an actual local-model trial. Local compatibility,
isolation, backup/restore and interruption recovery are functional acceptance.
Additional vendors, live production rollout, representative model/adoption
cohorts and enterprise identity/storage/retention/HA need target-specific inputs
and evidence. Serial bounded profiles do not establish a general parallel
multi-agent planner or universal autonomous SDLC.
