"""Check two distinct maintainers, counting a maintainer PR author.

Run only from a trusted base revision. Never execute code from the PR being
reviewed. Review IDs and commit IDs come from GitHub, not PR text or labels.
"""

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request

MAINTAINERS = frozenset({"zupo", "dz0ny", "am-on"})


def quorum(author, head, reviews):
    latest = {}
    for review in sorted(reviews, key=lambda value: value["id"]):
        login = review["user"]["login"]
        if login not in MAINTAINERS or login == author:
            continue
        # A later comment does not supersede a review decision. A dismissal,
        # change request, or approval on an old commit does.
        if review["state"] != "COMMENTED":
            latest[login] = review
    people = {author} & MAINTAINERS
    people |= {
        login for login, review in latest.items()
        if review["state"] == "APPROVED" and review.get("commit_id") == head
    }
    return sorted(people)


def api(path, data=None):
    request = urllib.request.Request(
        "https://api.github.com/" + path,
        data=json.dumps(data).encode() if data is not None else None,
        headers={
            "Authorization": "Bearer " + os.environ["GH_TOKEN"],
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2026-03-10",
            "Content-Type": "application/json",
        },
    )
    # Never print HTTP response bodies, request headers, or credentials.
    with urllib.request.urlopen(request, timeout=20) as response:
        return json.load(response)


def reviews_for(repo, number):
    reviews = []
    for page in range(1, 101):
        batch = api(f"repos/{repo}/pulls/{number}/reviews?per_page=100&page={page}")
        reviews.extend(batch)
        if len(batch) < 100:
            return reviews
    raise ValueError("Review pagination limit reached")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    selector = parser.add_mutually_exclusive_group(required=True)
    selector.add_argument("--pr", type=int)
    selector.add_argument("--release-sha")
    parser.add_argument("--publish-status", action="store_true")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repo):
        raise ValueError("Invalid repository")
    number = args.pr
    if args.release_sha:
        if not re.fullmatch(r"[0-9a-f]{40}", args.release_sha):
            raise ValueError("Invalid release commit")
        candidates = api(f"repos/{args.repo}/commits/{args.release_sha}/pulls?per_page=100")
        eligible = [p for p in candidates if p.get("merged_at") and p["base"]["ref"] == "main" and p.get("merge_commit_sha") == args.release_sha]
        if len(eligible) != 1:
            raise ValueError("Release commit must be the merge commit of one reviewed PR into main")
        number = eligible[0]["number"]
    if number is None or number <= 0:
        raise ValueError("Invalid PR number")
    pr = api(f"repos/{args.repo}/pulls/{number}")
    if pr["base"]["ref"] != "main":
        raise ValueError("Quorum policy targets main")
    head = pr["head"]["sha"]
    if args.publish_status:
        api(f"repos/{args.repo}/statuses/{head}", {
            "state": "pending", "context": "Maintainer quorum",
            "description": "Rechecking current maintainer reviews",
        })
    people = quorum(pr["user"]["login"], head, reviews_for(args.repo, number))
    passed = len(people) >= 2
    if args.publish_status:
        api(f"repos/{args.repo}/statuses/{head}", {
            "state": "success" if passed else "failure",
            "context": "Maintainer quorum",
            "description": f"{len(people)} of 2 required maintainers agree on this revision",
        })
    print(f"PR #{number}: {len(people)} distinct maintainers; quorum {'met' if passed else 'not met'}")
    return 0 if passed else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (urllib.error.URLError, ValueError, KeyError, TypeError):
        print("Unable to verify maintainer quorum; refusing approval.", file=sys.stderr)
        sys.exit(1)
