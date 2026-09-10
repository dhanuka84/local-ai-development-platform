#!/bin/sh
# Each invocation owns its own throwaway project. Preserve logs outside Docker
# when desired; cleanup never addresses the live or retained pilot projects.
set -eu
test_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_project="hybrid-ai-agent-ready-check-$(date +%s)-$$"
test_compose="$test_root/deploy/compose/compose.agent-ready-test.yaml"
compose() { docker compose -p "$test_project" -f "$test_compose" "$@"; }
cleanup() {
    result=$?
    trap - EXIT INT TERM
    if [ "$result" -ne 0 ]; then
        compose logs --no-color --tail=80 || true
    fi
    compose down --volumes --remove-orphans || true
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose config --quiet
compose build checks
compose up -d --wait --wait-timeout 240 postgres milvus cerbos otel-collector
compose run --rm --no-deps checks
