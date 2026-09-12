#!/usr/bin/env python3
"""Run isolated acceptance suites and bind each checklist row to exact results."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/dhanuka84/hybrid-ai-platform/"
PACKAGES = (
    "internal/e2e", "internal/postgres", "internal/age", "internal/milvus",
    "internal/telemetry", "internal/service", "cmd/agent-ready-pilot",
    "components/workpacket", "contracts", "internal/config", "internal/artifacts",
)


def load_coverage(root=ROOT):
    mapping = json.loads((root / "tests/agent-ready-coverage.json").read_text())
    rows = mapping["items"]
    ids = [row["id"] for row in rows]
    checklist = (root / "docs/agent-ready-gap-checklist.md").read_text()
    recorded = re.findall(r"^\| \[([ x])\] \| ([LE]\d{2}) \|", checklist, re.MULTILINE)
    completion = {item: "complete" if mark == "x" else "open" for mark, item in recorded}
    if len(ids) != len(set(ids)) or len(recorded) != len(completion) or set(ids) != set(completion):
        raise ValueError("coverage must map every checklist ID exactly once")
    for row in rows:
        row["delivery_status"] = completion[row["id"]]
        if not row.get("checks") or not row.get("boundary"):
            raise ValueError(f"{row['id']}: checks and evidence boundary are required")
        for check in row["checks"]:
            package, name = check.split(":", 1)
            if package not in PACKAGES or not name.startswith("Test"):
                raise ValueError(f"{row['id']}: unsupported test reference {check}")
    return rows


def test_outcomes(events):
    outcomes = {}
    for event in events:
        if event.get("Test") and event.get("Action") in ("pass", "fail", "skip"):
            key = event["Package"].removeprefix(MODULE) + ":" + event["Test"]
            # A failure cannot be overwritten by a later retry/pass in a log.
            previous = outcomes.get(key)
            action = event["Action"]
            outcomes[key] = "fail" if "fail" in (previous, action) else (
                "skip" if "skip" in (previous, action) else action)
    return outcomes


def coverage_report(rows, outcomes):
    report = []
    for row in rows:
        checks = {name: outcomes.get(name, "missing") for name in row["checks"]}
        report.append({**row, "checks": checks,
                       "regression_status": "pass" if all(v == "pass" for v in checks.values()) else "fail"})
    return report


def source_digest(root):
    files = [root / path for path in ("Dockerfile", ".dockerignore", "Makefile", "go.mod", "go.sum",
                                     "AGENTS.md", ".env.example", ".codex/config.toml",
                                     "examples/codex/config.toml", "examples/codex/config-stdio.toml",
                                     "docs/agent-ready-gap-checklist.md")]
    for directory in ("cmd", "components", "contracts", "internal", "migrations", "scripts", "tests", "deploy", "policies"):
        files.extend(p for p in (root / directory).rglob("*") if p.is_file() and p.suffix in (".go", ".py", ".sh", ".sql", ".json", ".yaml"))
    entries = {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
               for path in sorted(files)}
    digest = hashlib.sha256(json.dumps(entries, sort_keys=True).encode()).hexdigest()
    return digest, entries


def run(output):
    if os.environ.get("TEST_AGENT_READY_DISPOSABLE") != "true":
        raise ValueError("use make agent-ready-e2e with explicitly disposable services")
    rows = load_coverage()
    # Bind the exact copied inputs before executing tests. CI configuration is
    # excluded by .dockerignore and is hashed separately by the host launcher.
    digest, sources = source_digest(ROOT)
    output.mkdir(parents=True, exist_ok=True)
    if (output / "summary.json").exists():
        raise ValueError("refusing to overwrite a previous evidence receipt")
    started = datetime.now(timezone.utc).isoformat()
    outcomes, results = {}, []
    for package in PACKAGES:
        name = package.replace("/", "-")
        path = output / (name + ".jsonl")
        command = ["go", "test", "-race", "-count=1", "-json", "./" + package]
        with path.open("x") as log, (output / (name + ".stderr")).open("x") as errors:
            try:
                result = subprocess.run(command, cwd=ROOT, stdout=log, stderr=errors,
                                        timeout=600, check=False)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
        events = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
        outcomes.update(test_outcomes(events))
        results.append({"package": package, "command": command, "exit_code": code})
        print(f"{package}: {'PASS' if code == 0 else 'FAIL'}", flush=True)
        if code:
            for event in events:
                if event.get("Output"):
                    print(event["Output"], end="", flush=True)
            print((output / (name + ".stderr")).read_text(), flush=True)
    coverage = coverage_report(rows, outcomes)
    passed = all(r["exit_code"] == 0 for r in results) and all(r["regression_status"] == "pass" for r in coverage)
    summary = {
        "schema_version": "hybrid-ai/checklist-regression/v1",
        "started_at": started, "completed_at": datetime.now(timezone.utc).isoformat(),
        "status": "pass" if passed else "fail",
        "repository_revision": os.environ.get("TEST_REPOSITORY_REVISION", "unknown"),
        "source_snapshot_sha256": digest,
        "inference": {"provider": "none", "model": "none", "embedding_protocol_fixture": "ollama/synthetic-e2e-fixture"},
        "scope": "Synthetic regression evidence; does not approve real knowledge or certify live/enterprise deployment or autonomy cohorts.",
        "delivery_completion": {"complete": sum(row["delivery_status"] == "complete" for row in rows),
                                "total": len(rows), "open_ids": [row["id"] for row in rows if row["delivery_status"] == "open"]},
        "suites": results, "checklist": coverage,
    }
    with (output / "summary.json").open("x") as handle:
        json.dump(summary, handle, indent=2)
        handle.write("\n")
    with (output / "source-manifest.json").open("x") as handle:
        json.dump(sources, handle, indent=2)
        handle.write("\n")
    hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(output.iterdir()) if p.is_file()}
    with (output / "evidence-sha256.json").open("x") as handle:
        json.dump(hashes, handle, indent=2)
        handle.write("\n")
    for path in output.iterdir():
        if path.is_file():
            path.chmod(0o444)
    for row in coverage:
        failed = [name for name, result in row["checks"].items() if result != "pass"]
        if failed:
            print(f"{row['id']}: required tests did not pass: {', '.join(failed)}", flush=True)
    print(f"Checklist regressions: {sum(row['regression_status'] == 'pass' for row in coverage)}/{len(rows)}; receipt /evidence/summary.json", flush=True)
    print(f"Delivery completion remains {summary['delivery_completion']['complete']}/{len(rows)}; see per-item evidence boundaries", flush=True)
    return 0 if passed else 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("run", "validate-map"))
    parser.add_argument("--output", type=Path, default=Path("/evidence"))
    args = parser.parse_args()
    try:
        if args.command == "validate-map":
            print(f"Mapped all {len(load_coverage())} checklist IDs")
            sys.exit(0)
        sys.exit(run(args.output))
    except (ValueError, OSError, KeyError) as error:
        print(f"agent-ready-e2e: {error}", file=sys.stderr)
        sys.exit(1)
