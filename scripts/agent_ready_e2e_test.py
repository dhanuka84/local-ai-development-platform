#!/usr/bin/env python3
"""Prevent missing or skipped E2E tests from becoming a successful receipt."""
import unittest

from agent_ready_e2e import MODULE, coverage_report, load_coverage, test_outcomes


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
        self.assertEqual(report[0]["delivery_status"], "open")


if __name__ == "__main__":
    unittest.main()
