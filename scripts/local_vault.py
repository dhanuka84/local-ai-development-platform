#!/usr/bin/env python3
"""Small local credential vault for the development platform.

The vault is an authenticated AES-256-GCM envelope.  Its key is derived from an
operator passphrase with scrypt; the passphrase and derived key are never stored.
Runtime secret files are materialized only below /dev/shm for Docker Compose.
"""

from __future__ import annotations

import argparse
import base64
import getpass
import hashlib
import json
import os
import re
import secrets as secret_generator
import stat
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable, Mapping

try:
    from cryptography.exceptions import InvalidTag
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    from cryptography.hazmat.primitives.kdf.scrypt import Scrypt
except ImportError as exc:  # pragma: no cover - exercised only on an unprepared host
    raise SystemExit(
        "Python package 'cryptography' is required; install requirements/vault.txt"
    ) from exc


SCHEMA = "hybrid-ai/local-vault/v1"
RUNTIME_SCHEMA = "hybrid-ai/local-vault-runtime/v1"
AAD_CONTEXT = b"local-ai-development-platform credential vault\x00v1"
LEGACY_ENV_SECRET_NAMES = ("AUTH_TOKEN", "CONTROLLER_AUTH_TOKEN", "POSTGRES_PASSWORD")
GENERATED_SECRET_NAMES = ("MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD", "BACKUP_ENCRYPTION_KEY")
DEFAULT_SECRET_NAMES = (*LEGACY_ENV_SECRET_NAMES, *GENERATED_SECRET_NAMES)
RUNTIME_SECRET_NAMES = (*LEGACY_ENV_SECRET_NAMES, "MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD")
SECRET_NAME_RE = re.compile(r"^[A-Z][A-Z0-9_]{0,127}$")
DEFAULT_SCRYPT_N = 2**17
MIN_SCRYPT_N = 2**14
MAX_SCRYPT_N = 2**20
SCRYPT_R = 8
SCRYPT_P = 1
KEY_LENGTH = 32
SALT_LENGTH = 16
NONCE_LENGTH = 12
MAX_VAULT_BYTES = 4 * 1024 * 1024
MAX_SECRET_BYTES = 1024 * 1024
MIN_PASSPHRASE_LENGTH = 16


class VaultError(Exception):
    """An expected, safely reportable vault error."""


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def b64encode(value: bytes) -> str:
    return base64.b64encode(value).decode("ascii")


def b64decode(value: Any, field: str) -> bytes:
    if not isinstance(value, str):
        raise VaultError(f"vault field {field!r} must be a base64 string")
    try:
        return base64.b64decode(value, validate=True)
    except (ValueError, TypeError) as exc:
        raise VaultError(f"vault field {field!r} is not valid base64") from exc


def validate_secret_name(name: str) -> str:
    if not SECRET_NAME_RE.fullmatch(name):
        raise VaultError(f"invalid secret name {name!r}; use uppercase letters, digits, and underscores")
    return name


def validate_secrets(values: Mapping[str, Any]) -> dict[str, str]:
    result: dict[str, str] = {}
    for raw_name, raw_value in values.items():
        name = validate_secret_name(raw_name)
        if not isinstance(raw_value, str) or raw_value == "":
            raise VaultError(f"secret {name} must be a non-empty string")
        if "\x00" in raw_value:
            raise VaultError(f"secret {name} contains a NUL byte")
        result[name] = raw_value
    return result


def derive_key(passphrase: str, salt: bytes, n: int, r: int, p: int) -> bytes:
    if not isinstance(passphrase, str) or passphrase == "":
        raise VaultError("vault passphrase is required")
    kdf = Scrypt(salt=salt, length=KEY_LENGTH, n=n, r=r, p=p)
    return kdf.derive(passphrase.encode("utf-8"))


def encrypt_secrets(
    values: Mapping[str, str],
    passphrase: str,
    *,
    created_at: str | None = None,
    scrypt_n: int = DEFAULT_SCRYPT_N,
) -> dict[str, Any]:
    validated = validate_secrets(values)
    if len(passphrase) < MIN_PASSPHRASE_LENGTH:
        raise VaultError(f"vault passphrase must contain at least {MIN_PASSPHRASE_LENGTH} characters")
    if scrypt_n < MIN_SCRYPT_N or scrypt_n > MAX_SCRYPT_N or scrypt_n & (scrypt_n - 1):
        raise VaultError("scrypt n must be a supported power of two")

    salt = os.urandom(SALT_LENGTH)
    nonce = os.urandom(NONCE_LENGTH)
    now = utc_now()
    plaintext = json.dumps(
        {
            "schema": SCHEMA,
            "created_at": created_at or now,
            "updated_at": now,
            "secrets": validated,
        },
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    key = derive_key(passphrase, salt, scrypt_n, SCRYPT_R, SCRYPT_P)
    ciphertext = AESGCM(key).encrypt(nonce, plaintext, AAD_CONTEXT)
    return {
        "schema": SCHEMA,
        "kdf": {
            "name": "scrypt",
            "salt": b64encode(salt),
            "n": scrypt_n,
            "r": SCRYPT_R,
            "p": SCRYPT_P,
            "length": KEY_LENGTH,
        },
        "cipher": {
            "name": "AES-256-GCM",
            "nonce": b64encode(nonce),
            "ciphertext": b64encode(ciphertext),
        },
    }


def decrypt_secrets(document: Mapping[str, Any], passphrase: str) -> tuple[dict[str, str], str]:
    if document.get("schema") != SCHEMA:
        raise VaultError("unsupported vault schema")
    kdf = document.get("kdf")
    cipher = document.get("cipher")
    if not isinstance(kdf, dict) or not isinstance(cipher, dict):
        raise VaultError("vault envelope is incomplete")
    if kdf.get("name") != "scrypt" or cipher.get("name") != "AES-256-GCM":
        raise VaultError("unsupported vault cryptography parameters")

    try:
        n = int(kdf["n"])
        r = int(kdf["r"])
        p = int(kdf["p"])
        length = int(kdf["length"])
    except (KeyError, TypeError, ValueError) as exc:
        raise VaultError("vault KDF parameters are invalid") from exc
    if (
        n < MIN_SCRYPT_N
        or n > MAX_SCRYPT_N
        or n & (n - 1)
        or r != SCRYPT_R
        or p != SCRYPT_P
        or length != KEY_LENGTH
    ):
        raise VaultError("vault KDF parameters are outside the supported security bounds")

    salt = b64decode(kdf.get("salt"), "kdf.salt")
    nonce = b64decode(cipher.get("nonce"), "cipher.nonce")
    ciphertext = b64decode(cipher.get("ciphertext"), "cipher.ciphertext")
    if len(salt) != SALT_LENGTH or len(nonce) != NONCE_LENGTH:
        raise VaultError("vault salt or nonce has an invalid length")

    key = derive_key(passphrase, salt, n, r, p)
    try:
        plaintext = AESGCM(key).decrypt(nonce, ciphertext, AAD_CONTEXT)
    except InvalidTag as exc:
        raise VaultError("vault authentication failed; passphrase is wrong or the vault was modified") from exc
    try:
        payload = json.loads(plaintext)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise VaultError("decrypted vault payload is invalid") from exc
    if not isinstance(payload, dict) or payload.get("schema") != SCHEMA:
        raise VaultError("decrypted vault payload has an unsupported schema")
    created_at = payload.get("created_at")
    if not isinstance(created_at, str) or not created_at:
        raise VaultError("decrypted vault payload has no creation timestamp")
    raw_secrets = payload.get("secrets")
    if not isinstance(raw_secrets, dict):
        raise VaultError("decrypted vault payload has no secret map")
    return validate_secrets(raw_secrets), created_at


def assert_private_regular_file(path: Path, description: str) -> os.stat_result:
    try:
        metadata = path.lstat()
    except FileNotFoundError as exc:
        raise VaultError(f"{description} does not exist: {path}") from exc
    if not stat.S_ISREG(metadata.st_mode) or path.is_symlink():
        raise VaultError(f"{description} must be a regular, non-symlink file: {path}")
    if metadata.st_uid != os.getuid():
        raise VaultError(f"{description} is not owned by the current user: {path}")
    if stat.S_IMODE(metadata.st_mode) & 0o077:
        raise VaultError(f"{description} permissions must be 0600 or stricter: {path}")
    return metadata


def assert_runtime_secret(path: Path, description: str) -> os.stat_result:
    metadata = path.lstat()
    if not stat.S_ISREG(metadata.st_mode) or path.is_symlink():
        raise VaultError(f"{description} must be a regular, non-symlink file: {path}")
    if metadata.st_uid != os.getuid():
        raise VaultError(f"{description} is not owned by the current user: {path}")
    if stat.S_IMODE(metadata.st_mode) != 0o444:
        raise VaultError(f"{description} permissions must be 0444 inside the private tmpfs directory: {path}")
    return metadata


def ensure_private_directory(path: Path) -> None:
    if path.exists():
        metadata = path.lstat()
        if not stat.S_ISDIR(metadata.st_mode) or path.is_symlink():
            raise VaultError(f"private path must be a non-symlink directory: {path}")
        if metadata.st_uid != os.getuid():
            raise VaultError(f"private directory is not owned by the current user: {path}")
        if stat.S_IMODE(metadata.st_mode) & 0o077:
            raise VaultError(f"private directory permissions must be 0700 or stricter: {path}")
        return
    path.mkdir(parents=True, mode=0o700)
    os.chmod(path, 0o700)


def read_vault_bytes(path: Path) -> bytes:
    metadata = assert_private_regular_file(path, "vault")
    if metadata.st_size > MAX_VAULT_BYTES:
        raise VaultError("vault exceeds the maximum supported size")
    return path.read_bytes()


def load_vault(path: Path) -> tuple[dict[str, Any], bytes]:
    raw = read_vault_bytes(path)
    try:
        document = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise VaultError("vault is not valid JSON") from exc
    if not isinstance(document, dict):
        raise VaultError("vault envelope must be a JSON object")
    return document, raw


def atomic_write(
    path: Path, content: bytes, mode: int = 0o600, *, require_private_parent: bool = True
) -> None:
    if require_private_parent:
        ensure_private_directory(path.parent)
    else:
        parent_metadata = path.parent.lstat()
        if (
            not stat.S_ISDIR(parent_metadata.st_mode)
            or path.parent.is_symlink()
            or parent_metadata.st_uid != os.getuid()
        ):
            raise VaultError(f"output parent must be an owned, non-symlink directory: {path.parent}")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        os.fchmod(descriptor, mode)
        with os.fdopen(descriptor, "wb") as output:
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        os.chmod(path, mode)
        directory_fd = os.open(path.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if temporary.exists():
            temporary.unlink()


def save_vault(path: Path, values: Mapping[str, str], passphrase: str, created_at: str | None = None) -> None:
    document = encrypt_secrets(values, passphrase, created_at=created_at)
    encoded = (json.dumps(document, sort_keys=True, indent=2) + "\n").encode("utf-8")
    atomic_write(path, encoded)


def read_passphrase(
    path: Path | None, *, confirm: bool = False, prompt: str = "Vault passphrase"
) -> str:
    if path is not None:
        assert_private_regular_file(path, "passphrase file")
        value = path.read_text(encoding="utf-8").rstrip("\r\n")
    else:
        try:
            value = getpass.getpass(f"{prompt}: ")
            if confirm:
                repeated = getpass.getpass(f"Confirm {prompt.lower()}: ")
                if value != repeated:
                    raise VaultError("vault passphrases do not match")
        except EOFError as exc:
            raise VaultError(
                "vault passphrase unavailable; unlock interactively or use a private --passphrase-file"
            ) from exc
    if len(value) < MIN_PASSPHRASE_LENGTH:
        raise VaultError(f"vault passphrase must contain at least {MIN_PASSPHRASE_LENGTH} characters")
    return value


def default_runtime_directory() -> Path:
    return Path(f"/dev/shm/hybrid-ai-platform-vault-{os.getuid()}")


def assert_tmpfs_runtime_path(path: Path) -> Path:
    absolute = Path(os.path.abspath(path)).resolve(strict=False)
    shm = Path("/dev/shm").resolve(strict=True)
    try:
        common = Path(os.path.commonpath((absolute, shm)))
    except ValueError as exc:
        raise VaultError("runtime directory must be below /dev/shm") from exc
    if common != shm or absolute == shm:
        raise VaultError("runtime directory must be a dedicated directory below /dev/shm")
    probe = absolute
    while not probe.exists():
        probe = probe.parent
    mount_type = ""
    longest_mount = Path("/")
    try:
        mount_lines = Path("/proc/self/mountinfo").read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise VaultError("cannot verify the runtime filesystem type") from exc
    for line in mount_lines:
        before, separator, after = line.partition(" - ")
        fields = before.split()
        filesystem_fields = after.split()
        if not separator or len(fields) < 5 or not filesystem_fields:
            continue
        mount_point = Path(
            fields[4]
            .replace("\\040", " ")
            .replace("\\011", "\t")
            .replace("\\012", "\n")
            .replace("\\134", "\\")
        )
        try:
            contains_probe = Path(os.path.commonpath((probe, mount_point))) == mount_point
        except ValueError:
            contains_probe = False
        if contains_probe and len(mount_point.parts) >= len(longest_mount.parts):
            longest_mount = mount_point
            mount_type = filesystem_fields[0]
    if mount_type != "tmpfs":
        raise VaultError(f"runtime directory must be on tmpfs; {probe} is on {mount_type or 'an unknown filesystem'}")
    return absolute


def runtime_is_current(directory: Path, vault_hash: str, names: Iterable[str]) -> bool:
    expected_names = sorted(names)
    manifest_path = directory / ".manifest.json"
    try:
        ensure_private_directory(directory)
        assert_private_regular_file(manifest_path, "runtime manifest")
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        if manifest != {
            "schema": RUNTIME_SCHEMA,
            "vault_sha256": vault_hash,
            "secret_names": expected_names,
        }:
            return False
        for name in expected_names:
            assert_runtime_secret(directory / name, f"runtime secret {name}")
        allowed = set(expected_names) | {".manifest.json"}
        return all(entry.name in allowed for entry in directory.iterdir())
    except (VaultError, OSError, json.JSONDecodeError):
        return False


def materialize(
    vault_path: Path,
    runtime_directory: Path,
    names: Iterable[str],
    passphrase: str | None,
    *,
    force: bool = False,
) -> bool:
    directory = assert_tmpfs_runtime_path(runtime_directory)
    selected = sorted({validate_secret_name(name) for name in names})
    if not selected:
        raise VaultError("at least one secret name is required")
    document, raw = load_vault(vault_path)
    vault_hash = hashlib.sha256(raw).hexdigest()
    if not force and runtime_is_current(directory, vault_hash, selected):
        return False
    if passphrase is None:
        raise VaultError("vault passphrase is required to refresh runtime secrets")
    values, _ = decrypt_secrets(document, passphrase)
    missing = [name for name in selected if name not in values]
    if missing:
        raise VaultError("vault is missing required secrets: " + ", ".join(missing))

    ensure_private_directory(directory)
    allowed = set(selected) | {".manifest.json"}
    for entry in directory.iterdir():
        if entry.name in allowed:
            continue
        metadata = entry.lstat()
        if not stat.S_ISREG(metadata.st_mode) or entry.is_symlink() or metadata.st_uid != os.getuid():
            raise VaultError(f"refusing unexpected runtime entry: {entry}")
        entry.unlink()
    for name in selected:
        # Compose file-backed secrets retain source permissions. 0444 lets
        # non-root containers read the bind mount; the enclosing 0700 tmpfs
        # directory keeps the source inaccessible to other host users.
        atomic_write(directory / name, values[name].encode("utf-8"), mode=0o444)
    manifest = {
        "schema": RUNTIME_SCHEMA,
        "vault_sha256": vault_hash,
        "secret_names": selected,
    }
    atomic_write(
        directory / ".manifest.json",
        (json.dumps(manifest, sort_keys=True, indent=2) + "\n").encode("utf-8"),
    )
    return True


def recover_runtime(
    vault_path: Path,
    runtime_directory: Path,
    output_path: Path,
    new_passphrase: str,
) -> int:
    """Recover a lost-passphrase vault from its exact current tmpfs generation."""
    if output_path == vault_path or output_path.exists():
        raise VaultError(f"recovery output must be a new file: {output_path}")
    directory = assert_tmpfs_runtime_path(runtime_directory)
    _, raw = load_vault(vault_path)
    vault_hash = hashlib.sha256(raw).hexdigest()
    manifest_path = directory / ".manifest.json"
    assert_private_regular_file(manifest_path, "runtime manifest")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise VaultError("runtime manifest is not valid JSON") from exc
    if not isinstance(manifest, dict) or manifest.get("schema") != RUNTIME_SCHEMA:
        raise VaultError("runtime manifest has an unsupported schema")
    names = manifest.get("secret_names")
    if not isinstance(names, list) or not names or not all(isinstance(name, str) for name in names):
        raise VaultError("runtime manifest has invalid secret names")
    selected = sorted(validate_secret_name(name) for name in names)
    if len(selected) != len(set(selected)):
        raise VaultError("runtime manifest contains duplicate secret names")
    if manifest.get("vault_sha256") != vault_hash or not runtime_is_current(
        directory, vault_hash, selected
    ):
        raise VaultError("runtime credentials do not exactly match the current encrypted vault")

    values: dict[str, str] = {}
    for name in selected:
        path = directory / name
        metadata = assert_runtime_secret(path, f"runtime secret {name}")
        if metadata.st_size > MAX_SECRET_BYTES:
            raise VaultError(f"runtime secret {name} exceeds the maximum supported size")
        try:
            values[name] = path.read_text(encoding="utf-8")
        except UnicodeDecodeError as exc:
            raise VaultError(f"runtime secret {name} is not valid UTF-8") from exc
    values = validate_secrets(values)
    save_vault(output_path, values, new_passphrase)
    recovered, _ = load_decrypted(output_path, new_passphrase)
    if recovered != values:
        raise VaultError("recovered vault verification failed")
    return len(values)


def clean_runtime(runtime_directory: Path) -> bool:
    directory = assert_tmpfs_runtime_path(runtime_directory)
    if not directory.exists():
        return False
    ensure_private_directory(directory)
    for entry in directory.iterdir():
        metadata = entry.lstat()
        if not stat.S_ISREG(metadata.st_mode) or entry.is_symlink() or metadata.st_uid != os.getuid():
            raise VaultError(f"refusing unexpected runtime entry: {entry}")
        entry.unlink()
    directory.rmdir()
    return True


def parse_env_secrets(path: Path, names: Iterable[str]) -> dict[str, str]:
    assert_private_regular_file(path, "environment file")
    wanted = set(names)
    found: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line or line.lstrip().startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        name = name.strip()
        if name not in wanted:
            continue
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        if not value or value.startswith("CHANGE_ME"):
            raise VaultError(f"{path} has no usable value for {name}")
        found[name] = value
    missing = sorted(wanted - found.keys())
    if missing:
        raise VaultError(f"{path} is missing required credentials: " + ", ".join(missing))
    return validate_secrets(found)


def scrub_env_secrets(path: Path, names: Iterable[str]) -> None:
    assert_private_regular_file(path, "environment file")
    wanted = set(names)
    output: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines(keepends=True):
        candidate = line.lstrip()
        name = candidate.split("=", 1)[0].strip() if "=" in candidate else ""
        if name in wanted:
            continue
        output.append(line)
    atomic_write(path, "".join(output).encode("utf-8"), require_private_parent=False)


def load_decrypted(path: Path, passphrase: str) -> tuple[dict[str, str], str]:
    document, _ = load_vault(path)
    return decrypt_secrets(document, passphrase)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Manage the encrypted local platform credential vault")
    parser.add_argument("--vault", type=Path, default=Path(".local/vault.json"))
    parser.add_argument("--passphrase-file", type=Path)
    subparsers = parser.add_subparsers(dest="command", required=True)

    init_parser = subparsers.add_parser("init", help="create a vault with generated credentials")
    init_parser.add_argument("--generate", action="append", default=[], metavar="NAME")

    import_parser = subparsers.add_parser("import-env", help="import credentials from an existing .env")
    import_parser.add_argument("--env-file", type=Path, default=Path(".env"))
    import_parser.add_argument("--name", action="append", default=[], metavar="NAME")
    import_parser.add_argument("--scrub", action="store_true", help="remove imported credentials from .env")

    set_parser = subparsers.add_parser("set", help="replace one credential")
    set_parser.add_argument("name")
    set_parser.add_argument("--generate", action="store_true")

    subparsers.add_parser("list", help="list credential names after authenticating")
    subparsers.add_parser("check", help="authenticate and validate the vault")

    materialize_parser = subparsers.add_parser(
        "materialize", help="decrypt selected credentials into a protected /dev/shm directory"
    )
    materialize_parser.add_argument("--runtime-dir", type=Path, default=default_runtime_directory())
    materialize_parser.add_argument("--name", action="append", default=[], metavar="NAME")
    materialize_parser.add_argument("--force", action="store_true")

    recover_parser = subparsers.add_parser(
        "recover-runtime",
        help="create a new encrypted vault from the exact current tmpfs generation",
    )
    recover_parser.add_argument("--runtime-dir", type=Path, default=default_runtime_directory())
    recover_parser.add_argument("--output", type=Path)

    clean_parser = subparsers.add_parser("clean", help="remove materialized runtime credentials")
    clean_parser.add_argument("--runtime-dir", type=Path, default=default_runtime_directory())
    return parser


def run(args: argparse.Namespace) -> None:
    vault_path: Path = args.vault
    passphrase_path: Path | None = args.passphrase_file

    if args.command == "init":
        if vault_path.exists():
            raise VaultError(f"vault already exists; refusing to overwrite it: {vault_path}")
        names = args.generate or list(DEFAULT_SECRET_NAMES)
        values = {validate_secret_name(name): secret_generator.token_hex(32) for name in names}
        passphrase = read_passphrase(passphrase_path, confirm=True)
        save_vault(vault_path, values, passphrase)
        print(f"created encrypted vault with {len(values)} credentials: {vault_path}")
        return

    if args.command == "import-env":
        names = args.name or list(LEGACY_ENV_SECRET_NAMES)
        imported = parse_env_secrets(args.env_file, names)
        if vault_path.exists():
            passphrase = read_passphrase(passphrase_path)
            values, created_at = load_decrypted(vault_path, passphrase)
            values.update(imported)
        else:
            passphrase = read_passphrase(passphrase_path, confirm=True)
            values, created_at = imported, None
        if not args.name:
            for name in GENERATED_SECRET_NAMES:
                values.setdefault(name, secret_generator.token_hex(32))
        save_vault(vault_path, values, passphrase, created_at)
        if args.scrub:
            scrub_env_secrets(args.env_file, names)
        print(f"imported {len(imported)} credentials into encrypted vault: {vault_path}")
        return

    if args.command == "clean":
        removed = clean_runtime(args.runtime_dir)
        print("removed runtime credentials" if removed else "runtime credentials are already absent")
        return

    if args.command == "materialize":
        names = args.name or list(RUNTIME_SECRET_NAMES)
        document, raw = load_vault(vault_path)
        vault_hash = hashlib.sha256(raw).hexdigest()
        directory = assert_tmpfs_runtime_path(args.runtime_dir)
        selected = sorted({validate_secret_name(name) for name in names})
        if not args.force and runtime_is_current(directory, vault_hash, selected):
            print(f"runtime credentials are current: {directory}")
            return
        passphrase = read_passphrase(passphrase_path)
        # Reuse the already parsed envelope while retaining materialize() as a
        # separately testable boundary.
        del document
        changed = materialize(vault_path, directory, selected, passphrase, force=args.force)
        print(f"materialized {len(selected)} credentials in tmpfs: {directory}" if changed else "unchanged")
        return

    if args.command == "recover-runtime":
        output_path = args.output or vault_path.with_name(f"{vault_path.stem}.recovered{vault_path.suffix}")
        new_passphrase = read_passphrase(
            passphrase_path, confirm=True, prompt="New vault passphrase"
        )
        count = recover_runtime(vault_path, args.runtime_dir, output_path, new_passphrase)
        print(f"recovered {count} credentials into new encrypted vault: {output_path}")
        print("the original encrypted vault was not changed")
        return

    passphrase = read_passphrase(passphrase_path)
    values, created_at = load_decrypted(vault_path, passphrase)
    if args.command == "set":
        name = validate_secret_name(args.name)
        value = secret_generator.token_hex(32) if args.generate else getpass.getpass(f"Value for {name}: ")
        if not value:
            raise VaultError(f"secret {name} cannot be empty")
        values[name] = value
        save_vault(vault_path, values, passphrase, created_at)
        print(f"updated credential {name}")
    elif args.command == "list":
        for name in sorted(values):
            print(name)
    elif args.command == "check":
        print(f"vault is valid and contains {len(values)} credentials")
    else:  # pragma: no cover - argparse enforces the command set
        raise VaultError(f"unsupported command: {args.command}")


def main() -> int:
    os.umask(0o077)
    try:
        run(build_parser().parse_args())
    except (VaultError, OSError) as exc:
        print(f"local-vault: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
