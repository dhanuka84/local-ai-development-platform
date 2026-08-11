# Local setup and multi-repository indexing

This runbook takes a new workstation from Docker access through a fresh GPU
deployment, local model installation, multi-repository source analysis, and
verification. Examples deliberately use generic repository and project names.

## 1. Install and verify prerequisites

Required tools:

- Docker Engine with Compose v2
- Git
- Codex CLI
- GitHub CLI (`gh`) when cloning every repository owned by one account or
  organization
- A supported NVIDIA driver and NVIDIA Container Toolkit for GPU mode

Verify the client-side tools:

```bash
docker --version
docker compose version
git --version
codex --version
gh --version
```

### Docker socket permission

If Docker reports `permission denied` for `/var/run/docker.sock`, add the
current account to the `docker` group:

```bash
sudo usermod -aG docker "$(id -un)"
```

Completely log out of the desktop or SSH session and log back in. Opening a
new terminal inside the old desktop session may retain the old supplementary
groups. `newgrp docker` can update one shell temporarily, but a full
logout/login is the reliable workstation fix.

After reconnecting, verify both the group and the Docker server connection:

```bash
id -nG | tr ' ' '\n' | grep -x docker
docker info --format 'Docker server {{.ServerVersion}} is reachable'
```

Do not work around the problem with `chmod 666 /var/run/docker.sock`. Membership
in the `docker` group grants root-equivalent host control and should be limited
to trusted accounts.

## 2. Synchronize a GitHub organization

Authenticate GitHub CLI, then run the checked-in Make workflow from the
platform checkout. Choose a narrow parent directory; the workflow creates one
child directory named after the organization and one clean checkout beneath
it:

```bash
make repository-org-sync \
  GITHUB_ORG=example-organization \
  REPOSITORY_ROOT=/absolute/path/to/source-repositories
```

The target enumerates every non-empty, non-archived repository visible to the
authenticated GitHub identity. It clones missing repositories, fetches remote
state, fast-forwards the intended branch, and prints the repository, remote
default branch, analyzed branch, and full commit SHA. It fails rather than
changing a dirty checkout or a non-Git path.

Use an explicit branch override when analysis intentionally follows a branch
other than the forge default:

```bash
make repository-org-sync \
  GITHUB_ORG=example-organization \
  REPOSITORY_ROOT=/absolute/path/to/source-repositories \
  REPOSITORY_BRANCH_OVERRIDES='large-engine=java-25 another-repository=release/2.x'
```

Overrides are `repository=branch` pairs separated by spaces. The catalog keeps
the remote default branch separately from the checked-out analysis branch.
Review and version organization-specific overrides; silently assuming the
default branch can publish the wrong active graph.

## 3. Configure the platform

From the platform checkout:

```bash
cd /absolute/path/to/local-ai-development-platform
make env-init
```

`make env-init` creates `.env` with mode `0600` and refuses to overwrite an
existing file. Set `CODEGRAPH_HOST_ROOT` in `.env` to the absolute
`repository_root` selected above. Compose mounts that one directory read-only
at `/workspace`; an organization checkout is addressed as
`/workspace/<organization>/<repository-name>`.

Review these analyzer settings as well:

```dotenv
CODEGRAPH_ENABLED=true
CODEGRAPH_MAX_FILES=5000
CODEGRAPH_MAX_ENTITIES=200000
CODEGRAPH_MAX_RELATIONS=1000000
```

Increase a limit only after reviewing the repository and the expected database
growth. Keep `.env` out of Git.

Authenticate Codex and validate the complete configuration:

```bash
make codex-login
make preflight
```

## 4. Start normally or perform an explicitly destructive rebuild

For an ordinary GPU start that preserves all data:

```bash
make up-gpu
```

`make up-gpu` renders the base Compose file plus its NVIDIA overlay, builds
locally buildable images, starts the services in the background, applies
migrations, initializes Milvus, and pulls the configured embedding model. It
does not remove existing named volumes.

### Fresh rebuild with no retained volumes

The following Make target is destructive. It permanently removes PostgreSQL data,
Milvus data, Ollama models, artifacts, analyzer dependency caches, MinIO/etcd
state, and the Cerbos audit volume. Run it only when that complete loss is
intended:

```bash
make rebuild-fresh-gpu CONFIRM_DESTROY=all-platform-data
```

Use `make rebuild-fresh` for the CPU profile. Both targets require the exact
confirmation value, remove the project volumes and orphan containers, rebuild
every locally built image with `--pull --no-cache`, and force-create the stack.
They are intentionally separate from the data-preserving `make up` and
`make up-gpu` paths.

## 5. Pull and verify local models

The first stack start pulls the embedding model configured by
`OLLAMA_EMBEDDING_MODEL`. Pull the larger local coding model separately:

```bash
make pull-local-model
```

Verify the locally installed models:

```bash
make models-list
```

Deleting the Ollama volume requires downloading both models again.

## 6. Check platform health

```bash
make mcp-status
make doctor
```

`mcp-status` shows the Compose services and checks both `/healthz` and
`/readyz`. `doctor` checks PostgreSQL, Apache AGE, Ollama, and Milvus from inside the
Compose network.

If preflight succeeds but Compose then reports Docker socket permission denied,
return to the Docker group procedure in step 1. Preflight can validate the
Docker client and Compose configuration without proving that the current login
session may open the daemon socket.

## 7. Prepare reproducible repository identities

Index only clean commits unless a non-reproducible working-tree snapshot is
specifically required. `make repository-org-sync` performs the clean-tree
check and prints the remote default branch, intended analysis branch, and full
commit SHA for every repository. Treat that output as the run inventory. Do
not assume that the checked-out branch is the default branch.

Index foundational build, BOM, library, or platform repositories before their
dependents. The JVM analyzer installs successful disposable Maven builds into
the analyzer cache so later sibling scans can resolve those artifacts.

Repositories containing only documentation or unsupported languages are still
registered in `software_repositories`. They are reported as `catalog-only` and
do not produce an empty active code graph.

## 8. Index repositories through Make and MCP

The recommended organization-wide command is:

```bash
make repository-org-index-all \
  GITHUB_ORG=example-organization \
  REPOSITORY_PROJECT=local-development \
  REPOSITORY_BRANCH_OVERRIDES='large-engine=java-25'
make repository-org-wait
```

`repository-org-index-all` runs four idempotent phases:

1. synchronize clean checkouts at the intended branches;
2. register every eligible repository in the PostgreSQL catalog, including
   metadata-only repositories;
3. retain current active code snapshots and invoke the authenticated
   `code_repository_index` MCP tool only for missing or stale supported source;
4. print the active catalog, branch, revision, entity, relation, and queue
   state.

`repository-org-wait` waits until every code-index outbox event is completed
and fails immediately if an event enters retry state. Set `WAIT_TIMEOUT` when a
large organization needs more than the one-hour default.

For one repository, use the bounded workflow directly. It derives the remote,
default branch, active branch, exact revision, and container path from the
validated checkout, then waits for semantic projection and prints the active
snapshot:

```bash
make repository-sync-one \
  REPO=/absolute/path/to/source-repositories/large-engine \
  REPOSITORY_URL=https://github.com/example-organization/large-engine.git \
  REPOSITORY_BRANCH=java-25
make repository-index-one-all \
  REPO=/absolute/path/to/source-repositories/large-engine \
  REPOSITORY_PROJECT=local-development
```

For an interactive approval flow, start an MCP-connected Codex session. The
tool timeout is 20 minutes to allow large compiler-backed scans to finish:

```bash
make codex
```

Call `code_repository_index` once per repository and approve the write prompt.
Use this request shape, replacing every example value with the identity
collected in step 7:

```json
{
  "project_id": "local-development",
  "repository": {
    "name": "example-service",
    "canonical_url": "https://github.com/example-organization/example-service.git",
    "default_branch": "main"
  },
  "repository_path": "/workspace/example-organization/example-service",
  "branch": "main",
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "allow_dirty": false
}
```

The language router combines all applicable providers in mixed-language
repositories:

- Go: native Go compiler analysis
- Java and Gradle Kotlin: `scip-java`
- TypeScript and JavaScript: `scip-typescript`
- Python: `scip-python`

JVM, TypeScript/JavaScript, and Python providers work from disposable copies;
the mounted source stays read-only. Kotlin in Maven-only projects is not
supported by `scip-java`.

Every successful run atomically advances one active snapshot identified by
project, repository, checked-out branch, and exact revision. Symbol results
also carry that repository, branch, and revision, preventing same-named symbols
from different repositories or branches from becoming ambiguous.

The exact MCP and PostgreSQL names are `branch` and `revision`. They represent
the concepts sometimes called `branch_name` and `git_commit`; the platform does
not store a second pair of duplicate properties.

## 9. Verify active snapshots, counts, and timings

List the authoritative catalog and every active repository snapshot:

```bash
make repository-org-verify \
  GITHUB_ORG=example-organization \
  REPOSITORY_PROJECT=local-development
```

The recorded duration covers source preparation and analyzer execution. It
does not include the final PostgreSQL transaction or asynchronous semantic
embedding.

Check or wait for the embedding outbox:

```bash
make repository-org-queue-status
make repository-org-wait WAIT_TIMEOUT=3600
```

Follow the worker if pending events are not decreasing:

```bash
make mcp-logs
```

Press `Ctrl-C` to stop following logs; the containers continue running.

If repeated scans created duplicate or non-active pending code-entity events,
compact them before waiting:

```bash
make compact-code-outbox
make repository-org-wait
```

Compaction marks superseded events completed; it does not delete audit rows.
It retains the newest pending event for every entity in an active repository
snapshot. `make reindex` can recreate semantic projection work.

For a large backfill, workers can be scaled temporarily without restarting
PostgreSQL:

```bash
make worker-scale-postgres-fallback WORKER_REPLICAS=3
make repository-org-wait
make worker-scale-postgres-fallback WORKER_REPLICAS=1
```

Workers claim rows with `FOR UPDATE SKIP LOCKED`, embed code entities in
batches, and write each batch to Milvus in one upsert.

### Reference timing from one local run

One warm-cache organization run cataloged eight repositories. Seven source
repositories contained 130,239 active entities and 1,092,449 active relations;
one documentation repository was catalog-only. The largest repository,
analyzed from an explicit `java-25` override, produced 107,826 entities and
988,504 relations in about 3 minutes 23 seconds. After removing superseded
events, three batched workers projected 87,427 semantic events in about 22
minutes. Cold dependency caches, model placement, network downloads,
repository size, and build-plugin behavior can increase those times.

## 10. Verify retrieval and graph attribution

After the outbox drains, use `code_symbol_search` with a specific
`project_id`, query, and optional `repository_id`. Confirm each result contains
the expected repository name, branch, and revision. Then call
`code_graph_get` with that repository and symbol to traverse exact PostgreSQL
relationships.

Use `repository_graph_get` for explicitly recorded relationships between
repositories. Code indexing maps symbols to repository/branch/revision; it does
not invent cross-repository product relationships. Record those separately
with `repository_relation_upsert` and evidence when needed.

## Troubleshooting analyzer runs

- **MCP request times out:** Use the checked-in Codex configuration or
  `make codex`; both allow 1,200 seconds for tool calls. Check gateway logs
  before retrying because the server may still complete and atomically publish
  the scan after the client disconnects. Rerun `make repository-org-index-all`;
  it reconciles the active branch/revision rather than assuming cancellation.
- **Dirty-worktree rejection:** Commit or stash the changes. Set
  `allow_dirty=true` only when a fingerprinted, non-reproducible snapshot is
  intentionally acceptable.
- **Repository path rejected:** Confirm `CODEGRAPH_HOST_ROOT` is the intended
  parent, restart/recreate the gateway after changing `.env`, and pass the
  corresponding path below `/workspace`.
- **JVM dependency failure:** Index local foundational repositories first and
  inspect Maven/Gradle errors in gateway logs. Network access may be required
  to populate the analyzer cache.
- **TypeScript resolution is incomplete:** Confirm `package.json`, lockfiles,
  and `tsconfig` files are present. Dependency installation disables lifecycle
  scripts.
- **Python resolution is incomplete:** Python packages from the repository are
  not installed by the analyzer; ensure source roots and import structure are
  discoverable without executing the application.
- **Semantic search misses a new symbol:** The SQL graph is already active, but
  its outbox event may still be pending. Run `make repository-org-queue-status`
  and `make repository-org-wait`, then inspect worker logs if progress stops.
