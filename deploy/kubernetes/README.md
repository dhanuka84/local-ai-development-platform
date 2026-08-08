# Kubernetes base

This Kustomize base is an enterprise application-layer starting point, not a complete production cluster.

Before applying it:

1. Replace `ghcr.io/example/*` image names with signed images from your registry.
2. Create `hybrid-ai-secrets` through an external-secrets/KMS workflow with `DATABASE_URL`, `AUTH_TOKEN`, and optionally `MILVUS_API_KEY`; do not commit a Secret manifest.
3. Provision PostgreSQL, Milvus, and Ollama/private embedding endpoints and patch their addresses.
4. Choose a `ReadWriteMany` storage class or replace the local artifact adapter with object storage.
5. Add OIDC-aware ingress, egress NetworkPolicies, TLS, observability, and tenant authorization.
6. Run migration and Milvus-init Jobs as controlled release hooks before rolling Deployments.

The base keeps `CODEGRAPH_ENABLED=false`: enterprise gateways are query-only and do not mount repositories or contain a Go toolchain. Deploy the durable analysis-job API and sandboxed analyzer worker pool described in the enterprise guide before enabling repository indexing at enterprise scale.

Render without applying:

```bash
kubectl kustomize deploy/kubernetes/base
```
