#!/usr/bin/env python3
"""Disposable integration test for the cold backup/restore scripts."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import textwrap
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parent.parent
ARCHIVE_IMAGE = "alpine:3.24.0"
LOGICAL_VOLUMES = ("postgres-data", "artifact-data", "etcd-data", "minio-data", "milvus-data")


def run(*args: str, env: dict[str, str] | None = None, capture: bool = False) -> str:
    completed = subprocess.run(
        args,
        cwd=PROJECT_ROOT,
        env=env,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
    )
    return completed.stdout.strip() if capture else ""


def write_fake_gdrive_client(path: Path) -> None:
    path.write_text(
        textwrap.dedent(
            r'''#!/usr/bin/env python3
import json
import os
import shutil
import sys
from pathlib import Path

root = Path(os.environ["FAKE_GDRIVE_ROOT"])
args = sys.argv[1:]

if not args:
    raise SystemExit("missing fake Drive command")
command = args[0]
args = args[1:]

def option(name):
    index = args.index(name)
    return args[index + 1]

if command == "upload-bundle":
    if os.environ.get("FAKE_GDRIVE_FAIL_UPLOAD") == "true":
        raise SystemExit("injected upload failure")
    source = Path(option("--source-dir"))
    stamp = option("--stamp")
    destination = root / stamp
    destination.mkdir(parents=True, exist_ok=False)
    for item in source.iterdir():
        if item.is_file():
            shutil.copy2(item, destination / item.name)
    print(json.dumps({
        "schema": "hybrid-ai/gdrive-upload-receipt/v1",
        "stamp": stamp,
        "files": [],
    }))
elif command == "download-bundle":
    stamp = option("--stamp")
    source = root / stamp
    destination = Path(option("--output-dir"))
    destination.mkdir(parents=False, exist_ok=False)
    for item in source.iterdir():
        if item.is_file():
            shutil.copy2(item, destination / item.name)
    print(json.dumps({
        "schema": "hybrid-ai/gdrive-download-receipt/v1",
        "stamp": stamp,
        "files": [],
    }))
else:
    raise SystemExit("unsupported fake Drive command: " + command)
'''
        ),
        encoding="utf-8",
    )
    path.chmod(0o700)


def main() -> int:
    run("docker", "info", capture=True)
    run("docker", "image", "inspect", ARCHIVE_IMAGE, capture=True)
    project = f"hybrid-ai-backup-test-{os.getpid()}"
    with tempfile.TemporaryDirectory(prefix="hybrid-ai-backup-test-") as temporary:
        root = Path(temporary)
        compose_file = root / "compose.yaml"
        env_file = root / ".env"
        fake_client = root / "fake-gdrive-client"
        write_fake_gdrive_client(fake_client)
        env_file.write_text("TEST_ONLY=true\n", encoding="utf-8")
        env_file.chmod(0o600)
        compose_lines = [
            "services:",
            "  holder:",
            f"    image: {ARCHIVE_IMAGE}",
            '    command: ["sh", "-c", "while true; do sleep 3600; done"]',
            "    volumes:",
        ]
        compose_lines.extend(f"      - {name}:/volumes/{name}" for name in LOGICAL_VOLUMES)
        compose_lines.append("volumes:")
        compose_lines.extend(f"  {name}:" for name in LOGICAL_VOLUMES)
        compose_file.write_text("\n".join(compose_lines) + "\n", encoding="utf-8")
        compose = (
            "docker",
            "compose",
            "-p",
            project,
            "--env-file",
            str(env_file),
            "-f",
            str(compose_file),
        )
        test_env = os.environ.copy()
        test_env.update(
            {
                "FAKE_GDRIVE_ROOT": str(root / "remote"),
                "GDRIVE_CLIENT_BIN": str(fake_client),
                "COMPOSE_FILE": str(compose_file),
                "COMPOSE_ENV": str(env_file),
                "COMPOSE_PROJECT": project,
                "BACKUP_ARCHIVE_IMAGE": ARCHIVE_IMAGE,
                "STAMP": "integration-test",
                "BACKUP_ROOT": str(root / "backups"),
                "RESTORE_WORK_ROOT": str(root / "restore-work"),
            }
        )
        try:
            run(*compose, "up", "-d")
            config = json.loads(run(*compose, "config", "--format", "json", capture=True))
            volume_names = {name: config["volumes"][name]["name"] for name in LOGICAL_VOLUMES}
            for logical_name, volume_name in volume_names.items():
                run(
                    "docker",
                    "run",
                    "--rm",
                    "--network",
                    "none",
                    "--volume",
                    f"{volume_name}:/volume",
                    ARCHIVE_IMAGE,
                    "sh",
                    "-c",
                    f"printf '%s' original-{logical_name} > /volume/value",
                )

            backup_env = test_env | {
                "BACKUP_DATA_CLASSIFICATION": "approved-for-encrypted-cloud",
                "INCLUDE_OLLAMA": "false",
                "INCLUDE_ANALYZER_CACHE": "false",
            }
            failed_backup = subprocess.run(
                (str(PROJECT_ROOT / "scripts/manual-backup-to-gdrive.sh"),),
                cwd=PROJECT_ROOT,
                env=backup_env | {"STAMP": "integration-failure", "FAKE_GDRIVE_FAIL_UPLOAD": "true"},
                check=False,
            )
            if failed_backup.returncode == 0:
                raise RuntimeError("injected upload failure unexpectedly succeeded")
            running = run(*compose, "ps", "--services", "--status", "running", capture=True)
            if running != "holder":
                raise RuntimeError("backup failure did not restart the original service")

            run(str(PROJECT_ROOT / "scripts/manual-backup-to-gdrive.sh"), env=backup_env)
            running = run(*compose, "ps", "--services", "--status", "running", capture=True)
            if running != "holder":
                raise RuntimeError("backup did not restart the original service")

            for logical_name, volume_name in volume_names.items():
                run(
                    "docker",
                    "run",
                    "--rm",
                    "--network",
                    "none",
                    "--volume",
                    f"{volume_name}:/volume",
                    ARCHIVE_IMAGE,
                    "sh",
                    "-c",
                    f"printf '%s' changed-{logical_name} > /volume/value",
                )

            restore_env = test_env | {
                "CONFIRM_RESTORE": "integration-test",
                "ALLOW_VERSION_MISMATCH": "false",
            }
            remote_archive = root / "remote/integration-test/postgres-data.tar.gz"
            with remote_archive.open("ab") as output:
                output.write(b"tampered")
            failed_restore = subprocess.run(
                (str(PROJECT_ROOT / "scripts/manual-restore-from-gdrive.sh"),),
                cwd=PROJECT_ROOT,
                env=restore_env,
                check=False,
            )
            if failed_restore.returncode == 0:
                raise RuntimeError("tampered restore unexpectedly succeeded")
            running = run(*compose, "ps", "--services", "--status", "running", capture=True)
            if running != "holder":
                raise RuntimeError("pre-replacement restore failure stopped the service")
            shutil.copy2(
                root / "backups/cold/integration-test/postgres-data.tar.gz",
                remote_archive,
            )
            run(str(PROJECT_ROOT / "scripts/manual-restore-from-gdrive.sh"), env=restore_env)
            for logical_name, volume_name in volume_names.items():
                value = run(
                    "docker",
                    "run",
                    "--rm",
                    "--network",
                    "none",
                    "--volume",
                    f"{volume_name}:/volume:ro",
                    ARCHIVE_IMAGE,
                    "cat",
                    "/volume/value",
                    capture=True,
                )
                if value != f"original-{logical_name}":
                    raise RuntimeError(f"restore mismatch for {logical_name}: {value}")
            staging = run(
                "docker",
                "volume",
                "ls",
                "--quiet",
                "--filter",
                f"name={project}_restore-",
                capture=True,
            )
            if staging:
                raise RuntimeError(f"restore left staging volumes: {staging}")
        finally:
            subprocess.run((*compose, "down", "--volumes", "--remove-orphans"), check=False)
    print("manual backup/restore integration test passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
