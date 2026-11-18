#!/usr/bin/env python3
"""Reuse preparation tests only after verifying the generated release metadata."""
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("verify_release", ROOT / "scripts/verify-release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def candidate(root=ROOT):
    manifest = json.loads((root / "packaging/release.json").read_text())
    version = manifest.get("version", "")
    preparation = manifest.get("preparation", {})
    source = preparation.get("source_sha", "")
    run_id = preparation.get("run_id")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version) or not re.fullmatch(r"[0-9a-f]{40}", source) or type(run_id) is not int or run_id <= 0:
        return None
    try:
        release.verify_source(version, source, root)
    except (ValueError, subprocess.CalledProcessError):
        return None
    return version, run_id


if __name__ == "__main__":
    metadata = candidate()
    if metadata is None:
        print("metadata=false")
    else:
        version, run_id = metadata
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory) / "assets"
            run = json.loads(subprocess.check_output(["gh", "api", f"repos/ParetoSecurity/passlet/actions/runs/{run_id}"]))
            subprocess.run(["gh", "run", "download", str(run_id), "--repo", "ParetoSecurity/passlet", "--name", "passlet-release-assets", "--dir", str(artifacts)], check=True)
            release.verify(version, artifacts, run)
        print("metadata=true")
