#!/bin/sh
# Each invocation owns its own throwaway project. Preserve logs outside Docker
# when desired; cleanup never addresses the live or retained pilot projects.
set -eu
test_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_project="hybrid-ai-agent-ready-check-$(date +%s)-$$"
test_compose="$test_root/deploy/compose/compose.agent-ready-test.yaml"
mkdir -p "$test_root/.local/agent-ready-e2e"
AGENT_READY_REPORT_DIR=$(mktemp -d "$test_root/.local/agent-ready-e2e/run-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
TEST_REPOSITORY_REVISION=$(git -C "$test_root" rev-parse HEAD)
export AGENT_READY_REPORT_DIR TEST_REPOSITORY_REVISION
printf 'E2E evidence: %s\n' "$AGENT_READY_REPORT_DIR"
compose() { docker compose -p "$test_project" -f "$test_compose" "$@"; }
cleanup() {
    result=$?
    trap - EXIT INT TERM
    if [ "$result" -ne 0 ]; then
        compose logs --no-color --tail=80 >"$AGENT_READY_REPORT_DIR/services.log" 2>&1 || true
        printf 'Failure diagnostics: %s/services.log\n' "$AGENT_READY_REPORT_DIR"
    fi
    compose down --volumes --remove-orphans || true
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose config --quiet
git -C "$test_root" diff --binary >"$AGENT_READY_REPORT_DIR/working-tree.patch"
sha256sum "$test_root/.github/workflows/ci.yaml" >"$AGENT_READY_REPORT_DIR/ci-workflow.sha256"
compose build checks
compose up -d --wait --wait-timeout 240 postgres milvus cerbos otel-collector
compose run --rm --no-deps checks
