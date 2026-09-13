# Cerbos authorization policies

Updated September 13, 2026. [Developer guide](../../docs/developer-guide.md) ·
[Scoped autonomy decision](../../docs/adr/0011-scoped-autonomy-and-functional-acceptance.md).

These policy-as-code files define the agentic workflow authorization contract
used by the local Compose runtime. The Go gateway calls the internal Cerbos PDP
for workflow, knowledge, product knowledge/source, definition/metric,
repository-relation and code-repository actions. Client approval configuration never grants these server permissions.
Generated KB entries require explicit user publication; standing task authority
for validated definitions still uses trusted actor and validation evidence.

The gateway is the policy enforcement point. It authenticates the principal
and hydrates every principal/resource attribute from PostgreSQL before
calling Cerbos. Never construct these attributes from an agent prompt, MCP
arguments, or model output.

The [AI-native role/access contract](../../docs/ai-native-sdlc-expectations.md#access-controls-at-every-boundary)
extends these policies to product/source/field/environment/purpose/time/action
scope for each agent responsibility. Product records and source queries now
have policies, including human-only QA/decisions and a diagnostic read role.
The service also enforces operator-configured purpose/field limits and rechecks
retained source data. Broader delegation, budgets and environment controls
remain [A06 work](../../docs/sdlc-gap-assessment.md#a06). Tool discovery or a
prompt role must not grant them. Each new capability needs permitted and denied
cases plus attribution to its delegator and accountable owner; complete
source-to-action audit coverage is [A11](../../docs/sdlc-gap-assessment.md#a11).

Validate all policies and tests with:

```bash
make authz-policy-test
```

Protected actions must fail closed when the PDP is unavailable. See
[ADR-0008](../../docs/adr/0008-cerbos-contextual-authorization.md) and the
[agentic automation plan](../../docs/openclaw-agentic-automation-plan.md).
