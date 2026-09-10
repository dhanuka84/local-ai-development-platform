from __future__ import annotations

import copy
import json
import os
import stat
import tempfile
import unittest
from unittest import mock
from argparse import Namespace
from pathlib import Path

from cryptography.exceptions import InvalidTag

import local_vault


class LocalVaultTest(unittest.TestCase):
    passphrase = "correct horse battery staple"

    def test_unavailable_passphrase_fails_with_actionable_error(self) -> None:
        with mock.patch.object(local_vault.getpass, "getpass", side_effect=EOFError):
            with self.assertRaisesRegex(local_vault.VaultError, "unlock interactively"):
                local_vault.read_passphrase(None)

    def test_backup_key_is_generated_but_not_materialized_for_compose(self) -> None:
        self.assertIn("BACKUP_ENCRYPTION_KEY", local_vault.DEFAULT_SECRET_NAMES)
        self.assertNotIn("BACKUP_ENCRYPTION_KEY", local_vault.RUNTIME_SECRET_NAMES)
        self.assertNotIn("RCLONE_CONFIG_PASS", local_vault.DEFAULT_SECRET_NAMES)

    def test_encrypt_decrypt_round_trip_and_fresh_nonce(self) -> None:
        values = {
            "AUTH_TOKEN": "human-secret",
            "CONTROLLER_AUTH_TOKEN": "controller-secret",
            "POSTGRES_PASSWORD": "database-secret",
        }
        first = local_vault.encrypt_secrets(values, self.passphrase, scrypt_n=local_vault.MIN_SCRYPT_N)
        second = local_vault.encrypt_secrets(values, self.passphrase, scrypt_n=local_vault.MIN_SCRYPT_N)

        decrypted, created_at = local_vault.decrypt_secrets(first, self.passphrase)

        self.assertEqual(values, decrypted)
        self.assertTrue(created_at.endswith("Z"))
        self.assertNotEqual(first["cipher"]["nonce"], second["cipher"]["nonce"])
        self.assertNotEqual(first["cipher"]["ciphertext"], second["cipher"]["ciphertext"])

    def test_wrong_passphrase_and_tampering_fail_closed(self) -> None:
        document = local_vault.encrypt_secrets(
            {"AUTH_TOKEN": "secret"}, self.passphrase, scrypt_n=local_vault.MIN_SCRYPT_N
        )
        with self.assertRaises(local_vault.VaultError):
            local_vault.decrypt_secrets(document, "this passphrase is wrong")

        tampered = copy.deepcopy(document)
        ciphertext = bytearray(local_vault.b64decode(tampered["cipher"]["ciphertext"], "ciphertext"))
        ciphertext[0] ^= 1
        tampered["cipher"]["ciphertext"] = local_vault.b64encode(bytes(ciphertext))
        with self.assertRaises(local_vault.VaultError):
            local_vault.decrypt_secrets(tampered, self.passphrase)

    def test_private_file_permissions_are_enforced(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            vault = Path(temporary) / "vault.json"
            local_vault.save_vault(vault, {"AUTH_TOKEN": "secret"}, self.passphrase)
            self.assertEqual(stat.S_IMODE(vault.stat().st_mode), 0o600)
            os.chmod(vault, 0o644)
            with self.assertRaises(local_vault.VaultError):
                local_vault.load_vault(vault)

    @unittest.skipUnless(Path("/dev/shm").is_dir(), "/dev/shm is required")
    def test_materialize_is_tmpfs_only_private_and_reusable(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            vault = Path(temporary) / "vault.json"
            local_vault.save_vault(
                vault,
                {"AUTH_TOKEN": "secret", "POSTGRES_PASSWORD": "database"},
                self.passphrase,
            )
            runtime = Path(tempfile.mkdtemp(prefix="local-vault-test-", dir="/dev/shm"))
            runtime.rmdir()
            try:
                changed = local_vault.materialize(
                    vault, runtime, ["AUTH_TOKEN", "POSTGRES_PASSWORD"], self.passphrase
                )
                self.assertTrue(changed)
                self.assertEqual((runtime / "AUTH_TOKEN").read_text(), "secret")
                self.assertEqual(stat.S_IMODE(runtime.stat().st_mode), 0o700)
                self.assertEqual(stat.S_IMODE((runtime / "AUTH_TOKEN").stat().st_mode), 0o444)
                self.assertFalse(
                    local_vault.materialize(
                        vault, runtime, ["AUTH_TOKEN", "POSTGRES_PASSWORD"], None
                    )
                )
            finally:
                local_vault.clean_runtime(runtime)

    def test_runtime_directory_rejects_disk_paths(self) -> None:
        with self.assertRaises(local_vault.VaultError):
            local_vault.assert_tmpfs_runtime_path(Path("/tmp/local-vault"))

    @unittest.skipUnless(Path("/dev/shm").is_dir(), "/dev/shm is required")
    def test_recover_runtime_requires_exact_vault_generation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            vault = Path(temporary) / "vault.json"
            recovered_vault = Path(temporary) / "vault.recovered.json"
            values = {"AUTH_TOKEN": "secret", "POSTGRES_PASSWORD": "database"}
            local_vault.save_vault(vault, values, self.passphrase)
            runtime = Path(tempfile.mkdtemp(prefix="local-vault-recovery-test-", dir="/dev/shm"))
            runtime.rmdir()
            try:
                local_vault.materialize(vault, runtime, values, self.passphrase)
                count = local_vault.recover_runtime(
                    vault, runtime, recovered_vault, "a new recovery passphrase"
                )
                recovered, _ = local_vault.load_decrypted(
                    recovered_vault, "a new recovery passphrase"
                )
                self.assertEqual(count, 2)
                self.assertEqual(recovered, values)

                recovered_vault.unlink()
                local_vault.save_vault(vault, values, "a different vault passphrase")
                with self.assertRaises(local_vault.VaultError):
                    local_vault.recover_runtime(
                        vault, runtime, recovered_vault, "another recovery passphrase"
                    )
            finally:
                local_vault.clean_runtime(runtime)

    def test_import_and_scrub_env(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            os.chmod(temporary, 0o755)
            env_file = Path(temporary) / ".env"
            passphrase_file = Path(temporary) / "passphrase"
            vault = Path(temporary) / "private/vault.json"
            env_file.write_text(
                "APP_ENV=local\nAUTH_TOKEN=human\nCONTROLLER_AUTH_TOKEN=controller\n"
                "POSTGRES_PASSWORD=database\nLOG_LEVEL=info\n"
            )
            os.chmod(env_file, 0o600)
            passphrase_file.write_text(self.passphrase)
            os.chmod(passphrase_file, 0o600)
            local_vault.run(
                Namespace(
                    command="import-env",
                    vault=vault,
                    passphrase_file=passphrase_file,
                    env_file=env_file,
                    name=[],
                    scrub=True,
                )
            )
            values, _ = local_vault.load_decrypted(vault, self.passphrase)
            self.assertEqual(values["POSTGRES_PASSWORD"], "database")
            self.assertEqual(set(local_vault.DEFAULT_SECRET_NAMES), set(values))
            self.assertNotEqual(values["MINIO_ROOT_USER"], values["MINIO_ROOT_PASSWORD"])
            scrubbed = env_file.read_text()
            self.assertNotIn("AUTH_TOKEN", scrubbed)
            self.assertNotIn("POSTGRES_PASSWORD", scrubbed)
            self.assertIn("LOG_LEVEL=info", scrubbed)

            with env_file.open("a", encoding="utf-8") as handle:
                handle.write("GDRIVE_FOLDER_ID=https://drive.google.com/drive/folders/example-folder-id\n")
            local_vault.run(
                Namespace(
                    command="import-env",
                    vault=vault,
                    passphrase_file=passphrase_file,
                    env_file=env_file,
                    name=["GDRIVE_FOLDER_ID"],
                    scrub=True,
                )
            )
            values, _ = local_vault.load_decrypted(vault, self.passphrase)
            self.assertEqual(
                values["GDRIVE_FOLDER_ID"],
                "https://drive.google.com/drive/folders/example-folder-id",
            )
            self.assertNotIn("GDRIVE_FOLDER_ID", env_file.read_text())

    def test_envelope_contains_no_plaintext_secret(self) -> None:
        document = local_vault.encrypt_secrets(
            {"AUTH_TOKEN": "never-store-this-plaintext"},
            self.passphrase,
            scrypt_n=local_vault.MIN_SCRYPT_N,
        )
        self.assertNotIn("never-store-this-plaintext", json.dumps(document))


if __name__ == "__main__":
    unittest.main()
