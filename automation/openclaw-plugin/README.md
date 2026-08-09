# Hybrid Workflow Controller for OpenClaw

This plugin mirrors OpenClaw managed Task Flow state to the authoritative Go
MCP/PostgreSQL workflow service. It never calls PostgreSQL, Cerbos, Milvus, or
model providers directly.

Build and test:

```bash
make openclaw-plugin-deps
make openclaw-plugin-check
make openclaw-config-check
make openclaw-plugin-install
```

Set the configured controller token environment variable before starting the
OpenClaw Gateway. The default local setup uses the separate
`CONTROLLER_AUTH_TOKEN` workload principal with the `controller` role. Never
give OpenClaw the human `AUTH_TOKEN`: the all-roles solo developer uses that
credential in Codex or another local human approval surface to exercise
Development, QA, Product Owner, and Operations gates explicitly.
