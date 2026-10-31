<img src="assets/passlet.png" width="96" alt="Passlet icon">

# Passlet

Short-lived credentials for your agents.

Your agent running in a remote server or a sandbox needs access to GitHub. Let it ask you for scoped permissions on-the-fly instead of giving it perpetual carte blanche access.

Approve each request on your Mac with one native Touch ID/password prompt. Passlet retrieves protected Keychain credentials using the same authentication context, obtains a repository- and permission-scoped GitHub App user token, and delivers only that token. Git and `gh` work independently until expiry, even with your Mac offline. API actions retain your GitHub identity; Git commit author metadata is configured separately.

[Website](https://passlet.dev) · [Releases](https://github.com/ParetoSecurity/passlet/releases) · [Discord](https://discord.gg/989fwbsyw7)

## Version and requirements

This branch implements v0.2; v0.1.0 is already released. Follow these instructions with the v0.2 build. There are no compatibility aliases or migrations before v1.0.

- Approver: macOS 14+, a logged-in graphical session, and a signed Passlet app. No menu bar app is required.
- Agent: macOS or Linux, Passlet on PATH, and Git and/or GitHub CLI.
- Remote agents: OpenSSH with Unix socket forwarding, or opt-in Tailscale HTTPS.
- Each person owns and authorizes their own GitHub App. Organization installation may require an organization administrator.

## Install and set up your Mac

Download the matching release archive and verify its checksum against `SHA256SUMS`. Extract `Passlet.app` into `/Applications`. Keep the CLI and helper together inside the signed bundle:

```sh
mkdir -p "$HOME/.local/bin"
ln -s /Applications/Passlet.app/Contents/MacOS/passlet "$HOME/.local/bin/passlet"
export PATH="$HOME/.local/bin:$PATH"
passlet version
passlet setup github
passlet service install
passlet doctor
```

`setup github` opens your browser to create a personal GitHub App, install it on eligible repositories, and authorize your GitHub account. Review the maximum permissions shown in GitHub. The default maximum includes Contents, Pull requests, Issues and Workflows write, plus Actions read. Override it with `--permissions contents:read,pull_requests:read` if you want a read-only app. Each later agent request must still name its exact repositories and permissions, within that maximum.

Credentials go directly into user-presence-protected Keychain storage (`WhenUnlockedThisDeviceOnly`). The unused private signing key returned by GitHub is discarded without being written to disk. No shared Passlet app secret or broker is needed. Setup verifies the authorizing user owns the app and that an installation grants its requested permissions with accessible repositories. Pending organization approval or an interrupted setup produces an actionable error; remove the incomplete app in GitHub before starting a fresh setup. Setup refuses to replace an existing protected credential.

`service install` creates a per-user launch agent that starts at graphical login and restarts after a crash. It does not unlock credentials. Opening the app alone does not start the service. For foreground troubleshooting, stop the login service first, then run `passlet serve`. See [setup and troubleshooting](docs/SETUP.md).

## Local agents

On your Mac, no endpoint configuration, SSH, or Tailscale is needed. The default endpoint is `unix://$HOME/.local/share/passlet/passlet.sock`. Clear any explicit `PASSLET_ENDPOINT` if you want the default.

## Remote agents over SSH

On the remote machine, install the Linux CLI archive and create a private directory under your Unix account:

```sh
mkdir -p "$HOME/.local/share/passlet"
chmod 700 "$HOME/.local/share/passlet"
```

From your Mac, keep this SSH connection open, substituting your actual remote username and host. The path before the colon is the remote absolute path; the path after it is on your Mac:

```sh
ssh -NT -o ExitOnForwardFailure=yes \
  -o StreamLocalBindUnlink=yes \
  -R /home/alice/.local/share/passlet/mac.sock:"$HOME/.local/share/passlet/passlet.sock" \
  alice@remote-box
```

On that remote account, before starting Claude:

```sh
export PASSLET_ENDPOINT=unix:///home/alice/.local/share/passlet/mac.sock
passlet doctor
```

The remote SSH server must permit Unix socket forwarding. See [SSH server configuration, stale sockets, and Alice/Bob setup](docs/SETUP.md). Bob uses his own remote Unix account, socket path, Mac, and GitHub App. Do not share a Unix account or use a shared `/tmp/passlet.sock`.

## Request and use access

Ask for the minimum scope needed. Keep grant contents out of terminal transcripts:

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

The receipt reports user and actual expiry without printing the token. Always pass `--out` for agent requests; without it, JSON containing the token goes to stdout. The new grant file is mode 0600 and cannot overwrite another file. Remove it when finished; deleting it does not revoke the token.

Use HTTPS Git remotes. `exec --grant` uses the delivered token without contacting the Mac, overrides ambient Git/gh credentials for the child, and does not save the token in Git configuration. Tokens typically expire in eight hours, as set by GitHub. Parent tokens, refresh tokens, and client secrets never go to the agent.

## Tailscale HTTPS, explicitly opt-in

The default service opens no TCP port. To enable Tailscale alongside the local socket, install the service with `--tailscale-listen 127.0.0.1:8484` (uninstall an existing login service before changing its arguments), then expose only that loopback backend:

```sh
passlet service install --tailscale-listen 127.0.0.1:8484
tailscale serve --proxy-protocol=2 --tls-terminated-tcp=8443 tcp://127.0.0.1:8484
```

Use Tailscale Serve, not Funnel. Keep Serve running and configure network policy to allow intended callers on TCP 8443. Do not reset unrelated Serve configuration. On the agent, set `PASSLET_ENDPOINT=https://your-mac.your-tailnet.ts.net:8443`. Passlet uses PROXY v2 and WhoIs for caller identity; no Passlet node allowlist is added.

## Claude skill

Copy [skills/passlet/SKILL.md](skills/passlet/SKILL.md) to `~/.claude/skills/passlet/SKILL.md` on each agent account. Local Mac agents use the default socket; remote agents inherit their own `PASSLET_ENDPOINT`. The skill requests minimum access, reuses grants, and stops on unavailable Macs without credential fallbacks. It is also included in release archives and the app's `Contents/Resources/skills`.

## Status, diagnostics, and audit

```sh
passlet status
passlet doctor
passlet audit --limit 20
passlet service status
```

Diagnostics never mint tokens or trigger authentication. They distinguish locally recorded GitHub identity from current GitHub state; protected Keychain availability can remain unknown without authentication. `doctor` returns nonzero for detected faults and suggests repairs. `audit` reads an existing local SQLite database without creating one and emits structured events: caller/transport, scopes, decisions, expiry, and delivery outcome, with no credentials.

## Trust and failure boundaries

- A socket connection proves access to the socket, not that Claude initiated it. Other processes in the same account can request approval and read that account's delivered grants. Remote root is trusted and can access Alice or Bob's sockets. Prompts describe socket callers honestly.
- Tailscale identifies a node, not a process or remote Unix account. On a shared host, use separate SSH-forwarded sockets for each user's Mac. The loopback PROXY backend trusts local Mac processes; do not treat it as an isolation boundary between hostile local users.
- Every request requires native user presence. There are no queues, automatic request retries, client pairing, session keys, unattended approvals, or early revocation.
- If the Mac is offline, continue work requiring no new credential. Bring it online and make a new explicit request when ready. The connection timeout is 5 seconds, server deadline 120 seconds, and client timeout 130 seconds. Timeout or disconnect cancels pending authentication.
- Tokens are bearer credentials. Anyone who obtains one can use its granted access until expiry. Effective access is constrained by the requested scope, app permissions/installation, and the author's GitHub access. Some GitHub listing endpoints expose metadata beyond the scoped content repositories.
- Requests accept one repository owner, at most three repositories and eight permissions, with bounded prompt text. No caller-provided description or requested TTL is accepted.
- A successful response write records delivery from the server's perspective, not proof the agent saved it. Failed rotated-credential storage may require fresh GitHub authorization.

## Development and validation

Install Nix with flakes enabled, then use the pinned devenv shell:

```sh
nix-shell
make check-all
make integration             # Linux KVM, or a configured Linux remote builder
make check-macos             # macOS with Apple Command Line Tools
```

The pinned flake exports `lib.integrationTest`; `nix/tests.nix` supplies the repository source to it. On an Apple Silicon Mac with an aarch64 Linux KVM builder, run `nix-build nix/tests.nix --argstr system aarch64-linux --no-out-link`. Three isolated NixOS guests run real SSH, Unix accounts, Git, gh, and Passlet. Approval and GitHub fixtures exist only in the test binary; production has no bypass switches. These VM tests do not test Keychain, Touch ID, launchd, or Mac sleep/wake.

See [manual and UI validation](docs/VALIDATION.md) and [release controls](docs/RELEASING.md). For a development-signed helper, set `PASSLET_PROVISIONING_PROFILE` and `PASSLET_SIGNING_IDENTITY`, then `make macos`. The output is `build/Passlet.app`; use a dedicated test account/profile for isolated Keychain testing.

`nix/flake.lock` pins the tools and the non-flake `teamniteo/agents` input. Entering the environment links upstream rules and skills into `.claude`, preserving the local Passlet skill. Generated links are ignored and are not release assets. `nix flake update --flake ./nix` advances these conventions along with dependencies; the lock and workflow pins use the format supported by the Sunday updater in `teamniteo/ops`. Its `updates/projects.yaml` still needs a Passlet entry with `remote: ParetoSecurity/passlet`, `flakes: [nix]`, `go: ["."]`, `actions: [.github/workflows]`, `lint: {dir: ".", target: check-all}`, `ci: ci.yml`, and `active: true` before weekly updates include this repository. Update Go modules inside the shell with `go get -u ./... && go mod tidy`, then refresh the integration package’s `vendorHash` in `nix/test-package.nix` if dependencies changed and run checks. CI uses standard GitHub-hosted Linux/macOS runners and the same `teamniteo/gha-actions/nix` environment. It pulls prebuilt devenv dependencies from the public `devenv` Cachix cache without a cache credential. GitHub Actions caches Go modules and compilation outputs per OS, architecture, dependency/toolchain lock, and commit, with reuse across commits sharing that lock. Fork PR caches remain scoped by GitHub and are not restored by the signing workflow. Linux linting shares the unit-test environment, while NixOS integration runs separately. If a newly pinned Nix dependency is missing from the binary cache, the cold run still builds it from source.

## License

GNU General Public License v3.0 (GPL-3.0-only). See [LICENSE](LICENSE).
