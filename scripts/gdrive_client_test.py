#!/usr/bin/env python3
"""Unit tests for the vault-native Google Drive client."""

from __future__ import annotations

import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path

import gdrive_client


class DriveClientTest(unittest.TestCase):
    def test_folder_url_and_raw_id_are_accepted(self) -> None:
        folder_id = "example-folder-id-for-unit-tests-12345"
        self.assertEqual(gdrive_client.parse_folder_id(folder_id), folder_id)
        self.assertEqual(
            gdrive_client.parse_folder_id(
                f"https://drive.google.com/drive/folders/{folder_id}?usp=drive_link"
            ),
            folder_id,
        )
        with self.assertRaises(gdrive_client.DriveClientError):
            gdrive_client.parse_folder_id("https://example.com/drive/folders/not-google")

    def test_folder_id_is_vault_backed_not_a_cli_argument(self) -> None:
        args = gdrive_client.build_parser().parse_args(["check"])
        self.assertFalse(hasattr(args, "folder_id"))
        self.assertEqual(gdrive_client.FOLDER_ID, "GDRIVE_FOLDER_ID")

    def test_encryption_key_requires_32_hex_bytes(self) -> None:
        key = gdrive_client.parse_encryption_key("a5" * 32)
        self.assertEqual(len(key), 32)
        for invalid in ("", "a5" * 31, "zz" * 32, "A5" * 32):
            with self.subTest(invalid=invalid):
                with self.assertRaises(gdrive_client.DriveClientError):
                    gdrive_client.parse_encryption_key(invalid)

    def test_encryption_round_trip_and_authentication(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            os.chmod(root, 0o700)
            source = root / "source.tar.gz"
            encrypted = root / "source.tar.gz.enc"
            restored = root / "restored.tar.gz"
            source.write_bytes(os.urandom(gdrive_client.CHUNK_SIZE + 137))
            os.chmod(source, 0o600)
            key = os.urandom(32)

            encrypted_metadata = gdrive_client.encrypt_file(source, encrypted, key)
            restored_metadata = gdrive_client.decrypt_file(
                encrypted,
                restored,
                key,
                expected_ciphertext_sha256=encrypted_metadata["ciphertext_sha256"],
            )

            self.assertEqual(restored.read_bytes(), source.read_bytes())
            self.assertEqual(
                restored_metadata["plaintext_sha256"], hashlib.sha256(source.read_bytes()).hexdigest()
            )
            self.assertEqual(encrypted.stat().st_mode & 0o777, 0o600)
            self.assertEqual(restored.stat().st_mode & 0o777, 0o600)

    def test_tamper_does_not_release_plaintext(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            os.chmod(root, 0o700)
            source = root / "manifest.json"
            encrypted = root / "manifest.json.enc"
            output = root / "manifest.restored.json"
            source.write_bytes(b"authenticated backup content")
            os.chmod(source, 0o600)
            key = os.urandom(32)
            gdrive_client.encrypt_file(source, encrypted, key)
            content = bytearray(encrypted.read_bytes())
            content[len(gdrive_client.ENVELOPE_MAGIC) + gdrive_client.NONCE_LENGTH] ^= 1
            encrypted.write_bytes(content)

            with self.assertRaises(gdrive_client.DriveClientError):
                gdrive_client.decrypt_file(encrypted, output, key)

            self.assertFalse(output.exists())
            self.assertFalse((root / ".manifest.restored.json.partial").exists())

    def test_bundle_validation_checks_manifest_stamp_and_every_hash(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            os.chmod(root, 0o700)
            archive = root / "postgres-data.tar.gz"
            manifest = root / "manifest.json"
            checksums = root / "SHA256SUMS.txt"
            archive.write_bytes(b"archive")
            manifest.write_text(
                json.dumps(
                    {
                        "schema": gdrive_client.BACKUP_SCHEMA,
                        "stamp": "20260830-200000Z",
                    }
                ),
                encoding="utf-8",
            )
            checksums.write_text(
                "".join(
                    f"{gdrive_client.sha256_file(path)}  {path.name}\n"
                    for path in (manifest, archive)
                ),
                encoding="utf-8",
            )
            for path in root.iterdir():
                os.chmod(path, 0o600)

            files = gdrive_client.validate_bundle(root, "20260830-200000Z")
            self.assertEqual({path.name for path in files}, {path.name for path in root.iterdir()})
            archive.write_bytes(b"modified")
            with self.assertRaises(gdrive_client.DriveClientError):
                gdrive_client.validate_bundle(root, "20260830-200000Z")


if __name__ == "__main__":
    unittest.main()
