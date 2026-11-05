<h1 align="center">
  <a href="https://passlet.dev"><img src="assets/passlet.png" width="64" valign="middle" alt="Passlet icon"></a> Passlet
</h1>

<p align="center">
  Short-lived credentials for your agents.
  <br>
  GitHub API actions retain your identity.
  <br>
  <a href="#features">Features</a> · <a href="#install-and-set-up-your-mac">Get started</a> · <a href="https://passlet.dev">Website</a> · <a href="https://github.com/ParetoSecurity/passlet/releases">Releases</a> · <a href="https://discord.gg/989fwbsyw7">Discord</a>
</p>

## About

Give your coding agent GitHub access when it needs it. The agent asks for specific repositories and permissions, and you approve on your Mac with Touch ID.

Passlet delivers a short-lived token the agent can use with Git and `gh`, even after your Mac goes offline. Your long-lived credentials stay in your Mac's protected Keychain.

## Features

| Feature | What it does |
| --- | --- |
| Scoped GitHub access | Approve the repositories and permissions needed for each task. |
| Native approval | Review each request on your Mac and approve with Touch ID. |
| Protected credentials | Keep parent tokens, refresh tokens, and client secrets in your Mac's Keychain. Only the scoped token reaches the agent. |
| Local and remote agents | Connect through a local socket, SSH forwarding, or opt-in Tailscale HTTPS. |
| macOS and Linux agents | Run your agents on macOS or Linux. |
| Claude Code and Codex plugins | Let your agent request access and reuse approved grants through the bundled skill. |
| Work after approval | Use delivered tokens until expiry, even while your Mac is offline. |
| Audit history | Inspect requests, decisions, scopes, and delivery outcomes without recording credentials. |

We're hoping to do these next:

| Feature | Status |
| --- | --- |
| Fly.io, AWS, Cloudflare, and Vercel providers | Coming soon |
| Approve requests on Linux and Windows | Coming soon |
| iOS and Android companion apps for approvals | Coming soon |
| Resume setup after organization approval | Coming soon |
| Renew GitHub authorization without recreating your app | Coming soon |

## Install and set up your Mac

Approving requests requires macOS 14 or later and a logged-in graphical session.

Install [Passlet Approver on your Mac](docs/INSTALLATION.md#install-approver-on-macos) to hold credentials and approve requests. Passlet Agent is included for local coding tools. The guide starts with Nix/Home Manager.

Install through [Homebrew or Nix/Home Manager](docs/INSTALLATION.md). The package manager installs the CLI and signed approval helper, starts the per-user service, and owns updates and removal.

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install --cask paretosecurity/passlet/passlet
passlet approver github setup
passlet doctor
```

The first CLI-only release must publish package metadata before these installation commands are available.

`setup github` opens your browser to create a personal GitHub App, install it on eligible repositories, and authorize your GitHub account. Review the maximum permissions shown in GitHub. The default maximum requests the highest supported level for the repository, organization, and account permissions in the checked-in GitHub API catalog, including administration, secrets, and account keys. Metadata remains read-only. Enterprise-only and single-file permissions are excluded. Select All repositories during installation; each agent token remains scoped to explicitly approved repositories. Each later agent request must still name its exact repositories and permissions, within that maximum.

Credentials go directly into user-presence-protected Keychain storage (`WhenUnlockedThisDeviceOnly`). The unused private signing key returned by GitHub is discarded without being written to disk. No shared Passlet app secret or broker is needed. Setup verifies the authorizing user owns the app and that an installation grants its requested permissions with accessible repositories. Pending organization approval or an interrupted setup produces an actionable error; remove the incomplete app in GitHub before starting a fresh setup. Setup refuses to replace an existing protected credential.

The package-managed LaunchAgent starts at graphical login and restarts after a crash. It never unlocks credentials. Setup configures providers only. See [setup and troubleshooting](docs/SETUP.md).

## Local agents

On your Mac, no endpoint configuration, SSH, or Tailscale is needed. The default endpoint is `unix://$HOME/.local/share/passlet/passlet.sock`. Use `passlet connect unix://$HOME/.local/share/passlet/passlet.sock` to save the local endpoint. Clear any explicit `PASSLET_ENDPOINT` to use the saved setting.

## Remote agents over SSH

On your Approver Mac, declare a label for each SSH tunnel. With Home Manager, set `programs.passlet.service.ssh = [ "cruncher" ];` and reactivate. With Homebrew, add `ssh = ["cruncher"]` to `~/.config/passlet/approver.toml`, preserving any Tailscale setting, and restart the service. The Approver creates `~/.local/share/passlet/ssh-cruncher.sock`; approval prompts identify that route as “cruncher.” See [SSH tunnel configuration](docs/SETUP.md#ssh-tunnel-labels) for multiple servers and SSH config examples.

On the remote machine, install the Linux CLI archive and create a private directory under your Unix account:

```sh
mkdir -p "$HOME/.local/share/passlet"
chmod 700 "$HOME/.local/share/passlet"
```

From your Mac, keep this SSH connection open, substituting your actual remote username and host. The path before the colon is the remote absolute path; the path after it is on your Mac:

```sh
ssh -NT -o ExitOnForwardFailure=yes \
  -o StreamLocalBindUnlink=yes \
  -R /home/alice/.local/share/passlet/passlet.sock:"$HOME/.local/share/passlet/ssh-cruncher.sock" \
  alice@cruncher
```

On that remote account, before starting Claude:

```sh
passlet connect unix:///home/alice/.local/share/passlet/passlet.sock
passlet doctor
```

The remote SSH server must permit Unix socket forwarding. See [SSH server configuration, stale sockets, and Alice/Bob setup](docs/SETUP.md). Bob uses his own remote Unix account, socket path, Mac, and GitHub App. Do not share a Unix account or use a shared `/tmp/passlet.sock`.

## Choose an Approver

Run `passlet connect <endpoint>` once on each agent account to check reachability and save the Approver address in `~/.config/passlet/endpoint`. It supports HTTPS endpoints and absolute Unix socket URLs, including SSH-forwarded sockets. The health check does not request approval or verify GitHub credentials. If it fails, the previous endpoint remains saved. Use `passlet doctor` for credential verification.

```sh
passlet connect https://your-mac.your-tailnet.ts.net:17400
```

Commands choose their endpoint in this order: `--endpoint`, `PASSLET_ENDPOINT`, the saved endpoint, then the default local socket. `passlet connect` refuses to check or save a connection while `PASSLET_ENDPOINT` is set. Unset it before connecting. An unreadable or invalid saved setting produces an error rather than silently using another Approver. `passlet doctor` shows and checks the effective endpoint.

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

The receipt reports user and actual expiry without printing the token. Use `--out` when saving a grant for reuse. Without it, JSON containing the token goes to stdout and should be passed directly to `exec` rather than printed to the terminal. The new grant file is mode 0600 and cannot overwrite another file. Remove it when finished; deleting it does not revoke the token.

For one-off commands in Bash or Zsh, use process substitution to pass the grant without saving a file:

```sh
passlet exec \
  --grant <(passlet request --repos acme/project --permissions contents:read) \
  -- gh repo view acme/project
```

The grant travels through a separate pipe, leaving the child command's standard input available for interactive use or piped data. `request` owns the scope and endpoint options. `exec` requires `--grant` and never requests approval itself.

Use HTTPS Git remotes. `exec --grant` uses the delivered token without contacting the Mac, overrides ambient Git/gh credentials for the child, and does not save the token in Git configuration. Tokens typically expire in eight hours, as set by GitHub. Parent tokens, refresh tokens, and client secrets never go to the agent.

## Tailscale, explicitly opt-in

Enable `programs.passlet.service.tailscale = true;` with [Home Manager](docs/INSTALLATION.md#configure-tailscale-forwarding), or set `tailscale = true` in `~/.config/passlet/approver.toml` for Homebrew and restart the service.

Passlet keeps its local socket and creates a separate private socket for Tailscale Serve. At startup it checks forwarding on HTTPS port `17400`, leaves matching configuration alone, and creates it when absent. It refuses conflicting configuration and preserves unrelated Serve routes. No loopback TCP listener or separate setup command is needed. Tailscale must be running, signed in, and version 1.104.4 or newer. Its permissions and tailnet HTTPS settings must allow Serve configuration.

Run `passlet approver doctor` on the Approver Mac to see the connection instructions. Local agents connect automatically. On remote agents, use `passlet connect https://your-mac.your-tailnet.ts.net:17400`. Configure Tailscale network policy to allow intended callers on that port. Use Serve, not Funnel.

Only the separate private socket trusts Serve's forwarded peer address. Passlet verifies that address with `tailscale whois`, shows the device name in approval prompts, and records the full hostname and node ID in audit events. Normal local and SSH socket clients cannot select a Tailscale identity through HTTP headers.

## Agent plugins

Install the native plugin on each agent account. Both plugins use the same
[Passlet skill](skills/passlet/SKILL.md).

Claude Code:

```sh
claude plugin marketplace add ParetoSecurity/passlet
claude plugin install passlet@passlet --scope user
```

Codex CLI:

```sh
codex plugin marketplace add ParetoSecurity/passlet
codex plugin add passlet@passlet
```

Start a new agent session after installation. To update:

```sh
# Claude Code
claude plugin marketplace update passlet
claude plugin update passlet@passlet

# Codex CLI
codex plugin marketplace upgrade passlet
codex plugin add passlet@passlet
```

Install the Passlet CLI separately and configure the transport first. Local Mac agents use the default socket; remote agents save their Approver address with `passlet connect`. Installing a plugin does not mint a token or bypass native approval. Remove any previous manually installed standalone Passlet skill to avoid duplicate skills. The skill is also included in release archives and the app's `Contents/Resources/skills`.

The app and both agent plugins share the same version. The release process generates both plugin manifests alongside the Homebrew/Nix metadata, and the package-update PR publishes them together. Do not bump plugin versions separately.

## Updating

Use `brew upgrade --cask paretosecurity/passlet/passlet`, or update your pinned Passlet Nix source and activate Home Manager. The package manager restarts the service. Uninstall through the same manager. Credentials and audit history remain in place.

## Diagnostics and audit

Run `passlet` or `passlet --help` for the command list. Client commands stay at the top level: `connect`, `request`, `exec`, and `doctor`. Local service and credential commands live under `passlet approver`: `start`, `doctor`, `github setup`, `github reset`, and `audit`. A Mac can approve requests and also run client commands, without choosing an installation mode.

Run `passlet approver` to discover commands for managing approvals and credentials. `passlet doctor` checks the selected endpoint, while `passlet approver doctor` always checks the local service, ignoring `PASSLET_ENDPOINT` and the saved connection. Both verify GitHub access with native approval and issue no token. A stopped local service fails without prompting. Local socket permission errors, timeouts, and unhealthy responses are reported separately. Denying approval stops diagnostics immediately. After successful verification, local diagnostics show socket and Tailscale listener status and connection instructions for verified Tailscale forwarding.

```sh
passlet doctor
passlet approver audit --limit 20
```

`doctor` asks for Touch ID approval on the Approver’s Mac, unlocks Keychain, refreshes authorization when needed, and verifies your GitHub identity. It works through local and remote endpoints and returns nonzero if verification fails. It never issues an agent token. `audit` reads an existing local SQLite database without creating one and shows readable event summaries. Use `audit --json` for complete structured events, including caller/transport, scopes, decisions, expiry, and delivery outcome, with no credentials.

## Trust and failure boundaries

- Requests infer the app label from `CLAUDECODE=1` (Claude) or a nonempty `CODEX_THREAD_ID` (Codex). Missing or conflicting markers display “Shell”. Inference happens on the Agent machine for both `request` and `exec` when requesting access. This label is unverified and does not authenticate the requesting app. The Approver determines the device or socket route separately.
- A socket connection proves access to the socket, not that Claude initiated it. SSH labels identify the configured socket route, not an authenticated remote host or process. A process running as your Mac user or root can access every route socket. Other processes in the same account can request approval and read that account's delivered grants. Remote root is trusted and can access Alice or Bob's sockets. Prompts describe socket callers honestly.
- Tailscale identifies a node, not a process or remote Unix account. On a shared host, use separate SSH-forwarded sockets for each user's Mac. The private Tailscale socket trusts Tailscale and processes with access to the same Unix account.
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

Swift validation, lifecycle, and helper process tests run with `make check-macos` and in macOS CI without requesting approval or reading protected credentials. See [manual and UI validation](docs/VALIDATION.md) and [release controls](docs/RELEASING.md). For a development-signed helper, set `PASSLET_PROVISIONING_PROFILE` and `PASSLET_SIGNING_IDENTITY`, then `make macos`. The output is `build/Passlet.app`; use a dedicated test account/profile for isolated Keychain testing.

The default package set and Home Manager use the stable 26.05 release. Go 1.27 comes from stable. Only `golangci-lint` comes from a separate pinned `nixpkgs-unstable` input because the stable linter is built with Go 1.26. Both package sets are updated by the weekly updater.

`nix/flake.lock` pins the tools and the non-flake `teamniteo/agents` input. Entering the environment links upstream rules and skills into `.agents` (`.claude` is a committed symlink to it) alongside the repository's own `passlet` and `release-passlet` skills. Generated links are ignored and are not release assets. `nix flake update --flake ./nix` advances these conventions along with dependencies; the lock and workflow pins use the format supported by the Sunday updater in `teamniteo/ops`. Its `updates/projects.yaml` still needs a Passlet entry with `remote: ParetoSecurity/passlet`, `flakes: [nix]`, `go: ["."]`, `actions: [.github/workflows]`, `lint: {dir: ".", target: check-all}`, `ci: ci.yml`, and `active: true` before weekly updates include this repository. Update Go modules inside the shell with `go get -u ./... && go mod tidy`, then refresh the integration package’s `vendorHash` in `nix/test-package.nix` if dependencies changed and run checks. CI uses standard GitHub-hosted Linux/macOS runners and the same `teamniteo/gha-actions/nix` environment. It pulls prebuilt devenv dependencies from the public `devenv` Cachix cache without a cache credential. GitHub Actions caches Go modules and compilation outputs per OS, architecture, dependency/toolchain lock, and commit, with reuse across commits sharing that lock. Fork PR caches remain scoped by GitHub and are not restored by the signing workflow. Linux linting shares the unit-test environment, while NixOS integration runs separately. If a newly pinned Nix dependency is missing from the binary cache, the cold run still builds it from source.

## License

MIT License. See [LICENSE](LICENSE).

### Download a development build

The macOS CI job builds and uploads a universal app before running its tests. Linux checks and NixOS integration run in parallel. Open the [CI runs](https://github.com/ParetoSecurity/passlet/actions/workflows/ci.yml), select the commit or PR, and download `Passlet-macOS-preview.zip` from Artifacts once the build job finishes. Extract the ZIP once to obtain `Passlet.app`. Tests may still be running, so check the complete run before trusting a change. After CI finishes, a bot updates one PR comment with the preview download link, build-job link, and protected-Keychain limitation. The commenting workflow activates after it is merged into the default branch.

Preview builds use ad-hoc signatures without an Apple signing identity, provisioning profile, or notarization. They have a separate `dev.passlet.preview` bundle identifier and cannot exercise the production protected-Keychain flow. Use them to test CLI features that do not require credentials. Keep the released app installed for real credentials. Do not run preview and release services simultaneously because they share the local service configuration. macOS may require allowing the downloaded preview in System Settings → Privacy & Security. Only run artifacts from code you trust, including when downloading PR builds.

Developer ID signing and notarization remain exclusive to approved releases. To reproduce the preview locally, run `PASSLET_BUILD_MODE=unsigned ./scripts/build-macos.sh` inside devenv.
