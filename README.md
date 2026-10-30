<img src="assets/passlet.png" width="96" alt="Passlet icon">

# Passlet

**Short-lived credentials for your agents.**

Your agent running in a remote server or a sandbox needs access to GitHub. Let it
ask you for scoped permissions on-the-fly instead of giving it perpetual carte
blanche access.

Approve a request on your Mac with one Touch ID/password prompt. Passlet retrieves
protected Keychain credentials, obtains a repository- and permission-scoped GitHub
App **user** token, and delivers only that token to the agent. Git and `gh` then
work independently until the token expires, even while your Mac is offline.
GitHub actions retain your user attribution.

[Website](https://passlet.dev) · [Releases](https://github.com/paretosecurity/passlet/releases) · [Discord](https://discord.gg/989fwbsyw7)

## Status and requirements

Version 0.1 supports GitHub only. GitHub App registration and user authorization
are manual; guided onboarding is planned for v0.2. This is a CLI/service and a
background macOS helper, with no menu bar UI. Opening the app alone does not
configure or start the service.

- Approving Mac: macOS 14 or later, a logged-in graphical session, and Tailscale.
- Agent: Linux or macOS, Tailscale, and Git and/or GitHub CLI (`gh`).
- A GitHub App installed on the repositories you want to make eligible, with
  expiring user tokens and device authorization enabled.
- Tailscale network policy allowing agents to reach the approving Mac on TCP 8443.

## Install

Download the matching archive from Releases and verify its SHA-256 checksum against
the release's `SHA256SUMS` file. Release assets are attached automatically after
the signing workflow finishes; see [release instructions](docs/RELEASING.md).

On macOS, extract the universal archive and move `Passlet.app` to `/Applications`.
The signed CLI and helper are inside that bundle:

```sh
mkdir -p "$HOME/.local/bin"
ln -s /Applications/Passlet.app/Contents/MacOS/passlet "$HOME/.local/bin/passlet"
export PATH="$HOME/.local/bin:$PATH"
passlet version
```

Keep the helper executable inside the app bundle. On Linux, extract the amd64 or
arm64 archive and place `passlet` on your PATH. The archive also includes the
Claude skill and this README.

## Configure GitHub and Keychain

Register a GitHub App, enable expiring user tokens and device authorization, and
install it on the repositories you want to make available. Set its repository
permissions to the maximum access you want Passlet to be able to request. For
example, pushing branches and opening pull requests requires Contents and Pull
requests write access. Metadata read access is intrinsic.

Authorize the app as the user whose identity should appear on GitHub. Obtain the
user access token and refresh token through GitHub's user authorization flow. The
app's client ID and client secret are also required. The App's private signing key
is **not** used by Passlet's user-token flow. Each person's GitHub access still
limits what their tokens can do.

Prepare this JSON in a temporary mode-0600 file. `expires_at` is the actual parent
access-token expiry, expressed as a UTC RFC3339 timestamp:

```json
{
  "client_id": "YOUR_APP_CLIENT_ID",
  "client_secret": "YOUR_APP_CLIENT_SECRET",
  "access_token": "PARENT_USER_ACCESS_TOKEN",
  "refresh_token": "PARENT_REFRESH_TOKEN",
  "expires_at": "RFC3339_UTC_TIMESTAMP"
}
```

Import it while the Mac is unlocked:

```sh
passlet import \
  --helper /Applications/Passlet.app/Contents/MacOS/PassletHelper \
  < /private/path/to/credentials.json
```

Remove the temporary credential file after a successful import. Never put secrets
in arguments, Git, transcripts, or audit records. Import refuses to overwrite an
existing credential. Keychain uses user-presence protection and
`WhenUnlockedThisDeviceOnly`. Each request starts a fresh authentication context;
the credential read reuses that context without a second prompt.

## Start the Mac service

Create a private audit directory, then run the service in your logged-in session:

```sh
mkdir -p "$HOME/.local/share/passlet"
chmod 700 "$HOME/.local/share/passlet"
passlet serve \
  --listen 127.0.0.1:8484 \
  --helper /Applications/Passlet.app/Contents/MacOS/PassletHelper \
  --audit "$HOME/.local/share/passlet/audit.sqlite"
```

In a separate terminal, expose it through Tailscale Serve:

```sh
tailscale serve --proxy-protocol=2 --tls-terminated-tcp=8443 tcp://127.0.0.1:8484
```

Use **Serve**, not Funnel. Both commands must keep running. Do not reset unrelated
Tailscale Serve configuration. Set the approving Mac's actual Tailscale hostname
on the agent (the hostname below is an example):

```sh
export PASSLET_ENDPOINT=https://your-mac.your-tailnet.ts.net:8443
```

Tailscale policy controls network access; Passlet has no node allowlist. It obtains
the caller from Serve's PROXY v2 connection and Tailscale WhoIs. Request bodies
and HTTP headers cannot override that identity.

## Request and use access

Ask for the least access your task needs. For one command, `exec` requests approval
and then runs it with the scoped credential:

```sh
passlet exec \
  --repos acme/project \
  --permissions contents:read \
  -- gh api repos/acme/project/contents
```

To reuse a grant for multiple commands:

```sh
passlet_dir=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/passlet.XXXXXX")
chmod 700 "$passlet_dir"
passlet request \
  --repos acme/project \
  --permissions contents:write,pull_requests:write \
  --out "$passlet_dir/grant.json"

passlet exec --grant "$passlet_dir/grant.json" -- \
  git clone https://github.com/acme/project.git
passlet exec --grant "$passlet_dir/grant.json" -- \
  gh pr list --repo acme/project
```

Use HTTPS Git remotes. `exec --grant` does not contact your Mac. It overrides
ambient Git/gh credentials for its child process and does not persist the token
in Git configuration. Configure commit author identity locally in your checkout
if needed; API attribution and Git commit metadata are separate concepts.

Always use `--out` for agent requests: without it, grant JSON goes to stdout.
The grant file is mode 0600 and cannot overwrite an existing file. Keep its
contents out of transcripts. Remove the file when finished. Deleting a file does
not revoke its token. GitHub sets expiry, typically eight hours.

## Claude skill

Copy [skills/passlet/SKILL.md](skills/passlet/SKILL.md) to
`~/.claude/skills/passlet/SKILL.md` on the agent machine. Set `PASSLET_ENDPOINT`
before launching Claude. The skill teaches Claude to request minimum access,
reuse approved grants, and handle unavailable Macs without credential fallbacks.

The skill ships in Linux archives and in the macOS app at
`Passlet.app/Contents/Resources/skills/passlet/SKILL.md`.

## Boundaries and failures

- Requests accept one repository owner, up to three repositories and eight
  permissions, with bounded prompt length. No caller descriptions or requested TTL.
- No queue, automatic retry, unattended approval, or early token revocation.
  If your Mac is unavailable, the agent can continue work that needs no new token
  and ask you to bring it online before requesting again.
- Connection timeout is 5 seconds, server deadline 120 seconds, and client timeout
  130 seconds. Disconnects and timeouts cancel pending native authentication.
- A scoped token is still a bearer credential. Anyone who obtains it can use its
  granted access until it expires. Your agent must protect the delivered grant.
- WhoIs identifies a Tailscale node, not an individual process or Unix user on a
  shared server. Only approve callers and scopes you intend to authorize.
- Serve's backend listener is loopback-only. Local processes are trusted on that
  hop: PROXY headers are not cryptographically authenticated. Do not use this
  boundary between mutually untrusted users on the same Mac.
- A token's effective access is limited by its requested scope, the GitHub App's
  permissions and installation, and the authorizing user's access. Some listing
  endpoints may expose repository metadata outside the requested content scope.
- Refresh tokens, parent access tokens, and client secrets never go to the agent.
  A refresh is saved through the authenticated Keychain context before delivery;
  failure to store rotated credentials may require fresh GitHub authorization.

## Audit

The local SQLite audit log contains request IDs, caller identity, requested and
granted access, decisions, expiry, and delivery outcome. It contains no tokens.
A successful response write records delivery from the server's perspective; it
is not an acknowledgement that an agent stored the token.

```sh
sqlite3 "$HOME/.local/share/passlet/audit.sqlite" \
  'SELECT data FROM events ORDER BY rowid DESC LIMIT 10;'
```

## Build and test

Install Nix with flakes enabled, then enter `nix-shell` (or run `direnv allow`).
`nix/flake.lock` pins Go, Python, and the lint/build tools for local development, CI,
and releases. macOS builds also require Apple Command Line Tools and an appropriate
signing identity/profile. `go.mod` and `go.sum` pin the Go dependencies.

```sh
nix-shell
make check
make build
lint
```

Use `nix flake update --flake ./nix` to update the environment lock, and `go get -u ./... &&
go mod tidy` inside the shell to update Go dependencies. Commit both lock files
and run the checks before opening a PR.

The Sunday updater in `teamniteo/ops` tracks `nix/flake.lock`, Go modules, and
workflow action pins. CI uses `teamniteo/gha-actions/nix` to load this same
devenv-backed shell once; subsequent steps inherit its tools.

`make check` verifies modules, formatting, vet, race tests, and signing metadata.
Tests use fakes and local HTTP/IPC fixtures; they need no GitHub credentials or
Touch ID and cannot disable approval in the production executable.

For a development-signed macOS helper:

```sh
export PASSLET_PROVISIONING_PROFILE=/private/path/to/development.provisionprofile
export PASSLET_SIGNING_IDENTITY=YOUR_SIGNING_CERTIFICATE_FINGERPRINT
make macos
```

Use an explicit App ID and a development profile containing your Mac and identity.
The output is `build/Passlet.app`. Distribution uses a separate Developer ID build,
notarization, and stapling; see [RELEASING.md](docs/RELEASING.md). The production
bundle's separate Keychain identity requires its own credential import.

## License

GNU General Public License v3.0 (GPL-3.0-only). See [LICENSE](LICENSE).
