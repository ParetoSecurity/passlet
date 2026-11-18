# FAQ

## I have `gh` configured to save my token in macOS Keychain, why should I care about Passlet?

On macOS, `gh` retrieves your token by calling `/usr/bin/security`. When you select “Always Allow,” Keychain remembers permission for `/usr/bin/security`, not exclusively for `gh`. Any malicious process running as your user can call that same program to retrieve the token, with its full configured permissions.

Passlet requires Touch ID or your Mac password before accessing your long-lived credentials. The agent receives a short-lived token limited to the repositories and permissions you approve.

Another process under the agent’s Unix account can still read that delivered token, but its access is limited by the approved scope and expiry.

## Can sessions reuse an approved grant?

Yes. `passlet request` automatically reuses matching grants with at least one minute remaining from a private file cache on the Agent machine. Sessions under the same OS user share the cache. Agent key, Approver key, endpoint, scope, permissions, and expiry determine reuse. Separate paired Agents on different machines do not share their caches. Repository requests can reuse an approved organization or account grant for the same owner when its permissions cover the request. Matching repository grants are preferred. Cache hits work while the Approver is offline, without extending the original grant's expiry or permissions.

The cache directory is `~/.local/share/passlet/grants`, owned by your user with mode 0700. Cache and lock files have mode 0600. Passlet rejects unsafe ownership, permissions, symlinks, and hard links, and uses atomic writes and locking for concurrent sessions. Grants survive process exits and reboots, but retain their original expiry. Each paired identity and endpoint keeps at most 128 grants. Expired grants are ignored and removed on cache updates.

## Can I avoid retaining grants?

Use `--no-cache --stdout` with Bash or Zsh process substitution:

```sh
passlet exec \
  --grant <(passlet request --no-cache --stdout --repos acme/project --permissions contents:read) \
  -- gh repo view acme/project
```

This bypasses cache lookup and storage, requiring fresh approval even if a matching grant is already cached. It never reads, writes, or creates the cache. `exec` does not cache the piped grant either.

## How do `--out`, `--stdout`, and `--no-cache` interact?

| Options | Reuses cached grant | Saves grant to cache | Writes custom file | Grant JSON on stdout |
| --- | --- | --- | --- | --- |
| None | Yes, if valid and sufficient | Yes, on new approval | No | No |
| `--stdout` | Yes, if valid and sufficient | Yes, on new approval | No | Yes |
| `--out FILE` | Yes, if valid and sufficient | Yes, on new approval | Yes | No |
| `--no-cache` | Rejected: requires `--stdout` or `--out` | — | — | — |
| `--no-cache --stdout` | No, fresh approval required | No | No | Yes |
| `--no-cache --out FILE` | No, fresh approval required | No | Yes | No |

`--stdout` and `--out` are mutually exclusive. `--out` creates a mode-0600 file and refuses to overwrite an existing file. `--no-cache` leaves existing cached grants untouched. Every successful request prints a receipt on stderr. Without `--no-cache`, a cache miss requires fresh approval.

Use `passlet exec --repos acme/project --permissions contents:read -- COMMAND` to reuse cached access or request approval when no matching grant exists. Denied approval or an unavailable Approver prevents the command from starting. `exec --grant` uses the supplied grant without requesting approval.

## What about scheduled tasks?

A scheduled task your agent runs every day or every week cannot use Passlet grants because they have a short validity.

For these tasks, create a dedicated [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new). Choose the resource owner that owns the repository, select only the repositories the task needs, and grant only the permissions required for that task. Set an expiration and configure the token through your scheduler’s secret storage.

The task can run without you, and if its token leaks, the damage is limited to the scope you defined for it. Configure this explicitly for the scheduled task, rather than letting an agent fall back to another credential when Passlet approval is unavailable.
