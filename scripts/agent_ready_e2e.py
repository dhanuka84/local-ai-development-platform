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
        if not row.get("checks") or not row.get("boundary") or not row.get("functional_requirement"):
            raise ValueError(f"{row['id']}: checks, functional requirement and evidence boundary are required")
        deferred = row.setdefault("deferred_acceptance", [])
        if not isinstance(deferred, list) or any(not isinstance(item, str) or not item.strip() for item in deferred):
            raise ValueError(f"{row['id']}: deferred acceptance must list concrete remaining requirements")
        if row["delivery_status"] == "open" and not deferred:
            raise ValueError(f"{row['id']}: open delivery needs an explicit deferred requirement")
        if row["delivery_status"] == "complete" and deferred:
            raise ValueError(f"{row['id']}: completed delivery cannot contain deferred requirements")
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
        passed = all(v == "pass" for v in checks.values())
        report.append({**row, "checks": checks,
                       "regression_status": "pass" if passed else "fail",
                       "functional_status": "pass" if passed else "fail"})
    return report


def functional_summary(coverage, suites):
    failed_ids = [row["id"] for row in coverage if row["functional_status"] != "pass"]
    failed_suites = [suite["package"] for suite in suites if suite["exit_code"] != 0]
    return {
        "status": "pass" if coverage and suites and not failed_ids and not failed_suites else "fail",
        "passed": len(coverage) - len(failed_ids), "total": len(coverage),
        "failed_ids": failed_ids, "failed_suites": failed_suites,
    }


def functional_markdown(coverage, acceptance):
    lines = ["# Local functional acceptance", "",
             f"Result: **{acceptance['status']}**; {acceptance['passed']}/{acceptance['total']} mapped requirements passed.", "",
             "Scope: existing local KB functionality tested with disposable services and synthetic data.",
             "Generated KB entries stay pending unless an explicit operator decision is supplied.", "",
             "| ID | Result | Functional requirement |", "|---|---|---|"]
    for row in coverage:
        requirement = row["functional_requirement"].replace("|", "\\|").replace("\n", " ")
        lines.append(f"| {row['id']} | {row['functional_status']} | {requirement} |")
    if acceptance["failed_suites"]:
        lines += ["", "Failed suites: " + ", ".join(acceptance["failed_suites"])]
    lines += ["", "## Deferred rollout and adoption acceptance", "",
              "These requirements are outside this functional pass. Their deferral never excuses a failed functional test.", ""]
    for row in coverage:
        for item in row.get("deferred_acceptance", []):
            lines.append(f"- **{row['id']}**: {item}")
    return "\n".join(lines) + "\n"


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
    acceptance = functional_summary(coverage, results)
    passed = acceptance["status"] == "pass"
    summary = {
        "schema_version": "hybrid-ai/checklist-regression/v1",
        "started_at": started, "completed_at": datetime.now(timezone.utc).isoformat(),
        "status": "pass" if passed else "fail",
        "repository_revision": os.environ.get("TEST_REPOSITORY_REVISION", "unknown"),
        "source_snapshot_sha256": digest,
        "inference": {"provider": "none", "model": "none", "embedding_protocol_fixture": "ollama/synthetic-e2e-fixture"},
        "scope": "Synthetic regression evidence; does not approve real knowledge or certify live/enterprise deployment or autonomy cohorts.",
        "acceptance_scope": "local-functionality",
        "functional_acceptance": acceptance,
        "deferred_acceptance": [{"id": row["id"], "requirements": row["deferred_acceptance"]}
                                for row in coverage if row["deferred_acceptance"]],
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
    with (output / "functional-acceptance.md").open("x") as handle:
        handle.write(functional_markdown(coverage, acceptance))
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
    print(f"Local functional acceptance: {acceptance['status'].upper()} ({acceptance['passed']}/{acceptance['total']}); receipt /evidence/functional-acceptance.md", flush=True)
    print(f"Rollout/adoption acceptance deferred for {len(summary['deferred_acceptance'])} items; tracked separately from functional acceptance", flush=True)
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
