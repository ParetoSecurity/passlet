#!/usr/bin/env python3
"""Require an open same-repository PR and passing CI before release preparation."""
import argparse
import json
from pathlib import Path

REPOSITORY = "ParetoSecurity/passlet"


def verify(sha, pulls, runs):
    if not any(
        pr.get("state") == "open"
        and pr.get("base", {}).get("ref") == "main"
        and pr.get("base", {}).get("repo", {}).get("full_name") == REPOSITORY
        and (pr.get("head", {}).get("repo") or {}).get("full_name") == REPOSITORY
        and pr.get("head", {}).get("sha") == sha
        for pr in pulls
    ):
        raise ValueError("Preparation must target the current head of an open PR in this repository against main")
    workflow_runs = runs.get("workflow_runs", [])
    latest = workflow_runs[0] if workflow_runs else {}
    if not (
        latest.get("head_sha") == sha
        and latest.get("path") == ".github/workflows/ci.yml"
        and latest.get("event") == "pull_request"
        and latest.get("status") == "completed"
        and latest.get("conclusion") == "success"
    ):
        raise ValueError("The latest PR CI run must have passed for the exact preparation commit")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sha")
    parser.add_argument("pulls", type=Path)
    parser.add_argument("runs", type=Path)
    args = parser.parse_args()
    verify(args.sha, json.loads(args.pulls.read_text()), json.loads(args.runs.read_text()))
