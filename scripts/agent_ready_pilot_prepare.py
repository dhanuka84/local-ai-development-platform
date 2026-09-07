#!/usr/bin/env python3
"""Prepare a fresh private runtime for the isolated local pilot, never approve it.

This generates only a scoped workload credential. It does not copy live data,
bootstrap a human identity, invoke models, or modify Docker/live databases.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess


def prepare(destination, project, database, model):
    root = Path(__file__).resolve().parents[1]
    destination = Path(destination).resolve()
    local_root = (root / ".local").resolve()
    if not destination.is_relative_to(local_root) or destination == local_root:
        raise ValueError("runtime must be a new child of this repository's .local directory")
    if not project.startswith("pilot-") or not database.startswith("pilot_"):
        raise ValueError("isolated pilot project/database prefixes are required")
    if not database.replace("_", "").isalnum():
        raise ValueError("database name must contain only letters, numbers and underscores")
    destination.mkdir(parents=True, mode=0o700, exist_ok=False)
    os.chmod(destination, 0o700)
    source = destination / "source"
    shutil.copytree(root / "examples/agent-ready-pilot/source", source,
                    ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
    subprocess.run(["git", "init", "--quiet", "-b", "main", str(source)], check=True)
    subprocess.run(["git", "-C", str(source), "add", "test_labels.py", "test_keys.py"], check=True)
    subprocess.run(["git", "-C", str(source), "-c", "user.name=Agent-ready pilot workload",
                    "-c", "user.email=pilot@localhost", "commit", "--quiet", "-m",
                    "Synthetic normalization acceptance fixtures"], check=True)
    revision = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip()
    platform_revision = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
    state = destination / "state"
    state.mkdir(mode=0o700)
    (destination / "bin").mkdir(mode=0o700)
    token = secrets.token_urlsafe(48)
    actor = "workload:" + project

    def private_file(path, content):
        with path.open("x", encoding="utf-8") as handle:
            os.chmod(path, 0o600)
            handle.write(content)

    private_file(state / "workload.token", token)
    private_file(state / "principals.json", json.dumps([{
        "id": actor, "display_name": "Local pilot execution workload", "human": False,
        "roles": ["controller", "development", "validation_executor"],
        "project_ids": [project], "token": token,
    }]))
    # The database credential is the existing disposable dependency fixture,
    # never a live credential. The workload credential is file-backed only.
    environment = {
        "APP_ENV": "local", "AUTH_MODE": "token", "AUTHORIZATION_MODE": "cerbos",
        "AUTH_TOKEN_FILE": "/pilot/state/workload.token",
        "AUTH_PRINCIPALS_JSON_FILE": "/pilot/state/principals.json",
        "DATABASE_URL": f"postgres://hybrid:agent-ready-test-only@hybrid-ai-agent-ready-db-20260906:5432/{database}?sslmode=disable",
        "CERBOS_ADDRESS": "hybrid-ai-agent-ready-cerbos-20260906:3593",
        "MILVUS_ADDRESS": "hybrid-ai-agent-ready-milvus-20260906:19530",
        "MILVUS_COLLECTION": database, "MILVUS_DATABASE": "",
        "OLLAMA_URL": "http://pilot-ollama:11434",
        "OLLAMA_EMBEDDING_MODEL": "embeddinggemma:latest", "EMBEDDING_DIMENSION": "768",
        "ARTIFACTS_PATH": "/pilot/state/artifacts", "GRAPH_BACKEND": "postgres",
        "CODEGRAPH_ENABLED": "false", "CODEGRAPH_ALLOWED_ROOTS": "/pilot/source",
        "AUTO_APPROVE_LOCAL": "false", "SEARCH_LEXICAL_FALLBACK": "true",
        "AGENT_READY_PILOT_ISOLATED": "true", "HTTP_ADDRESS": "127.0.0.1:8080",
        "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "safe.directory", "GIT_CONFIG_VALUE_0": "/pilot/source",
    }
    private_file(destination / "runtime.env", "".join(f"{key}={value}\n" for key, value in environment.items()))

    def packet(key, filename, function, separator, tests):
        return {
            "schema_version": "hybrid-ai/work-packet/v1", "id": key,
            "goal": (f"Add new Python file {filename} defining {function}(value: str) -> str. "
                     "Canonicalize whitespace and Unicode case: split input on any whitespace, "
                     f"join words using {separator!r}, then apply Unicode casefold. "
                     "Whitespace-only input becomes the empty string. Preserve punctuation. "
                     "Use Python's standard library only, no imports from other local files, "
                     "no I/O or network. Existing test files are read-only. "
                     "Return a proper git unified diff adding this single file, with correct hunk counts and final newline. "
                     "Also propose a concise generalized lesson about normalization and idempotence."),
            "workspace": "/pilot/source", "base_revision": revision, "mode": "patch",
            "task_class": "maintenance", "data_classification": "internal",
            "local_only": True, "cloud_review": False, "destructive": False,
            "allowed_files": [filename], "rollback": ["Discard the disposable verifier clone"],
            "checks": [
                {"name": "diff-integrity", "argv": ["git", "diff", "--cached", "--check"], "timeout_seconds": 10},
                {"name": "six-behavior-tests", "argv": ["python3", "-m", "unittest", "-v", tests], "timeout_seconds": 20},
            ],
            "limits": {"max_changed_files": 1, "max_diff_lines": 60, "max_patch_bytes": 12000},
        }

    # unittest needs its local test/module import root; the isolated container
    # provides the security boundary, not Python's isolated import mode.
    a = packet("normalization-a", "labels.py", "normalize_label", " ", "test_labels.py")
    b = packet("normalization-b", "keys.py", "canonical_key", "-", "test_keys.py")
    spec = {"project_id": project, "run_key": project + ":normalization", "model": model,
            "branch": "main", "task_a": a, "task_b": b}
    private_file(state / "pilot.json", json.dumps(spec, indent=2) + "\n")
    private_file(state / "setup.json", json.dumps({
        "project_id": project, "database": database, "actor": actor, "human": False,
        "provider": "ollama", "model": model, "embedding_model": "embeddinggemma:latest",
        "platform_revision": platform_revision, "source_revision": revision,
        "status": "prepared_not_executed", "approval": "none",
    }, indent=2) + "\n")
    print(json.dumps({"runtime": str(destination), "project_id": project,
                      "database": database, "source_revision": revision,
                      "platform_revision": platform_revision, "credential": "private file; not printed"}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("runtime")
    parser.add_argument("--project", default="pilot-agent-ready-20260906")
    parser.add_argument("--database", default="pilot_agent_ready_20260906")
    parser.add_argument("--model", default="qwen3.6:35b")
    options = parser.parse_args()
    prepare(options.runtime, options.project, options.database, options.model)
