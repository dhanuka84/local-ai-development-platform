#!/bin/sh
# Local functional proof with trusted host workers and separately isolated
# untrusted patches. Every service, credential, repository and approval is a
# disposable fixture. Cleanup names only this invocation's Docker resources.
set -eu
sdlc_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sdlc_project="hybrid-ai-sdlc-check-$(date +%s)-$$"
mkdir -p "$sdlc_root/.local/sdlc-e2e"
AGENT_READY_REPORT_DIR=$(mktemp -d "$sdlc_root/.local/sdlc-e2e/run-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
TEST_REPOSITORY_REVISION=$(git -C "$sdlc_root" rev-parse HEAD)
export AGENT_READY_REPORT_DIR TEST_REPOSITORY_REVISION
SDLC_TEST_KAFKA_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
export SDLC_TEST_KAFKA_PORT
sdlc_image="$sdlc_project-evaluator:local"
compose() { docker compose -p "$sdlc_project" -f "$sdlc_root/deploy/compose/compose.agent-ready-test.yaml" -f "$sdlc_root/deploy/compose/compose.sdlc-test.yaml" "$@"; }
cleanup() {
    sdlc_result=$?
    trap - EXIT INT TERM
    if [ "$sdlc_result" -ne 0 ]; then compose logs --no-color --tail=100 >"$AGENT_READY_REPORT_DIR/services.log" 2>&1 || true; fi
    compose down --volumes --remove-orphans || true
    docker image rm "$sdlc_image" >/dev/null 2>&1 || true
    exit "$sdlc_result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf 'SDLC evidence: %s\n' "$AGENT_READY_REPORT_DIR"
cd "$sdlc_root"
git diff --binary >"$AGENT_READY_REPORT_DIR/working-tree.patch"
sdlc_source="$AGENT_READY_REPORT_DIR/source"
python3 - "$sdlc_root" "$sdlc_source" <<'PY'
import os, shutil, subprocess, sys
from pathlib import Path
root, destination=map(Path,sys.argv[1:])
paths=subprocess.check_output(['git','ls-files','-z','--cached','--others','--exclude-standard'],cwd=root).decode().split('\0')
for name in sorted(set(paths)):
    if not name: continue
    relative=Path(name)
    if relative.parts[0] in ('.git','.local','data','bin') or (relative.name.startswith('.env') and relative.name!='.env.example'): continue
    source=root/relative
    if not source.exists(): continue
    if source.is_symlink(): raise ValueError('acceptance snapshot refuses source symlinks: '+name)
    target=destination/relative
    target.parent.mkdir(parents=True,exist_ok=True)
    shutil.copy2(source,target)
PY
cd "$sdlc_source"
python3 - "$AGENT_READY_REPORT_DIR" <<'PY'
import json, sys
from pathlib import Path
sys.path.insert(0, 'scripts')
from agent_ready_e2e import source_digest
digest, entries = source_digest(Path.cwd())
(Path(sys.argv[1])/'source-manifest.json').write_text(json.dumps({'source_sha256': digest, 'files': entries}, indent=2)+'\n')
PY
docker build --target sdlc-evaluator -t "$sdlc_image" . >"$AGENT_READY_REPORT_DIR/evaluator-build.log" 2>&1
TEST_SDLC_SANDBOX_IMAGE=$(docker image inspect --format '{{.Id}}' "$sdlc_image")
export TEST_SDLC_SANDBOX_IMAGE
compose up -d --wait --wait-timeout 240 postgres milvus cerbos otel-collector kafka loki prometheus gitea
compose exec -T gitea gitea --config /etc/gitea/app.ini admin user create --username synthetic-delivery --password synthetic-forge-only --email delivery@example.invalid --must-change-password=false >"$AGENT_READY_REPORT_DIR/forge-bootstrap.log" 2>&1
compose exec -T gitea gitea --config /etc/gitea/app.ini admin user create --username synthetic-evaluator --password synthetic-forge-only --email evaluator@example.invalid --must-change-password=false >>"$AGENT_READY_REPORT_DIR/forge-bootstrap.log" 2>&1
TEST_DATABASE_URL="postgres://hybrid:synthetic-acceptance-only@$(compose port postgres 5432)/hybrid?sslmode=disable"
TEST_MILVUS_ADDRESS=$(compose port milvus 19530)
TEST_CERBOS_ADDRESS=$(compose port cerbos 3593)
TEST_OTEL_ENDPOINT="http://$(compose port otel-collector 4318)/v1/traces"
TEST_KAFKA_ADDRESS="127.0.0.1:$SDLC_TEST_KAFKA_PORT"
TEST_S3_ENDPOINT="http://$(compose port minio 9000)"
TEST_LOKI_ENDPOINT="http://$(compose port loki 3100)"
TEST_PROMETHEUS_ENDPOINT="http://$(compose port prometheus 9090)"
TEST_FORGE_ENDPOINT="http://$(compose port gitea 3000)"
TEST_POSTGRES_CONTAINER=$(compose ps -q postgres)
TEST_AGENT_READY_DISPOSABLE=true
TEST_BINARY_DIR="$AGENT_READY_REPORT_DIR/bin"
export TEST_DATABASE_URL TEST_MILVUS_ADDRESS TEST_CERBOS_ADDRESS TEST_OTEL_ENDPOINT TEST_AGENT_READY_DISPOSABLE TEST_BINARY_DIR
export TEST_KAFKA_ADDRESS TEST_S3_ENDPOINT TEST_LOKI_ENDPOINT TEST_PROMETHEUS_ENDPOINT
export TEST_FORGE_ENDPOINT TEST_POSTGRES_CONTAINER
mkdir -p "$TEST_BINARY_DIR"
go build -buildvcs=false -o "$TEST_BINARY_DIR/" ./cmd/gateway ./cmd/worker ./cmd/admin ./cmd/agent-ready-pilot ./cmd/sdlc-worker ./cmd/source-adapter ./cmd/sdlc
sdlc_test_status=0
sdlc_test_tags=sdlc_host
if [ -n "${SDLC_LOCAL_MODEL:-}" ]; then
    TEST_SDLC_LOCAL_MODEL=$SDLC_LOCAL_MODEL
    TEST_SDLC_LOCAL_OLLAMA_URL=${SDLC_LOCAL_OLLAMA_URL:?set the explicit local Ollama origin}
    export TEST_SDLC_LOCAL_MODEL TEST_SDLC_LOCAL_OLLAMA_URL
    sdlc_test_tags=sdlc_host,sdlc_local
fi
go test -buildvcs=false -tags "$sdlc_test_tags" -race -count=1 -json -run 'TestSDLC|TestSandbox' ./internal/e2e ./internal/sdlcworker >"$AGENT_READY_REPORT_DIR/tests.jsonl" 2>&1 || sdlc_test_status=$?
python3 scripts/sdlc_acceptance_report.py "$AGENT_READY_REPORT_DIR" "$sdlc_test_status" "$TEST_SDLC_SANDBOX_IMAGE"
