"""Synthetic Task B tests applying the same normalization principle."""
import unittest
from keys import canonical_key


class KeyTests(unittest.TestCase):
    def test_outer_space(self):
        self.assertEqual(canonical_key("  Release  "), "release")

    def test_internal_whitespace(self):
        self.assertEqual(canonical_key("  Release\t\n  Candidate "), "release-candidate")

    def test_unicode_casefold(self):
        self.assertEqual(canonical_key("Straße"), "strasse")

    def test_empty(self):
        self.assertEqual(canonical_key(" \t\n"), "")

    def test_idempotent(self):
        value = canonical_key(" Release\tCANDIDATE ")
        self.assertEqual(canonical_key(value), value)

    def test_preserves_punctuation(self):
        self.assertEqual(canonical_key(" Release: V1.2 "), "release:-v1.2")
