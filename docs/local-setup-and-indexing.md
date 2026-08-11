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

## 2. Clone a collection of repositories

Authenticate GitHub CLI and choose a narrow parent directory for the source
checkouts:

```bash
gh auth status

repository_owner=example-owner
repository_root=/absolute/path/to/source-repositories
mkdir -p "$repository_root"

gh repo list "$repository_owner" --limit 1000 --json nameWithOwner \
  --jq '.[].nameWithOwner' |
while IFS= read -r repository; do
  destination="$repository_root/${repository##*/}"
  if [ -d "$destination/.git" ]; then
    if [ -n "$(git -C "$destination" status --porcelain)" ]; then
      printf 'Skipping dirty checkout: %s\n' "$destination" >&2
      continue
    fi
    git -C "$destination" pull --ff-only
  else
    gh repo clone "$repository" "$destination"
  fi
done
```

This updates clean existing checkouts, clones missing repositories, and reports
dirty checkouts without changing them. Review dirty or deliberately pinned
checkouts manually.

Inventory the checked-out branch and exact revision of every Git repository:

```bash
for repository_path in "$repository_root"/*; do
  [ -d "$repository_path/.git" ] || continue
  printf '%s | %s | %s\n' \
    "$(basename "$repository_path")" \
    "$(git -C "$repository_path" branch --show-current)" \
    "$(git -C "$repository_path" rev-parse HEAD)"
done
```

## 3. Configure the platform

From the platform checkout:

```bash
cd /absolute/path/to/local-ai-development-platform
make env-init
```

`make env-init` creates `.env` with mode `0600` and refuses to overwrite an
existing file. Set `CODEGRAPH_HOST_ROOT` in `.env` to the absolute
`repository_root` selected above. Compose mounts that one directory read-only
at `/workspace`; each child repository is then addressed as
`/workspace/<repository-name>`.

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

The following reset is destructive. It permanently removes PostgreSQL data,
Milvus data, Ollama models, artifacts, analyzer dependency caches, MinIO/etcd
state, and the Cerbos audit volume. Run it only when that complete loss is
intended:

```bash
docker compose --env-file .env \
  -f deploy/compose/compose.yaml \
  -f deploy/compose/compose.gpu.yaml \
  down --volumes --remove-orphans

docker compose --env-file .env \
  -f deploy/compose/compose.yaml \
  -f deploy/compose/compose.gpu.yaml \
  build --pull --no-cache

docker compose --env-file .env \
  -f deploy/compose/compose.yaml \
  -f deploy/compose/compose.gpu.yaml \
  up --force-recreate -d
```

Confirm that the old project volumes were removed after the `down` command:

```bash
docker volume ls -q \
  --filter label=com.docker.compose.project=hybrid-ai-platform
```

The command should print nothing before the stack is started again. The new
start creates a new set of empty volumes.

## 5. Pull and verify local models

The first stack start pulls the embedding model configured by
`OLLAMA_EMBEDDING_MODEL`. Pull the larger local coding model separately:

```bash
make pull-local-model
```

Verify the locally installed models:

```bash
docker compose --env-file .env -f deploy/compose/compose.yaml \
  exec ollama ollama list
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
specifically required. For each repository, record:

```bash
repository_path=/absolute/path/to/source-repositories/example-service

git -C "$repository_path" status --short
git -C "$repository_path" remote get-url origin
git -C "$repository_path" branch --show-current
git -C "$repository_path" symbolic-ref --quiet --short \
  refs/remotes/origin/HEAD | sed 's#^origin/##'
git -C "$repository_path" rev-parse HEAD
```

An empty `status --short` result is clean. The symbolic-ref command reports the
remote default branch; if that remote does not advertise `origin/HEAD`, query
the forge instead. Use the full commit SHA. Do not assume that the checked-out
branch is the default branch.

Index foundational build, BOM, library, or platform repositories before their
dependents. The JVM analyzer installs successful disposable Maven builds into
the analyzer cache so later sibling scans can resolve those artifacts.

Repositories containing only documentation or unsupported languages may be
reported as skipped; they do not produce an empty active code graph.

## 8. Index repositories through MCP

Start an MCP-connected Codex session. The tool timeout is 20 minutes to allow
large compiler-backed scans to finish:

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
    "canonical_url": "git@github.com:example-owner/example-service.git",
    "default_branch": "main"
  },
  "repository_path": "/workspace/example-service",
  "branch": "main",
  "revision": "0123456789abcdef0123456789abcdef01234567",
  "allow_dirty": false
}
```

For a repeatable multi-repository run, ask the MCP-connected agent to:

```text
Index each clean Git checkout immediately below /workspace, one at a time.
Use project_id local-development. Read each repository's origin, checked-out
branch, default branch, and full HEAD revision; pass those exact values to
code_repository_index with allow_dirty=false. Index foundational build and
library repositories before dependent applications. Skip only repositories
with no supported source language, and return repository, branch, revision,
entity count, relation count, duration, and any skip or failure reason.
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

## 9. Verify active snapshots, counts, and timings

List every active repository snapshot directly from authoritative PostgreSQL:

```bash
docker compose --env-file .env -f deploy/compose/compose.yaml \
  exec -T postgres psql -U hybrid -d hybrid -P pager=off -c "
SELECT repository.name AS repository,
       run.branch,
       left(run.revision, 12) AS revision,
       run.statistics->>'entities' AS entities,
       run.statistics->>'relations' AS relations,
       round(extract(epoch FROM (run.completed_at - run.started_at))::numeric, 1)
         AS analyzer_seconds
FROM code_repository_heads AS head
JOIN software_repositories AS repository ON repository.id = head.repository_id
JOIN code_analysis_runs AS run ON run.id = head.analysis_run_id
ORDER BY repository.name;"
```

The recorded duration covers source preparation and analyzer execution. It
does not include the final PostgreSQL transaction or asynchronous semantic
embedding.

Check the embedding outbox:

```bash
docker compose --env-file .env -f deploy/compose/compose.yaml \
  exec -T postgres psql -U hybrid -d hybrid -P pager=off -c "
SELECT topic,
       count(*) FILTER (WHERE completed_at IS NULL) AS pending,
       count(*) FILTER (WHERE completed_at IS NOT NULL) AS completed,
       count(*) FILTER (
         WHERE completed_at IS NULL AND last_error <> ''
       ) AS retrying
FROM outbox_events
GROUP BY topic
ORDER BY topic;"
```

Follow the worker if pending events are not decreasing:

```bash
make mcp-logs
```

Press `Ctrl-C` to stop following logs; the containers continue running.

### Reference timing from one local run

One warm-cache run indexed seven source repositories containing 130,279
entities and 1,092,381 relations in 524.2 seconds of recorded analyzer time.
The practical serial wall time was approximately 10–12 minutes after allowing
for graph persistence and orchestration. The largest repository took about
3 minutes 24 seconds. Cold dependency caches, network downloads, repository
size, and build-plugin behavior can increase those times substantially.

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
  before retrying because the server may still have completed the scan.
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
  its outbox event may still be pending. Check the outbox query and worker logs.
