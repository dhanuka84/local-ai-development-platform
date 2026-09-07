"""Synthetic Task A acceptance tests; no application or personal data."""
import unittest
from labels import normalize_label


class LabelTests(unittest.TestCase):
    def test_outer_space(self):
        self.assertEqual(normalize_label("  Release  "), "release")

    def test_internal_whitespace(self):
        self.assertEqual(normalize_label("  Release\t\n  Candidate "), "release candidate")

    def test_unicode_casefold(self):
        self.assertEqual(normalize_label("Straße"), "strasse")

    def test_empty(self):
        self.assertEqual(normalize_label(" \t\n"), "")

    def test_idempotent(self):
        value = normalize_label(" Release\tCANDIDATE ")
        self.assertEqual(normalize_label(value), value)

    def test_preserves_punctuation(self):
        self.assertEqual(normalize_label(" Release: V1.2 "), "release: v1.2")
