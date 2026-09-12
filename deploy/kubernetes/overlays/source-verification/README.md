# Repository source verification

Documentation reviewed September 12, 2026. Local verification evidence is
recorded in the [checklist](../../../../docs/agent-ready-gap-checklist.md);
target-cluster acceptance is still deferred. See the
[enterprise guide](../../../../docs/enterprise-deployment.md).

This overlay supplies the same read-only Git snapshots and allowed root to the
gateway and workers. It keeps synchronous repository analysis disabled. Build
the gateway image with Docker target `gateway-source-verifier`; the distroless
base gateway has no Git executable and cannot verify repository sources.

Before applying, provision `hybrid-ai-repository-snapshots` as an existing,
release-specific read-only PVC accessible by every replica. Populate it outside
the application pods. Each repository must include Git objects and an exact
`refs/heads/<branch>` at its approved revision. Use an immutable volume snapshot
or storage-level write protection; `readOnly` on a pod mount alone cannot prevent
another pod or storage administrator from changing the backing data. Never mount
a developer's mutable checkout or a hostPath volume into this overlay.

Keep identical repository paths, branch refs and commits across replicas. Source
manifest references use `/repository-snapshots/<repository>`. Deploy a new PVC
name for an updated snapshot and revise/validate affected knowledge before
publication. A mismatched or missing source remains ineligible. Local snapshot
verification makes no assertion about the current remote forge branch.

Replace the example image names with built, signed image digests and patch the
Cerbos endpoint for the target cluster. Keep its policy bundle compatible with
the application release. The worker has write access to the evidence volume so
it can retain actual source-check receipts; repository mounts stay read-only.
Both pods retain a read-only root filesystem with bounded scratch space.

Render without contacting a cluster:

```sh
kubectl kustomize deploy/kubernetes/overlays/source-verification
```

Before production, verify in the target cluster: both replicas resolve the same
Git commit as UID 65532; writes below `/repository-snapshots` fail; a missing ref
or changed revision blocks source eligibility; CAS writes succeed; rolling a
snapshot does not mix revisions; the existing backup/restore and authorization
checks pass. No target cluster or PVC is provisioned by this overlay. Dependency
network policies, identity, TLS and availability remain deployment requirements.
