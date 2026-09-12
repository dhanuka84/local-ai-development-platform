"""Exercise Codex launch recipes with disposable credentials and a recording CLI.

No installed Codex, inference, live gateway or real vault is used. Prerequisite
checks are omitted explicitly so the test covers the launch boundary itself.
"""
import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
import tomllib
import unittest


ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("codex", "codex-repo", "codex-local", "codex-local-repo")


class HealthHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200 if self.path == "/healthz" else 404)
        self.end_headers()

    def log_message(self, *args):
        pass


class CodexLaunchTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="codex-launch-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        shutil.copyfile(ROOT / "Makefile", self.root / "Makefile")
        self.vault = self.root / "synthetic vault"
        self.vault.mkdir()
        self.token = "synthetic-launch-test-credential"
        (self.vault / "AUTH_TOKEN").write_text(self.token)
        (self.vault / "AUTH_TOKEN").chmod(0o600)
        self.repository = self.root / "target repository"
        self.repository.mkdir()
        self.record = self.root / "launch.json"
        executable = self.root / "codex"
        executable.write_text(f"#!{sys.executable}\n" + '''import json, os, pathlib, sys
if sys.argv[1:] == ["login", "status"]:
    raise SystemExit(0)
pathlib.Path(os.environ["LAUNCH_RECORD"]).write_text(json.dumps({
    "argv": sys.argv[1:], "cwd": os.getcwd(),
    "operator_token_loaded": os.environ.get("HYBRID_AI_MCP_TOKEN") == os.environ["FIXTURE_TOKEN"],
}))
''')
        executable.chmod(0o700)
        self.env = {"PATH": str(self.root) + os.pathsep + os.environ["PATH"],
                    "LAUNCH_RECORD": str(self.record), "FIXTURE_TOKEN": self.token}
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), HealthHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join)
        self.addCleanup(server.shutdown)
        self.endpoint = f"http://127.0.0.1:{server.server_port}"

    def launch(self, target):
        self.record.unlink(missing_ok=True)
        return subprocess.run([
            "make", "--no-print-directory", "-o", "mcp-preflight", "-o", "codex-local-check",
            target, f"VAULT_RUNTIME_DIR={self.vault}", f"REPO={self.repository}",
            f"MCP_BASE_URL={self.endpoint}", "CODEX_LOCAL_MODEL=synthetic:local",
        ], cwd=self.root, env=self.env, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=15, check=False)

    def test_each_launcher_uses_standing_authority_and_operator_token(self):
        for target in TARGETS:
            with self.subTest(target=target):
                result = self.launch(target)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertNotIn(self.token, result.stdout + result.stderr)
                record = json.loads(self.record.read_text())
                self.assertTrue(record["operator_token_loaded"])
                args = record["argv"]
                self.assertNotIn(self.token, " ".join(args))
                overrides = {}
                for index, arg in enumerate(args):
                    if arg == "-c":
                        key, value = args[index + 1].split("=", 1)
                        overrides[key] = tomllib.loads("value=" + value)["value"]
                self.assertEqual(overrides["approval_policy"], "never")
                prefix = "mcp_servers.hybrid_knowledge."
                self.assertEqual(overrides[prefix + "url"], self.endpoint + "/mcp")
                self.assertEqual(overrides[prefix + "bearer_token_env_var"], "HYBRID_AI_MCP_TOKEN")
                self.assertIs(overrides[prefix + "enabled"], True)
                self.assertIs(overrides[prefix + "required"], True)
                self.assertEqual(overrides[prefix + "default_tools_approval_mode"], "approve")
                self.assertEqual(overrides[prefix + "tools.knowledge_candidate_decide.approval_mode"], "prompt")
                for tool in ("context_definition_decide", "repository_relation_upsert", "code_repository_index"):
                    self.assertEqual(overrides[prefix + "tools." + tool + ".approval_mode"], "approve")
                if "local" in target:
                    self.assertIn("--oss", args)
                    self.assertEqual(args[args.index("--local-provider") + 1], "ollama")
                    self.assertEqual(args[args.index("--model") + 1], "synthetic:local")
                else:
                    self.assertNotIn("--oss", args)
                if target.endswith("-repo"):
                    self.assertEqual(args[args.index("-C") + 1], str(self.repository))
                else:
                    self.assertEqual(record["cwd"], str(self.root))

    def test_missing_credential_stops_each_launcher(self):
        (self.vault / "AUTH_TOKEN").unlink()
        for target in TARGETS:
            with self.subTest(target=target):
                result = self.launch(target)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("vault AUTH_TOKEN is empty", result.stderr)
                self.assertFalse(self.record.exists())

    def test_configs_require_lesson_decisions_but_allow_definition_publication(self):
        for path in (".codex/config.toml", "examples/codex/config.toml", "examples/codex/config-stdio.toml"):
            with self.subTest(path=path):
                config = tomllib.loads((ROOT / path).read_text())
                self.assertEqual(config["approval_policy"], "never")
                for server in config["mcp_servers"].values():
                    self.assertEqual(server["default_tools_approval_mode"], "approve")
                    self.assertEqual(server["tools"]["knowledge_candidate_decide"]["approval_mode"], "prompt")
                    self.assertEqual(server["tools"]["context_definition_decide"]["approval_mode"], "approve")
                    for name, tool in server["tools"].items():
                        if name != "knowledge_candidate_decide":
                            self.assertEqual(tool["approval_mode"], "approve")


if __name__ == "__main__":
    unittest.main()
