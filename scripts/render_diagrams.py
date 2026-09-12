#!/usr/bin/env python3
"""Render repository-native Mermaid diagrams and record source/export digests."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import struct
import subprocess

ROOT = Path(__file__).resolve().parents[1]
DIRECTORY = ROOT / "docs/diagrams"
MANIFEST = DIRECTORY / "manifest.json"
MERMAID = "@mermaid-js/mermaid-cli@11.16.0"


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--name", help="Render one diagram basename; default: all .mmd files")
    args = parser.parse_args()
    sources = sorted(DIRECTORY.glob("*.mmd"))
    if args.name:
        sources = [path for path in sources if path.stem == args.name]
        if not sources:
            parser.error("Unknown diagram name")
    manifest = json.loads(MANIFEST.read_text()) if MANIFEST.exists() else {}
    records = manifest.get("diagrams", {})
    config = DIRECTORY / "puppeteer-config.json"
    for source in sources:
        print(f"Rendering {source.name}", flush=True)
        common = ["npx", "-y", MERMAID, "-p", str(config), "-i", str(source), "-b", "#ffffff"]
        for suffix in (".svg", ".png"):
            command = common + ["-o", str(source.with_suffix(suffix))]
            if suffix == ".png":
                command += ["-w", "2400", "-s", "2"]
            subprocess.run(command, cwd=ROOT, check=True)
        width, height = struct.unpack(">II", source.with_suffix(".png").read_bytes()[16:24])
        records[source.stem] = {
            "source_sha256": digest(source),
            "svg_sha256": digest(source.with_suffix(".svg")),
            "png_sha256": digest(source.with_suffix(".png")),
            "png_width": width,
            "png_height": height,
            "renderer": MERMAID,
            "renderer_sha256": digest(Path(__file__)),
            "config_sha256": digest(config),
        }
    active = {path.stem for path in DIRECTORY.glob("*.mmd")}
    result = {"schema": "hybrid-ai/diagram-manifest/v1",
              "diagrams": {key: records[key] for key in sorted(records) if key in active}}
    temporary = MANIFEST.with_suffix(".json.tmp")
    temporary.write_text(json.dumps(result, indent=2) + "\n")
    temporary.replace(MANIFEST)


if __name__ == "__main__":
    main()
