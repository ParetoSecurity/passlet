---
name: passlet
description: Obtain scoped GitHub access through Passlet before using git or gh against private repositories, reading PRs or issues, or pushing changes. Request only the repositories and permissions needed, reuse approved grants, and handle an unavailable Mac without falling back to ambient credentials.
---

# GitHub access through Passlet

Run Claude normally. Request GitHub access yourself when the task needs it. `passlet` must be on PATH. Local Mac agents use the default private Unix socket when `PASSLET_ENDPOINT` is unset. Remote agents use their own SSH-forwarded socket (for example `unix:///home/alice/.local/share/passlet/mac.sock`) or an explicitly configured Tailscale HTTPS origin. Never guess a teammate’s endpoint. Use `passlet doctor` when troubleshooting is requested. It prompts for native approval on the Conductor’s Mac, unlocks credentials, refreshes authorization when needed, and verifies GitHub identity without issuing an agent token. Tell the user to expect the prompt, and do not run it as an unattended preflight or retry loop. Socket access does not authenticate Claude; other processes in the same account and remote root are trusted. The user authenticates on the Mac, and only a scoped GitHub user token reaches this agent. Actions retain the user's GitHub attribution.

Use Passlet for authenticated GitHub operations. Do not use inherited PAT variables, cached SSH-session tokens, `gh auth login`, SSH Git remotes, or other credentials as a fallback. Local Git operations need no grant. Existing task authorization still determines which changes you may make; receiving a token does not authorize unrelated writes.

## Request and reuse a grant

Choose explicit `owner/repository` names and the minimum permissions needed:

| Operation | Permissions |
| --- | --- |
| Clone, fetch, read files, inspect remote refs | `contents:read` |
| Read PRs and their diffs | `contents:read,pull_requests:read` |
| Push a branch | `contents:write` |
| Push a branch and create/update its PR | `contents:write,pull_requests:write` |
| Read or update issues | `issues:read` or `issues:write` |
| Inspect Actions runs | `actions:read` |

For administrative work, use `administration:write` with `--repos` for a repository. Organization permissions use `--org NAME` (for example `members:write`); account permissions use `--account HANDLE` (for example `git_ssh_keys:write`). These scope selectors are mutually exclusive. Organization/account grants are explicitly wider than a repository grant and include metadata read access to all installed repositories for that target. Never substitute an organization or account grant for a repository grant without explaining and obtaining approval for the wider scope. Use `--out` and reuse grants as below for every scope.

Request additional permissions only when the task needs their specific operations. Passlet includes intrinsic `metadata:read`. Group explicit repositories belonging to the same owner into one request; different owners need separate grants. Requests accept up to 100 repositories, subject to transport size limits. Long approval lists end with "and X additional repositories"; the grant still contains the complete explicit list. Do not substitute organization-wide access for a list of repositories. Passlet accepts no caller description or requested TTL.

Tell the user which repositories and permissions you are requesting, then make one request. Allow the command at least 130 seconds so the user has time to approve. Never put this request in a retry loop.

```sh
passlet_dir=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/passlet.XXXXXX")
chmod 700 "$passlet_dir"
passlet request \
  --repos acme/project \
  --permissions contents:read \
  --out "$passlet_dir/grant.json"
printf 'Grant path: %s\n' "$passlet_dir/grant.json"
```

Replace the example repository and permissions with the task's actual scope. Check that the request succeeded before using the file. `--out` creates a new mode-0600 file and refuses overwrites. Never omit `--out`: otherwise the token JSON goes to stdout and into your transcript.

Keep the concrete grant path, approved scope, and expiry in your session context and handoff summaries so later prompts can reuse it. Check for a suitable retained grant before requesting another. Shell variables may not survive between tool calls, so use that concrete path in later commands. Never read the grant file into your context, print it, source it, extract its token, or put the token in an argument or Git URL. The request receipt already reports the actual expiry without revealing the token.

Run each authenticated command through the grant:

```sh
passlet exec --grant /concrete/private/path/grant.json -- \
  gh api repos/acme/project/contents

passlet exec --grant /concrete/private/path/grant.json -- \
  git ls-remote https://github.com/acme/project.git HEAD
```

Reuse the same grant for operations within its approved scope until expiry. `exec --grant` does not contact the Mac. It supplies the scoped credentials to Git and gh and disables global/system Git configuration and ambient credential helpers for that child. Use HTTPS remotes. If needed, configure commit author identity locally in the checkout.

Retain the grant file and its private directory after each command, task, and prompt for later reuse. Do not actively delete grants, including expired ones, unless the user asks. Keep the directory mode 0700 and grant mode 0600. Retention does not extend a grant's expiry or scope: use it only while valid and for authorized operations within its approved scope. Deleting a grant file does not revoke its token.

## Failures and new access

- Mac unavailable, connection lost, or approval timeout: stop requesting. Continue useful local work that needs no token. Tell the user to bring the Mac online and confirm when ready; only then make a new explicit request. There is no queue or automatic retry.
- Approval denied or cancelled: do not immediately prompt again or try another credential. Explain which work needs access and wait for the user to direct the next step.
- `approver_busy`: do other work; do not poll or repeatedly request approval.
- Expired grant: explain that a new approval is needed and make a fresh explicit request for the still-needed scope. Never reuse or expose the expired token, and never implement a background renewal loop.
- GitHub denies an operation: do not assume it needs write access. Check the requested operation, granted scope recorded in your session, and repository installation. Explain any specifically missing permission before requesting a different grant. Do not broaden access automatically to make an error disappear.
- Repository not installed: ask the user to add that repository to the GitHub App installation. Do not request access to a different repository as a workaround.

Repository-listing endpoints can expose metadata beyond the scoped repositories. A listing is not proof of content access. Validate access through the intended operation rather than probing unrelated repositories.
