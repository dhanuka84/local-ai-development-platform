#!/usr/bin/env python3
"""Check local documentation links, reachability and Mermaid export freshness."""
from __future__ import annotations

import hashlib
import html
import json
from pathlib import Path
import re
import struct
import subprocess
import sys
from urllib.parse import unquote, urlsplit
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
LINK = re.compile(r'!?\[(?:[^\[\]]|\[[^\]]*\])*\]\(\s*(<[^>]+>|[^\s)]+)(?:\s+["\'][^\n]*?["\'])?\s*\)')
REFERENCE = re.compile(r'^ {0,3}\[[^\]]+\]:\s*(<[^>]+>|\S+)', re.MULTILINE)


def outside_fences(text: str) -> str:
    lines = []
    fence = ""
    for line in text.splitlines():
        match = re.match(r"^ {0,3}(`{3,}|~{3,})", line)
        if match:
            marker = match.group(1)
            if not fence:
                fence = marker
            elif marker[0] == fence[0] and len(marker) >= len(fence):
                fence = ""
            lines.append("")
        else:
            lines.append("" if fence else line)
    return "\n".join(lines)


def anchors(text: str) -> set[str]:
    result = set(re.findall(r'<a\s+(?:name|id)=["\']([^"\']+)["\']', text))
    counts: dict[str, int] = {}
    for heading in re.findall(r"^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$", outside_fences(text), re.MULTILINE):
        heading = re.sub(r"!?\[([^\]]+)\]\([^)]*\)", r"\1", heading)
        heading = html.unescape(re.sub(r"<[^>]+>", "", heading)).lower()
        slug = "".join(char for char in heading if char.isalnum() or char in "_- ")
        slug = slug.replace(" ", "-")
        number = counts.get(slug, 0)
        counts[slug] = number + 1
        result.add(slug if not number else f"{slug}-{number}")
    return result


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    files = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=ROOT).decode().split("\0")
    documents = sorted({ROOT / name for name in files if name.endswith(".md") and (ROOT / name).is_file()})
    contents = {path: path.read_text() for path in documents}
    graph: dict[Path, set[Path]] = {path: set() for path in documents}
    errors = []
    checked = 0
    for path, text in contents.items():
        visible = outside_fences(text)
        targets = [match.group(1).strip("<>") for match in LINK.finditer(visible)]
        targets += [match.group(1).strip("<>") for match in REFERENCE.finditer(visible)]
        for target in targets:
            parsed = urlsplit(target)
            if parsed.scheme or parsed.netloc:
                continue
            checked += 1
            destination = (path.parent / unquote(parsed.path)).resolve() if parsed.path else path
            label = str(path.relative_to(ROOT))
            if not destination.is_relative_to(ROOT):
                errors.append(f"{label}: link leaves repository: {target}")
            elif not destination.exists():
                errors.append(f"{label}: missing local target: {target}")
            elif destination.suffix == ".md":
                if destination in graph:
                    graph[path].add(destination)
                if parsed.fragment and unquote(parsed.fragment) not in anchors(destination.read_text()):
                    errors.append(f"{label}: missing heading: {target}")
    reachable = set()
    pending = [ROOT / "README.md"]
    while pending:
        path = pending.pop()
        if path not in reachable:
            reachable.add(path)
            pending.extend(graph.get(path, ()))
    for path in documents:
        if path not in reachable:
            errors.append(f"Documentation is not linked from README: {path.relative_to(ROOT)}")

    directory = ROOT / "docs/diagrams"
    sources = sorted(directory.glob("*.mmd"))
    try:
        manifest = json.loads((directory / "manifest.json").read_text())
        if manifest.get("schema") != "hybrid-ai/diagram-manifest/v1":
            errors.append("Unexpected diagram manifest schema")
        records = manifest["diagrams"]
        if set(records) != {source.stem for source in sources}:
            errors.append("Diagram manifest does not match Mermaid source inventory")
        for source in sources:
            entry = records.get(source.stem, {})
            paths = {
                "source_sha256": source,
                "svg_sha256": source.with_suffix(".svg"),
                "png_sha256": source.with_suffix(".png"),
                "renderer_sha256": ROOT / "scripts/render_diagrams.py",
                "config_sha256": directory / "puppeteer-config.json",
            }
            for key, path in paths.items():
                if not path.exists() or entry.get(key) != sha(path):
                    errors.append(f"{source.name}: stale or missing {key}; run make diagrams")
            svg = source.with_suffix(".svg")
            if svg.exists():
                root = ET.fromstring(svg.read_text())
                if root.tag != "{http://www.w3.org/2000/svg}svg":
                    errors.append(f"{svg.name}: invalid SVG root")
            png = source.with_suffix(".png")
            if png.exists():
                data = png.read_bytes()
                if data[:8] != b"\x89PNG\r\n\x1a\n":
                    errors.append(f"{png.name}: invalid PNG signature")
                width, height = struct.unpack(">II", data[16:24])
                if (width, height) != (entry.get("png_width"), entry.get("png_height")):
                    errors.append(f"{png.name}: dimensions do not match manifest")
    except (OSError, ValueError, KeyError, ET.ParseError, struct.error) as error:
        errors.append(f"Diagram validation failed: {error}")

    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f"Documentation check failed: {len(errors)} issue(s)", file=sys.stderr)
        return 1
    print(f"Documentation checks passed: {len(documents)} Markdown files, "
          f"{checked} local links, {len(sources)} Mermaid sources and SVG/PNG export pairs.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
