#!/usr/bin/env python3
"""Prevent missing or skipped E2E tests from becoming a successful receipt."""
import unittest
import json
from pathlib import Path
import tempfile

from agent_ready_e2e import MODULE, ROOT, coverage_report, functional_markdown, functional_summary, load_coverage, test_outcomes


class EvidenceReceiptTests(unittest.TestCase):
    def test_every_checklist_item_has_an_explicit_evidence_boundary(self):
        self.assertTrue(load_coverage())

    def test_missing_skipped_and_failed_checks_are_not_success(self):
        rows = [{"id": "L01", "checks": ["internal/e2e:TestRequired"], "boundary": "synthetic"}]
        for result in (None, "skip", "fail"):
            events = [] if result is None else [{"Package": MODULE + "internal/e2e", "Test": "TestRequired", "Action": result}]
            report = coverage_report(rows, test_outcomes(events))
            self.assertEqual(report[0]["regression_status"], "fail")

    def test_later_pass_does_not_erase_failed_attempt(self):
        events = [{"Package": MODULE + "internal/e2e", "Test": "TestRequired", "Action": action}
                  for action in ("fail", "pass")]
        self.assertEqual(test_outcomes(events)["internal/e2e:TestRequired"], "fail")

    def test_subtest_must_execute_even_if_parent_passes(self):
        rows = [{"id": "E01", "checks": ["internal/e2e:TestRuntime/Revocation"], "boundary": "synthetic"}]
        events = [{"Package": MODULE + "internal/e2e", "Test": "TestRuntime", "Action": "pass"}]
        self.assertEqual(coverage_report(rows, test_outcomes(events))[0]["regression_status"], "fail")

    def test_passing_regression_does_not_complete_open_delivery(self):
        rows = [{"id": "L18", "checks": ["internal/e2e:TestRuntime"],
                 "delivery_status": "open", "boundary": "live vault unlock required"}]
        report = coverage_report(rows, {"internal/e2e:TestRuntime": "pass"})
        self.assertEqual(report[0]["regression_status"], "pass")
        self.assertEqual(report[0]["functional_status"], "pass")
        self.assertEqual(report[0]["delivery_status"], "open")

    def test_deferring_rollout_never_excuses_a_functional_failure(self):
        rows = [{"id": "L18", "checks": ["internal/e2e:TestRestart"],
                 "delivery_status": "open", "functional_requirement": "Restart preserves state",
                 "deferred_acceptance": ["Live vault unlock and cutover"], "boundary": "synthetic"}]
        for action in (None, "skip", "fail"):
            outcomes = {} if action is None else {"internal/e2e:TestRestart": action}
            coverage = coverage_report(rows, outcomes)
            report = functional_summary(coverage, [{"package": "internal/e2e", "exit_code": 0}])
            self.assertEqual(report["status"], "fail")
            self.assertEqual(report["failed_ids"], ["L18"])
            self.assertEqual(report["passed"], 0)

    def test_unmapped_suite_failure_still_fails_functional_acceptance(self):
        coverage = [{"id": "L01", "functional_status": "pass"}]
        report = functional_summary(coverage, [{"package": "internal/postgres", "exit_code": 1}])
        self.assertEqual(report["status"], "fail")
        self.assertEqual(report["failed_suites"], ["internal/postgres"])
        self.assertEqual(functional_summary([], [])["status"], "fail")

    def test_functional_report_keeps_deferred_acceptance_visible(self):
        rows = [{"id": "L18", "checks": ["internal/e2e:TestRestart"],
                 "delivery_status": "open", "functional_requirement": "Restart preserves state",
                 "deferred_acceptance": ["Live vault unlock and cutover"], "boundary": "synthetic"}]
        coverage = coverage_report(rows, {"internal/e2e:TestRestart": "pass"})
        acceptance = functional_summary(coverage, [{"package": "internal/e2e", "exit_code": 0}])
        self.assertEqual(acceptance["status"], "pass")
        self.assertEqual(coverage[0]["delivery_status"], "open")
        rendered = functional_markdown(coverage, acceptance)
        self.assertIn("| L18 | pass | Restart preserves state |", rendered)
        self.assertIn("Live vault unlock and cutover", rendered)

    def test_scope_requires_functional_criteria_and_explicit_deferrals(self):
        original = json.loads((ROOT / "tests/agent-ready-coverage.json").read_text())
        for invalid in ("missing_criterion", "missing_deferral", "inconsistent_completed_delivery"):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / "tests").mkdir()
                (root / "docs").mkdir()
                (root / "docs/agent-ready-gap-checklist.md").write_text((ROOT / "docs/agent-ready-gap-checklist.md").read_text())
                mapping = json.loads(json.dumps(original))
                row = next(row for row in mapping["items"] if row["id"] == "L18")
                if invalid == "missing_criterion":
                    del row["functional_requirement"]
                elif invalid == "missing_deferral":
                    row["deferred_acceptance"] = []
                else:
                    mapping["items"][0]["deferred_acceptance"] = ["Incomplete rollout"]
                (root / "tests/agent-ready-coverage.json").write_text(json.dumps(mapping))
                with self.assertRaises(ValueError):
                    load_coverage(root)


if __name__ == "__main__":
    unittest.main()
