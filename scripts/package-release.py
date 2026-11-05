#!/usr/bin/env python3
"""Generate plugin versions and pinned package metadata from release artifacts."""
import argparse
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parent.parent


def generate(version, artifacts, output):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("Expected X.Y.Z release version")
    release = {"version": version, "artifacts": {}}
    replacements = {"@VERSION@": version}
    for platform, suffix, placeholder in [
        ("macos_universal", "zip", "@MAC_SHA256@"),
        ("linux_amd64", "tar.gz", "@LINUX_AMD64_SHA256@"),
        ("linux_arm64", "tar.gz", "@LINUX_ARM64_SHA256@"),
    ]:
        name = f"passlet_{version}_{platform}.{suffix}"
        with (artifacts / name).open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        replacements[placeholder] = digest
        release["artifacts"][platform] = {
            "url": f"https://github.com/ParetoSecurity/passlet/releases/download/v{version}/{name}",
            "sha256": digest,
        }
    files = {"packaging/release.json": json.dumps(release, indent=2) + "\n"}
    for template, destination in [("passlet.rb", "Casks/passlet.rb"), ("passlet-agent.rb", "Formula/passlet-agent.rb")]:
        text = (ROOT / "packaging/homebrew" / (template + ".in")).read_text()
        for key, value in replacements.items():
            text = text.replace(key, value)
        files[destination] = text
    for name in ["plugin.json", ".claude-plugin/plugin.json"]:
        plugin = json.loads((ROOT / name).read_text())
        plugin["version"] = version
        files[name] = json.dumps(plugin, indent=2) + "\n"
    for name, content in files.items():
        path = output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("artifacts", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    generate(args.version, args.artifacts, args.output)
