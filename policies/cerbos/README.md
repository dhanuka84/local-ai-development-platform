# Cerbos authorization policies

These policy-as-code files define the agentic workflow authorization contract
used by the local Compose runtime. The Go gateway calls the internal Cerbos PDP
for workflow, knowledge, repository-relation, and code-repository actions.

The gateway is the policy enforcement point. It authenticates the principal
and hydrates every principal/resource attribute from PostgreSQL before
calling Cerbos. Never construct these attributes from an agent prompt, MCP
arguments, or model output.

Validate all policies and tests with:

```bash
make authz-policy-test
```

Protected actions must fail closed when the PDP is unavailable. See
[ADR-0008](../../docs/adr/0008-cerbos-contextual-authorization.md) and the
[agentic automation plan](../../docs/openclaw-agentic-automation-plan.md).
