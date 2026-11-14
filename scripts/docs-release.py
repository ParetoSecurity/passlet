#!/usr/bin/env python3
"""Generate release labels only outside Markdown code examples."""
import argparse
from pathlib import Path
import re

MARKER = re.compile(r'(<AddedIn\s+version=)([\x22\x27])unreleased\2(\s*/>)')


def released_text(text, version):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("Expected X.Y.Z release version")
    lines = []
    fence = None
    for line in text.splitlines(keepends=True):
        match = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if match:
            value = match[1]
            if fence is None:
                fence = value
            elif value[0] == fence[0] and len(value) >= len(fence):
                fence = None
            lines.append(line)
        elif fence is None and re.match(r"^\s{0,3}<AddedIn\s+version=", line):
            lines.append(MARKER.sub(lambda m: f"{m[1]}{m[2]}{version}{m[2]}{m[3]}", line))
        else:
            lines.append(line)
    return "".join(lines)


def updates(source, version):
    result = {}
    for path in sorted((source / "docs").rglob("*.mdx")):
        original = path.read_text()
        updated = released_text(original, version)
        if original != updated:
            result[path.relative_to(source).as_posix()] = updated
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    for name, text in updates(Path(__file__).resolve().parent.parent, args.version).items():
        path = args.output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
