"""Local tests for private pilot setup; no models, Docker, or live services."""
import contextlib
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock

import agent_ready_pilot_prepare as prepare


class PilotSetupTests(unittest.TestCase):
    def test_scoped_workload_and_private_non_overwriting_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "examples/agent-ready-pilot/source"
            source.mkdir(parents=True)
            for filename in ("test_labels.py", "test_keys.py"):
                (source / filename).write_text("# synthetic setup fixture\n")
            runtime = root / ".local/pilot-test"
            output = io.StringIO()
            with mock.patch.object(prepare, "__file__", str(root / "scripts/prepare.py")), \
                    mock.patch.object(prepare.subprocess, "run"), \
                    mock.patch.object(prepare.subprocess, "check_output", return_value="a" * 40), \
                    contextlib.redirect_stdout(output):
                prepare.prepare(runtime, "pilot-test", "pilot_test", "fixture:local")
                with self.assertRaises(FileExistsError):
                    prepare.prepare(runtime, "pilot-test", "pilot_test", "fixture:local")
                with self.assertRaises(ValueError):
                    prepare.prepare(root, "pilot-test", "pilot_test", "fixture:local")
            identity = json.loads((runtime / "state/principals.json").read_text())[0]
            self.assertFalse(identity["human"])
            self.assertEqual(identity["project_ids"], ["pilot-test"])
            self.assertEqual(set(identity["roles"]), {"controller", "development", "validation_executor"})
            token = (runtime / "state/workload.token").read_text()
            self.assertGreaterEqual(len(token), 48)
            self.assertNotIn(token, output.getvalue())
            for path in (runtime / "runtime.env", runtime / "state/workload.token",
                         runtime / "state/principals.json"):
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            packet = json.loads((runtime / "state/pilot.json").read_text())["task_a"]
            self.assertTrue(packet["local_only"])
            self.assertFalse(packet["cloud_review"])
            self.assertEqual(packet["allowed_files"], ["labels.py"])
            self.assertEqual(packet["base_revision"], "a" * 40)
            self.assertEqual(packet["checks"][1]["argv"], ["python3", "-m", "unittest", "-v", "test_labels.py"])
            self.assertEqual(packet["limits"]["max_changed_files"], 1)
            self.assertEqual(stat.S_IMODE(runtime.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE((runtime / "state").stat().st_mode), 0o700)

    def test_invalid_scope_does_not_create_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runtime = root / ".local/pilot-test"
            with mock.patch.object(prepare, "__file__", str(root / "scripts/prepare.py")):
                for project, database in (("live", "pilot_test"), ("pilot-test", "live"),
                                          ("pilot-test", "pilot_test\nINJECT=true")):
                    with self.subTest(project=project, database=database):
                        with self.assertRaises(ValueError):
                            prepare.prepare(runtime, project, database, "fixture:local")
                        self.assertFalse(runtime.exists())


if __name__ == "__main__":
    unittest.main()
