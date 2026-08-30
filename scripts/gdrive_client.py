#!/usr/bin/env python3
"""Vault-native Google Drive client for encrypted cold-backup bundles.

The Drive destination, OAuth credentials, and client-side encryption key are
read from the local encrypted vault.  The Google account password is never
requested or stored.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import sys
import tempfile
from pathlib import Path
from typing import Any, Mapping
from urllib.parse import urlparse

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

import local_vault


DRIVE_SCOPE = "https://www.googleapis.com/auth/drive.file"
FOLDER_MIME_TYPE = "application/vnd.google-apps.folder"
TOKEN_URI = "https://oauth2.googleapis.com/token"
BACKUP_SCHEMA = "hybrid-ai/cold-volume-backup/v1"
ENVELOPE_MAGIC = b"HYBRIDAI-GDRIVE\x00\x01"
ENVELOPE_AAD = b"local-ai-development-platform Google Drive backup envelope\x00v1"
NONCE_LENGTH = 12
TAG_LENGTH = 16
CHUNK_SIZE = 8 * 1024 * 1024
MAX_NAME_LENGTH = 255
SAFE_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$")
SAFE_ID_RE = re.compile(r"^[A-Za-z0-9_-]{10,256}$")
SAFE_STAMP_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
CHECKSUM_RE = re.compile(r"^([0-9a-f]{64}) [ *]([^/]+)$")
CLIENT_ID = "GDRIVE_CLIENT_ID"
CLIENT_SECRET = "GDRIVE_CLIENT_SECRET"
REFRESH_TOKEN = "GDRIVE_REFRESH_TOKEN"
FOLDER_ID = "GDRIVE_FOLDER_ID"
ENCRYPTION_KEY = "BACKUP_ENCRYPTION_KEY"


class DriveClientError(Exception):
    """An expected error safe to show without exposing credentials."""


def import_google() -> tuple[Any, Any, Any, Any, Any]:
    try:
        from google.auth.transport.requests import Request
        from google.oauth2.credentials import Credentials
        from google_auth_oauthlib.flow import InstalledAppFlow
        from googleapiclient.discovery import build
        from googleapiclient.http import MediaFileUpload, MediaIoBaseDownload
    except ImportError as exc:  # pragma: no cover - depends on workstation setup
        raise DriveClientError(
            "Google API packages are required; install requirements/gdrive.txt"
        ) from exc
    return Request, Credentials, InstalledAppFlow, build, (MediaFileUpload, MediaIoBaseDownload)


def require_secrets(values: Mapping[str, str], names: tuple[str, ...]) -> None:
    missing = [name for name in names if not values.get(name)]
    if missing:
        raise DriveClientError(
            "vault is missing required credentials: " + ", ".join(missing)
        )


def load_vault_values(
    vault_path: Path, passphrase_path: Path | None, required: tuple[str, ...]
) -> tuple[dict[str, str], str, str]:
    passphrase = local_vault.read_passphrase(passphrase_path)
    values, created_at = local_vault.load_decrypted(vault_path, passphrase)
    require_secrets(values, required)
    return values, passphrase, created_at


def parse_folder_id(value: str) -> str:
    candidate = value.strip()
    if candidate.startswith(("https://", "http://")):
        parsed = urlparse(candidate)
        if parsed.hostname not in {"drive.google.com", "www.drive.google.com"}:
            raise DriveClientError("Google Drive folder URL has an unexpected host")
        match = re.search(r"/folders/([A-Za-z0-9_-]+)", parsed.path)
        if match is None:
            raise DriveClientError("Google Drive URL does not contain a folder ID")
        candidate = match.group(1)
    if not SAFE_ID_RE.fullmatch(candidate):
        raise DriveClientError("Google Drive folder ID is invalid")
    return candidate


def parse_stamp(value: str) -> str:
    if not SAFE_STAMP_RE.fullmatch(value):
        raise DriveClientError("backup stamp contains unsafe characters")
    return value


def parse_encryption_key(value: str) -> bytes:
    if not re.fullmatch(r"[0-9a-f]{64}", value):
        raise DriveClientError(
            f"{ENCRYPTION_KEY} must be a 64-character lowercase hexadecimal value"
        )
    try:
        key = bytes.fromhex(value)
    except ValueError as exc:
        raise DriveClientError(
            f"{ENCRYPTION_KEY} must be a 64-character lowercase hexadecimal value"
        ) from exc
    if len(key) != 32 or len(value) != 64:
        raise DriveClientError(
            f"{ENCRYPTION_KEY} must be a 64-character lowercase hexadecimal value"
        )
    return key


def assert_owned_regular_file(path: Path, description: str) -> os.stat_result:
    try:
        metadata = path.lstat()
    except FileNotFoundError as exc:
        raise DriveClientError(f"{description} does not exist: {path}") from exc
    if not stat.S_ISREG(metadata.st_mode) or path.is_symlink():
        raise DriveClientError(f"{description} must be a regular, non-symlink file: {path}")
    if metadata.st_uid != os.getuid():
        raise DriveClientError(f"{description} is not owned by the current user: {path}")
    return metadata


def assert_owned_private_directory(path: Path, description: str) -> None:
    try:
        metadata = path.lstat()
    except FileNotFoundError as exc:
        raise DriveClientError(f"{description} does not exist: {path}") from exc
    if not stat.S_ISDIR(metadata.st_mode) or path.is_symlink():
        raise DriveClientError(f"{description} must be a non-symlink directory: {path}")
    if metadata.st_uid != os.getuid():
        raise DriveClientError(f"{description} is not owned by the current user: {path}")
    if stat.S_IMODE(metadata.st_mode) & 0o077:
        raise DriveClientError(f"{description} permissions must be 0700 or stricter: {path}")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(CHUNK_SIZE):
            digest.update(chunk)
    return digest.hexdigest()


def safe_bundle_files(directory: Path) -> list[Path]:
    assert_owned_private_directory(directory, "backup directory")
    result: list[Path] = []
    for path in sorted(directory.iterdir(), key=lambda item: item.name):
        if not SAFE_NAME_RE.fullmatch(path.name) or len(path.name) > MAX_NAME_LENGTH:
            raise DriveClientError(f"backup contains an unsafe file name: {path.name!r}")
        assert_owned_regular_file(path, "backup entry")
        result.append(path)
    if not result:
        raise DriveClientError("backup directory is empty")
    return result


def parse_checksum_file(path: Path) -> dict[str, str]:
    checksums: dict[str, str] = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except UnicodeDecodeError as exc:
        raise DriveClientError("SHA256SUMS.txt is not valid UTF-8") from exc
    for line in lines:
        match = CHECKSUM_RE.fullmatch(line)
        if match is None:
            raise DriveClientError("SHA256SUMS.txt contains an invalid entry")
        digest, name = match.groups()
        if name in checksums:
            raise DriveClientError(f"SHA256SUMS.txt contains duplicate entry {name}")
        checksums[name] = digest
    if not checksums:
        raise DriveClientError("SHA256SUMS.txt contains no entries")
    return checksums


def validate_bundle(directory: Path, stamp: str) -> list[Path]:
    files = safe_bundle_files(directory)
    by_name = {path.name: path for path in files}
    for required in ("manifest.json", "SHA256SUMS.txt"):
        if required not in by_name:
            raise DriveClientError(f"backup is missing {required}")
    checksums = parse_checksum_file(by_name["SHA256SUMS.txt"])
    expected_names = set(by_name) - {"SHA256SUMS.txt"}
    if set(checksums) != expected_names:
        raise DriveClientError("SHA256SUMS.txt does not describe the complete backup bundle")
    for name, expected in checksums.items():
        if sha256_file(by_name[name]) != expected:
            raise DriveClientError(f"plaintext checksum validation failed for {name}")
    try:
        manifest = json.loads(by_name["manifest.json"].read_text(encoding="utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise DriveClientError("manifest.json is invalid") from exc
    if (
        not isinstance(manifest, dict)
        or manifest.get("schema") != BACKUP_SCHEMA
        or manifest.get("stamp") != stamp
    ):
        raise DriveClientError("manifest schema or stamp does not match the requested backup")
    return files


def encrypt_file(source_path: Path, destination_path: Path, key: bytes) -> dict[str, Any]:
    assert_owned_regular_file(source_path, "plaintext input")
    if destination_path.exists() or destination_path.is_symlink():
        raise DriveClientError(f"encrypted output already exists: {destination_path}")
    nonce = os.urandom(NONCE_LENGTH)
    encryptor = Cipher(algorithms.AES(key), modes.GCM(nonce)).encryptor()
    encryptor.authenticate_additional_data(ENVELOPE_AAD)
    plaintext_digest = hashlib.sha256()
    ciphertext_digest = hashlib.sha256()
    header = ENVELOPE_MAGIC + nonce
    try:
        descriptor = os.open(destination_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "wb") as destination, source_path.open("rb") as source:
            destination.write(header)
            ciphertext_digest.update(header)
            while chunk := source.read(CHUNK_SIZE):
                plaintext_digest.update(chunk)
                encrypted = encryptor.update(chunk)
                destination.write(encrypted)
                ciphertext_digest.update(encrypted)
            final = encryptor.finalize()
            destination.write(final)
            ciphertext_digest.update(final)
            destination.write(encryptor.tag)
            ciphertext_digest.update(encryptor.tag)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        destination_path.unlink(missing_ok=True)
        raise
    return {
        "plaintext_sha256": plaintext_digest.hexdigest(),
        "plaintext_size": source_path.stat().st_size,
        "ciphertext_sha256": ciphertext_digest.hexdigest(),
        "ciphertext_size": destination_path.stat().st_size,
    }


def decrypt_file(
    source_path: Path,
    destination_path: Path,
    key: bytes,
    *,
    expected_ciphertext_sha256: str | None = None,
) -> dict[str, Any]:
    metadata = assert_owned_regular_file(source_path, "encrypted input")
    minimum_size = len(ENVELOPE_MAGIC) + NONCE_LENGTH + TAG_LENGTH
    if metadata.st_size < minimum_size:
        raise DriveClientError("encrypted input is truncated")
    if destination_path.exists() or destination_path.is_symlink():
        raise DriveClientError(f"plaintext output already exists: {destination_path}")
    partial_path = destination_path.with_name(f".{destination_path.name}.partial")
    if partial_path.exists() or partial_path.is_symlink():
        raise DriveClientError(f"partial plaintext output already exists: {partial_path}")
    ciphertext_digest = hashlib.sha256()
    plaintext_digest = hashlib.sha256()
    try:
        with source_path.open("rb") as source:
            header = source.read(len(ENVELOPE_MAGIC) + NONCE_LENGTH)
            if header[: len(ENVELOPE_MAGIC)] != ENVELOPE_MAGIC:
                raise DriveClientError("encrypted input has an unsupported envelope")
            nonce = header[len(ENVELOPE_MAGIC) :]
            source.seek(-TAG_LENGTH, os.SEEK_END)
            tag = source.read(TAG_LENGTH)
            ciphertext_length = metadata.st_size - len(header) - TAG_LENGTH
            source.seek(len(header))
            decryptor = Cipher(algorithms.AES(key), modes.GCM(nonce, tag)).decryptor()
            decryptor.authenticate_additional_data(ENVELOPE_AAD)
            ciphertext_digest.update(header)
            remaining = ciphertext_length
            descriptor = os.open(partial_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, "wb") as destination:
                while remaining:
                    chunk = source.read(min(CHUNK_SIZE, remaining))
                    if not chunk:
                        raise DriveClientError("encrypted input ended unexpectedly")
                    remaining -= len(chunk)
                    ciphertext_digest.update(chunk)
                    plaintext = decryptor.update(chunk)
                    destination.write(plaintext)
                    plaintext_digest.update(plaintext)
                ciphertext_digest.update(tag)
                final = decryptor.finalize()
                destination.write(final)
                plaintext_digest.update(final)
                destination.flush()
                os.fsync(destination.fileno())
        actual_ciphertext_sha256 = ciphertext_digest.hexdigest()
        if (
            expected_ciphertext_sha256 is not None
            and actual_ciphertext_sha256 != expected_ciphertext_sha256
        ):
            raise DriveClientError("downloaded ciphertext does not match Google Drive SHA-256")
        os.replace(partial_path, destination_path)
        os.chmod(destination_path, 0o600)
    except InvalidTag as exc:
        partial_path.unlink(missing_ok=True)
        raise DriveClientError(
            "encrypted input authentication failed; it was modified or the encryption key is wrong"
        ) from exc
    except BaseException:
        partial_path.unlink(missing_ok=True)
        raise
    return {
        "plaintext_sha256": plaintext_digest.hexdigest(),
        "plaintext_size": destination_path.stat().st_size,
        "ciphertext_sha256": ciphertext_digest.hexdigest(),
        "ciphertext_size": metadata.st_size,
    }


def oauth_credentials(values: Mapping[str, str]) -> Any:
    Request, Credentials, _, _, _ = import_google()
    require_secrets(values, (CLIENT_ID, CLIENT_SECRET, REFRESH_TOKEN))
    credentials = Credentials(
        token=None,
        refresh_token=values[REFRESH_TOKEN],
        token_uri=TOKEN_URI,
        client_id=values[CLIENT_ID],
        client_secret=values[CLIENT_SECRET],
        scopes=[DRIVE_SCOPE],
    )
    try:
        credentials.refresh(Request())
    except Exception as exc:
        raise DriveClientError(
            "Google OAuth refresh failed; run 'make gdrive-auth' to authorize again"
        ) from exc
    return credentials


def drive_service(credentials: Any) -> Any:
    _, _, _, build, _ = import_google()
    try:
        return build("drive", "v3", credentials=credentials, cache_discovery=False)
    except Exception as exc:
        raise DriveClientError("could not initialize the Google Drive API client") from exc


def api_execute(request: Any, operation: str) -> Any:
    try:
        return request.execute()
    except Exception as exc:
        raise DriveClientError(f"Google Drive {operation} failed") from exc


def check_folder_writable(service: Any, folder_id: str) -> None:
    """Create and trash an empty marker to test drive.file access to a parent."""
    marker = api_execute(
        service.files().create(
            body={
                "name": ".hybrid-ai-write-check",
                "parents": [folder_id],
                "appProperties": {"hybridAiPurpose": "write-check"},
            },
            fields="id,name,parents",
            supportsAllDrives=True,
        ),
        "folder write check",
    )
    if marker.get("parents") != [folder_id]:
        trash_incomplete_folder(service, marker["id"])
        raise DriveClientError("Google Drive write check used an unexpected parent folder")
    try:
        api_execute(
            service.files().update(
                fileId=marker["id"],
                body={"trashed": True},
                fields="id,trashed",
                supportsAllDrives=True,
            ),
            "write-check cleanup",
        )
    except DriveClientError:
        raise DriveClientError(
            "Drive folder is writable, but the temporary marker could not be trashed"
        )


def find_backup_folders(service: Any, parent_id: str, stamp: str) -> list[dict[str, Any]]:
    query = (
        f"'{parent_id}' in parents and trashed = false and mimeType = '{FOLDER_MIME_TYPE}' "
        f"and appProperties has {{ key='hybridAiBackupStamp' and value='{stamp}' }}"
    )
    response = api_execute(
        service.files().list(
            q=query,
            fields="files(id,name,appProperties)",
            spaces="drive",
            includeItemsFromAllDrives=True,
            supportsAllDrives=True,
            pageSize=100,
        ),
        "backup lookup",
    )
    return list(response.get("files", []))


def create_backup_folder(service: Any, parent_id: str, stamp: str) -> dict[str, Any]:
    if find_backup_folders(service, parent_id, stamp):
        raise DriveClientError(f"backup stamp already exists in Google Drive: {stamp}")
    return api_execute(
        service.files().create(
            body={
                "name": stamp,
                "mimeType": FOLDER_MIME_TYPE,
                "parents": [parent_id],
                "appProperties": {
                    "hybridAiSchema": "cold-volume-backup-v1",
                    "hybridAiBackupStamp": stamp,
                    "hybridAiState": "uploading",
                },
            },
            fields="id,name,appProperties",
            supportsAllDrives=True,
        ),
        "folder creation",
    )


def upload_encrypted_file(service: Any, folder_id: str, path: Path, remote_name: str) -> dict[str, Any]:
    _, _, _, _, media_classes = import_google()
    MediaFileUpload, _ = media_classes
    media = MediaFileUpload(
        str(path), mimetype="application/octet-stream", chunksize=CHUNK_SIZE, resumable=True
    )
    request = service.files().create(
        body={
            "name": remote_name,
            "parents": [folder_id],
            "appProperties": {"hybridAiEncrypted": "aes-256-gcm-v1"},
        },
        media_body=media,
        fields="id,name,size,sha256Checksum",
        supportsAllDrives=True,
    )
    response = None
    try:
        while response is None:
            status, response = request.next_chunk()
            if status is not None:
                print(f"Uploading {remote_name}: {int(status.progress() * 100)}%", file=sys.stderr)
    except Exception as exc:
        raise DriveClientError(f"Google Drive resumable upload failed for {remote_name}") from exc
    if not response.get("sha256Checksum"):
        response = api_execute(
            service.files().get(
                fileId=response["id"],
                fields="id,name,size,sha256Checksum",
                supportsAllDrives=True,
            ),
            f"checksum lookup for {remote_name}",
        )
    return response


def mark_folder_complete(service: Any, folder_id: str, stamp: str) -> dict[str, Any]:
    return api_execute(
        service.files().update(
            fileId=folder_id,
            body={
                "appProperties": {
                    "hybridAiSchema": "cold-volume-backup-v1",
                    "hybridAiBackupStamp": stamp,
                    "hybridAiState": "complete",
                }
            },
            fields="id,name,appProperties",
            supportsAllDrives=True,
        ),
        "backup finalization",
    )


def trash_incomplete_folder(service: Any, folder_id: str) -> None:
    try:
        service.files().update(
            fileId=folder_id,
            body={"trashed": True},
            fields="id",
            supportsAllDrives=True,
        ).execute()
    except Exception:
        print(
            "Warning: an incomplete Drive folder could not be moved to trash",
            file=sys.stderr,
        )


def upload_bundle(service: Any, parent_id: str, stamp: str, source_dir: Path, key: bytes) -> dict[str, Any]:
    files = validate_bundle(source_dir, stamp)
    folder = create_backup_folder(service, parent_id, stamp)
    uploaded: list[dict[str, Any]] = []
    try:
        with tempfile.TemporaryDirectory(prefix=".gdrive-encrypted-", dir=source_dir.parent) as name:
            encrypted_directory = Path(name)
            os.chmod(encrypted_directory, 0o700)
            for source_path in files:
                remote_name = f"{source_path.name}.enc"
                encrypted_path = encrypted_directory / remote_name
                envelope = encrypt_file(source_path, encrypted_path, key)
                remote = upload_encrypted_file(service, folder["id"], encrypted_path, remote_name)
                remote_sha256 = remote.get("sha256Checksum")
                if not remote_sha256:
                    raise DriveClientError(
                        f"Google Drive did not provide a SHA-256 checksum for {remote_name}"
                    )
                if remote_sha256 != envelope["ciphertext_sha256"]:
                    raise DriveClientError(
                        f"Google Drive SHA-256 verification failed for {remote_name}"
                    )
                uploaded.append(
                    {
                        "name": remote_name,
                        "size": int(remote.get("size", envelope["ciphertext_size"])),
                        "sha256": remote_sha256,
                    }
                )
                encrypted_path.unlink()
        mark_folder_complete(service, folder["id"], stamp)
    except BaseException:
        trash_incomplete_folder(service, folder["id"])
        raise
    return {
        "schema": "hybrid-ai/gdrive-upload-receipt/v1",
        "stamp": stamp,
        "files": uploaded,
    }


def list_folder_files(service: Any, folder_id: str) -> list[dict[str, Any]]:
    files: list[dict[str, Any]] = []
    page_token = None
    while True:
        response = api_execute(
            service.files().list(
                q=f"'{folder_id}' in parents and trashed = false",
                fields="nextPageToken,files(id,name,mimeType,size,sha256Checksum)",
                spaces="drive",
                includeItemsFromAllDrives=True,
                supportsAllDrives=True,
                pageSize=1000,
                pageToken=page_token,
            ),
            "backup file listing",
        )
        files.extend(response.get("files", []))
        page_token = response.get("nextPageToken")
        if not page_token:
            return files


def download_encrypted_file(service: Any, remote: Mapping[str, Any], destination: Path) -> None:
    _, _, _, _, media_classes = import_google()
    _, MediaIoBaseDownload = media_classes
    request = service.files().get_media(fileId=remote["id"], supportsAllDrives=True)
    try:
        with destination.open("xb") as output:
            os.chmod(destination, 0o600)
            downloader = MediaIoBaseDownload(output, request, chunksize=CHUNK_SIZE)
            done = False
            while not done:
                status, done = downloader.next_chunk()
                if status is not None:
                    print(
                        f"Downloading {remote['name']}: {int(status.progress() * 100)}%",
                        file=sys.stderr,
                    )
            output.flush()
            os.fsync(output.fileno())
    except Exception as exc:
        destination.unlink(missing_ok=True)
        raise DriveClientError(f"Google Drive download failed for {remote['name']}") from exc


def cleanup_created_directory(path: Path) -> None:
    if not path.exists():
        return
    for entry in path.iterdir():
        metadata = entry.lstat()
        if stat.S_ISREG(metadata.st_mode) and not entry.is_symlink():
            entry.unlink()
        else:
            raise DriveClientError(f"refusing unexpected restore work entry: {entry}")
    path.rmdir()


def download_bundle(service: Any, parent_id: str, stamp: str, output_dir: Path, key: bytes) -> dict[str, Any]:
    matches = find_backup_folders(service, parent_id, stamp)
    complete = [
        folder
        for folder in matches
        if folder.get("appProperties", {}).get("hybridAiState") == "complete"
    ]
    if len(complete) != 1:
        raise DriveClientError(
            f"expected exactly one complete Google Drive backup for {stamp}; found {len(complete)}"
        )
    folder = complete[0]
    remote_files = list_folder_files(service, folder["id"])
    if not remote_files:
        raise DriveClientError("Google Drive backup folder is empty")
    if output_dir.exists() or output_dir.is_symlink():
        raise DriveClientError(f"restore output already exists: {output_dir}")
    output_dir.mkdir(parents=False, mode=0o700)
    os.chmod(output_dir, 0o700)
    restored: list[dict[str, Any]] = []
    names: set[str] = set()
    try:
        for remote in sorted(remote_files, key=lambda item: item.get("name", "")):
            remote_name = remote.get("name")
            remote_sha256 = remote.get("sha256Checksum")
            if (
                remote.get("mimeType") == FOLDER_MIME_TYPE
                or not isinstance(remote_name, str)
                or not remote_name.endswith(".enc")
                or not SAFE_NAME_RE.fullmatch(remote_name[:-4])
            ):
                raise DriveClientError("Google Drive backup contains an unexpected object")
            plain_name = remote_name[:-4]
            if plain_name in names:
                raise DriveClientError(f"Google Drive backup contains duplicate file {plain_name}")
            names.add(plain_name)
            if not isinstance(remote_sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", remote_sha256):
                raise DriveClientError(f"Google Drive has no valid SHA-256 for {remote_name}")
            encrypted_path = output_dir / f".{remote_name}.download"
            plaintext_path = output_dir / plain_name
            download_encrypted_file(service, remote, encrypted_path)
            envelope = decrypt_file(
                encrypted_path,
                plaintext_path,
                key,
                expected_ciphertext_sha256=remote_sha256,
            )
            encrypted_path.unlink()
            restored.append(
                {
                    "name": plain_name,
                    "sha256": envelope["plaintext_sha256"],
                    "size": envelope["plaintext_size"],
                }
            )
        validate_bundle(output_dir, stamp)
    except BaseException:
        cleanup_created_directory(output_dir)
        raise
    return {
        "schema": "hybrid-ai/gdrive-download-receipt/v1",
        "stamp": stamp,
        "files": restored,
    }


def authorize(values: dict[str, str]) -> Any:
    _, _, InstalledAppFlow, _, _ = import_google()
    require_secrets(values, (CLIENT_ID, CLIENT_SECRET))
    configuration = {
        "installed": {
            "client_id": values[CLIENT_ID],
            "client_secret": values[CLIENT_SECRET],
            "auth_uri": "https://accounts.google.com/o/oauth2/auth",
            "token_uri": TOKEN_URI,
            "redirect_uris": ["http://localhost"],
        }
    }
    try:
        flow = InstalledAppFlow.from_client_config(configuration, scopes=[DRIVE_SCOPE])
        credentials = flow.run_local_server(
            host="127.0.0.1",
            port=0,
            open_browser=True,
            access_type="offline",
            prompt="consent",
            authorization_prompt_message="Open this URL in your browser:\n{url}",
            success_message="Google Drive authorization completed. You may close this tab.",
        )
    except Exception as exc:
        raise DriveClientError("Google OAuth browser authorization failed") from exc
    if not credentials.refresh_token:
        raise DriveClientError(
            "Google did not return a refresh token; revoke the app grant and run authorization again"
        )
    values[REFRESH_TOKEN] = credentials.refresh_token
    return credentials


def emit_json(value: Mapping[str, Any]) -> None:
    print(json.dumps(value, sort_keys=True, separators=(",", ":")))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Upload and restore client-side-encrypted backups using Google Drive OAuth"
    )
    parser.add_argument(
        "--vault", type=Path, default=Path(os.environ.get("VAULT_FILE", ".local/vault.json"))
    )
    passphrase_default = os.environ.get("VAULT_PASSPHRASE_FILE")
    parser.add_argument(
        "--passphrase-file",
        type=Path,
        default=Path(passphrase_default) if passphrase_default else None,
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("auth", help="authorize Drive in a browser and store the refresh token")
    subparsers.add_parser("check", help="verify OAuth and writable folder access")
    upload_parser = subparsers.add_parser("upload-bundle", help="encrypt and upload one backup bundle")
    upload_parser.add_argument("--stamp", required=True)
    upload_parser.add_argument("--source-dir", type=Path, required=True)
    download_parser = subparsers.add_parser(
        "download-bundle", help="download, authenticate, and decrypt one backup bundle"
    )
    download_parser.add_argument("--stamp", required=True)
    download_parser.add_argument("--output-dir", type=Path, required=True)
    return parser


def run(args: argparse.Namespace) -> None:
    if args.command == "auth":
        values, passphrase, created_at = load_vault_values(
            args.vault, args.passphrase_file, (CLIENT_ID, CLIENT_SECRET, FOLDER_ID)
        )
        folder_id = parse_folder_id(values[FOLDER_ID])
        credentials = authorize(values)
        local_vault.save_vault(args.vault, values, passphrase, created_at)
        service = drive_service(credentials)
        check_folder_writable(service, folder_id)
        emit_json(
            {
                "schema": "hybrid-ai/gdrive-auth/v1",
                "scope": DRIVE_SCOPE,
                "writable": True,
            }
        )
        return

    required = (CLIENT_ID, CLIENT_SECRET, REFRESH_TOKEN, FOLDER_ID)
    if args.command in {"upload-bundle", "download-bundle"}:
        required += (ENCRYPTION_KEY,)
    values, _, _ = load_vault_values(args.vault, args.passphrase_file, required)
    folder_id = parse_folder_id(values[FOLDER_ID])
    service = drive_service(oauth_credentials(values))
    if args.command == "check":
        check_folder_writable(service, folder_id)
        emit_json(
            {
                "schema": "hybrid-ai/gdrive-check/v1",
                "writable": True,
            }
        )
    elif args.command == "upload-bundle":
        emit_json(
            upload_bundle(
                service,
                folder_id,
                parse_stamp(args.stamp),
                args.source_dir,
                parse_encryption_key(values[ENCRYPTION_KEY]),
            )
        )
    elif args.command == "download-bundle":
        emit_json(
            download_bundle(
                service,
                folder_id,
                parse_stamp(args.stamp),
                args.output_dir,
                parse_encryption_key(values[ENCRYPTION_KEY]),
            )
        )
    else:  # pragma: no cover - argparse enforces commands
        raise DriveClientError(f"unsupported command: {args.command}")


def main() -> int:
    os.umask(0o077)
    try:
        run(build_parser().parse_args())
    except (DriveClientError, local_vault.VaultError, OSError) as exc:
        print(f"gdrive-client: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("gdrive-client: interrupted", file=sys.stderr)
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
