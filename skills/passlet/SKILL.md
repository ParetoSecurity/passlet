---
name: passlet
description: Use Passlet for authenticated GitHub operations through Git, gh, or the GitHub API. Reuse approved access, request only the scope needed, and recover from transient failures without interrupting the task.
---

# GitHub access through Passlet

Public repository reads do not need Passlet. Use unauthenticated HTTPS access for those reads, without ambient tokens or credential helpers. Use Passlet for private repository reads, writes, or any operation that requires authentication. Let its approval prompt handle consent, without a separate explanation or confirmation in chat. Local Git operations need no grant. A grant authorizes access, not unrelated changes.

## Run the command

```sh
passlet exec --repos acme/project --permissions contents:read \
  -- gh api repos/acme/project/contents
```

Use the task's actual repositories and minimum permissions. Allow at least 130 seconds for approval. `exec` reuses matching cached access or requests approval when needed. No separate `request`, cache inspection, or diagnostic preflight is needed.

| Operation | Permissions |
| --- | --- |
| Clone, fetch, read files, inspect remote refs | `contents:read` |
| Read PRs and their diffs | `contents:read,pull_requests:read` |
| Push a branch | `contents:write` |
| Push a branch and create/update its PR | `contents:write,pull_requests:write` |
| Read or update issues | `issues:read` or `issues:write` |
| Inspect Actions runs | `actions:read` |
| Read CI check results | `checks:read` |
| Administer a repository | `administration:write` |

Group repositories belonging to the same owner into one request, up to 100. Different owners need separate grants. Metadata read is included automatically. Use `--org NAME` for organization operations such as `members:write`, or `--account HANDLE` for account operations such as `git_ssh_keys:write`.

Use HTTPS Git remotes. Passlet supplies `GH_TOKEN`, `GITHUB_TOKEN`, and Git authentication to the child. Never fall back to ambient tokens, `gh auth login`, or SSH credentials. Keep tokens out of logs, arguments, URLs, and tool output. Let Passlet manage its cache, and leave existing grants alone unless the user asks to remove them.

## Recover quietly

Handle recoverable failures without narrating them or asking the user to retry. Limit automatic recovery to one retry per failed operation, then continue independent work and report only what remains blocked.

- Connection failure, `approver_busy`, or approval timeout: wait briefly or do independent work, then retry once with the same scope. An explicit denial or cancellation ends the attempt, do not prompt again.
- Expired or rejected credentials: retry the ordinary scoped `exec` once when safe. Passlet ignores expired grants and removes cached tokens that GitHub rejects after a command failure, so the retry can obtain and cache fresh approval. Do not explain token expiry or revocation unless asked.
- Missing permissions: use the failed operation and GitHub's response to identify the specific permission needed for the authorized task, then retry with that permission through Passlet. Do not treat every 403 or 404 as a permission problem or try progressively broader scopes.
- Before retrying a write, establish that it did not succeed or that repeating it is safe. If its outcome is uncertain, inspect the result before proceeding. Do not replay an entire script after a partial failure.
- Pairing, installation, or sandbox problems: preserve the selected endpoint and trust configuration. Do not re-pair, switch credentials, or weaken the sandbox automatically. If user action is required, state the necessary action briefly.

## When gh is unavailable

Use the GitHub REST API with `curl` inside `passlet exec`, without asking the user to install or authenticate another client:

```sh
passlet exec --repos acme/project --permissions contents:read -- sh -c '
  set +x
  printf "Authorization: Bearer %s\n" "$GH_TOKEN" |
    curl --disable --fail --silent --show-error \
      --header @- \
      --header "Accept: application/vnd.github.raw+json" \
      https://api.github.com/repos/acme/project/readme
'
```

Keep shell tracing disabled and send the authorization header only to `https://api.github.com`, without following redirects. For JSON endpoints, use `Accept: application/vnd.github+json`. Send write payloads from a file with `--data-binary @file.json`, and handle pagination for listings.

## Connection setup

Use the configured endpoint. Local agents default to the private Unix socket; remote agents use their configured SSH-forwarded socket or Tailscale HTTPS origin. Do not guess another agent's endpoint. Run `passlet doctor` only when diagnosing the connection, it requires approval. Use `passlet approver doctor` to diagnose the Mac's local service. For pairing requested by the user, follow [pairing setup](https://passlet.dev/docs/guides/setup.md#pair-agents).
