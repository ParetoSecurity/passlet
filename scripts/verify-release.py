#!/usr/bin/env python3
"""Verify a final release tag against its signed preparation artifacts."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("package_release", ROOT / "scripts/package-release.py")
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)

METADATA = {
    "Casks/passlet.rb",
    "Formula/passlet-agent.rb",
    "packaging/release.json",
    "plugin.json",
    ".claude-plugin/plugin.json",
}


def verify(version, artifacts, run, root=ROOT):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("Expected X.Y.Z release version")
    release = json.loads((root / "packaging/release.json").read_text())
    preparation = release.get("preparation", {})
    source = preparation.get("source_sha", "")
    run_id = preparation.get("run_id")
    if not isinstance(run_id, int) or run_id <= 0 or not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("Release metadata must identify its preparation run and source SHA")
    if not (
        run.get("id") == run_id
        and run.get("head_sha") == source
        and run.get("head_branch") == f"v{version}-prepare"
        and run.get("event") == "push"
        and run.get("path") == ".github/workflows/release.yml"
        and run.get("repository", {}).get("full_name") == "ParetoSecurity/passlet"
        and run.get("status") == "completed"
        and run.get("conclusion") == "success"
    ):
        raise ValueError("Artifacts must come from the successful preparation workflow for this release")
    subprocess.run(["git", "merge-base", "--is-ancestor", source, "HEAD"], cwd=root, check=True)
    changed = subprocess.check_output(["git", "diff", "--name-only", source, "HEAD"], cwd=root, text=True)
    if set(changed.splitlines()) - METADATA:
        raise ValueError("Source changed after preparation; prepare a new release before publishing")
    for name in ["plugin.json", ".claude-plugin/plugin.json"]:
        original = json.loads(subprocess.check_output(["git", "show", f"{source}:{name}"], cwd=root))
        original["version"] = version
        if json.loads((root / name).read_text()) != original:
            raise ValueError(f"Tagged metadata changes the prepared plugin beyond its version: {name}")

    names = [f"passlet_{version}_{platform}.{suffix}" for platform, suffix in [
        ("macos_universal", "zip"), ("linux_amd64", "tar.gz"), ("linux_arm64", "tar.gz"),
    ]]
    sums = f"passlet_{version}_SHA256SUMS"
    if {p.name for p in artifacts.iterdir()} != set(names + [sums]):
        raise ValueError("Prepared assets must contain exactly the three archives and checksum file")
    expected = ""
    for name in names:
        with (artifacts / name).open("rb") as stream:
            expected += f"{hashlib.file_digest(stream, 'sha256').hexdigest()}  {name}\n"
    if (artifacts / sums).read_text() != expected:
        raise ValueError("Prepared archive checksums do not match")
    with tempfile.TemporaryDirectory() as directory:
        generated = Path(directory)
        package.generate(version, artifacts, generated, run_id, source, source=root)
        for name in sorted(METADATA):
            if (root / name).read_bytes() != (generated / name).read_bytes():
                raise ValueError(f"Tagged metadata does not match prepared artifacts: {name}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("artifacts", type=Path)
    parser.add_argument("run", type=Path)
    args = parser.parse_args()
    verify(args.version, args.artifacts, json.loads(args.run.read_text()))
