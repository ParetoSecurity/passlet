#!/usr/bin/env python3
"""Add verified preparation metadata to its unchanged release PR."""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
REPOSITORY = "ParetoSecurity/passlet"
spec = importlib.util.spec_from_file_location("verify_release", ROOT / "scripts/verify-release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def select_pr(run, pulls):
    match = re.fullmatch(r"v([0-9]+\.[0-9]+\.[0-9]+)-prepare", run.get("head_branch", ""))
    if not (match and run.get("event") == "push" and run.get("path") == ".github/workflows/release.yml"
            and run.get("repository", {}).get("full_name") == REPOSITORY
            and run.get("status") == "completed" and run.get("conclusion") == "success"
            and re.fullmatch(r"[0-9a-f]{40}", run.get("head_sha", ""))):
        raise ValueError("Expected a successful release preparation in this repository")
    matches = [pr for pr in pulls if pr.get("state") == "open"
               and pr.get("base", {}).get("ref") == "main"
               and pr.get("base", {}).get("repo", {}).get("full_name") == REPOSITORY
               and (pr.get("head", {}).get("repo") or {}).get("full_name") == REPOSITORY
               and pr.get("head", {}).get("ref") != "main"
               and pr.get("head", {}).get("sha") == run["head_sha"]]
    if len(matches) != 1:
        raise ValueError("Expected one open release PR still at the preparation SHA")
    return match[1], matches[0]["head"]["ref"]


def update(run_id):
    def api(endpoint):
        return json.loads(subprocess.check_output(["gh", "api", f"repos/{REPOSITORY}/{endpoint}"]))

    run = api(f"actions/runs/{run_id}")
    source = run["head_sha"]
    version, branch = select_pr(run, api(f"commits/{source}/pulls"))
    subprocess.run(["git", "check-ref-format", "--branch", branch], check=True)
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        checkout, assets = root / "checkout", root / "assets"
        subprocess.run(["git", "worktree", "add", "--detach", str(checkout), source], check=True)
        try:
            subprocess.run(["gh", "run", "download", str(run_id), "--repo", REPOSITORY, "--name", "passlet-release-assets", "--dir", str(assets)], check=True)
            release.package.generate(version, assets, checkout, run_id, source, source=checkout)
            subprocess.run(["git", "-C", str(checkout), "add", "--", *sorted(release.METADATA), "docs"], check=True)
            subprocess.run(["git", "-C", str(checkout), "-c", "user.name=github-actions[bot]", "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com", "-c", "commit.gpgsign=false", "commit", "-m", f"chore: Prepare v{version} packages", "-m", "Pin packages and plugins to the signed release artifacts."], check=True)
            release.verify(version, assets, run, checkout)
            # A normal push rejects concurrent changes instead of overwriting them.
            subprocess.run(["gh", "auth", "setup-git"], check=True)
            subprocess.run(["git", "-C", str(checkout), "push", f"https://github.com/{REPOSITORY}.git", f"HEAD:refs/heads/{branch}"], check=True)
            subprocess.run(["gh", "workflow", "run", "ci.yml", "--repo", REPOSITORY, "--ref", branch], check=True)
        finally:
            subprocess.run(["git", "worktree", "remove", "--force", str(checkout)], check=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_id", type=int)
    update(parser.parse_args().run_id)
