# Setup and troubleshooting

Use the signed v0.2 bundle on each approving Mac and the matching CLI on the agent. No machine-wide daemon or root service is needed on the Mac. Setup, approval, and login startup run as the logged-in person.

## GitHub onboarding

Run `passlet approver github setup` on your Approver Mac and enter your GitHub handle, or use `passlet approver github setup --handle alice`. The app name is pre-filled as `passlet-alice` and remains editable on GitHub. Long handles are shortened to fit GitHub’s 34-character app-name limit. Setup checks for an existing app with that name before opening the browser and stops with a settings link if found. GitHub still validates names during creation. The loopback listener runs until setup completes or the setup process stops. It uses independent state values for manifest conversion and OAuth, a random callback path, and PKCE. Browser confirmation is required for app registration, installation, and user authorization. The app belongs to that person; install it in any organizations where it should operate. Installing an app does not grant the user access they did not already have.

After app creation and authorization, GitHub's authorization callback redirects the same browser tab to the installation page. Choose **All repositories** for the account or organization. The full default permission set requires an organization owner to approve installation on an organization. GitHub redirects to `http://127.0.0.1:17402/github/installed`; Passlet verifies that installation's app, user, permissions, and accessible repositories before saving credentials and showing success. The listener exists only during setup. If the port is occupied, setup stops before opening the browser; it never silently changes ports. The predictable installation callback is a signal to verify access, not proof of authorization. Manifest conversion and OAuth retain separate random state values, session paths, and PKCE. If GitHub rejects saved authorization, setup directs you to reset and configure again.

If organization approval is pending, ask its administrator to approve the installation and permissions. If setup fails before saving credentials, delete the incomplete personal app from GitHub's Developer settings before restarting. Do not copy callback URLs, OAuth codes, or secret responses into support reports. If Keychain already contains credentials, setup asks for Touch ID to read the configured app identity and opens GitHub’s installation page without replacing credentials. Use `passlet approver github reset` to deliberately remove local credentials before starting over; protected items may not appear in Keychain Access.

App/account identity and tokens are stored together in one protected Keychain item. There is no separate metadata file. Noninteractive status checks do not unlock credentials or display account identity.


## Login startup

[Homebrew or Home Manager](INSTALLATION.md) installs the per-user LaunchAgent and manages updates and removal. It starts at graphical login and restarts failures with a ten-second throttle. Startup never runs provider setup or unlocks Keychain. Do not edit the generated plist or install the service through another manager.

Run `passlet doctor` to check availability. For launchd details use `launchctl print gui/$(id -u)/dev.passlet.agent`. If startup fails, verify you are in the logged-in Mac's graphical session and check this output for its last exit status. Reinstall the Homebrew cask or reactivate Home Manager after correcting the problem.

To troubleshoot in the foreground, stop the managed service with `launchctl bootout gui/$(id -u)/dev.passlet.agent`, then run `passlet approver start`. This is a temporary diagnostic command, not an installation method. Reinstall/reactivate the package when finished. A private lock prevents two approvers from replacing each other's active socket. Never delete a live socket or its lock to work around an error.

Uninstalling through the package manager removes startup while retaining credentials and audit history. Sleep temporarily makes the Mac unavailable; delivered tokens remain usable.

## Pair agents

Successful `passlet approver github setup` pairs the local Agent automatically and switches its saved connection to this Mac, replacing any saved remote endpoint. Existing keys are reused. If the local Agent was revoked, setup requires explicit pairing instead of restoring its access.

Installation, service startup, and Tailscale settings are declarative. Pairing is managed by Passlet and survives Home Manager rebuilds. There are no public keys or pairing secrets to copy into Nix configuration.

On your Mac, run:

```sh
passlet approver pair
```

Use the printed command that matches the Agent's connection. For Tailscale, use the command with your Mac's address:

```sh
passlet pair --endpoint https://your-mac.your-tailnet.ts.net:17400 --code <code>
```

For the default socket on the Agent, omit the endpoint. This applies both to a local Agent on the Mac and to an SSH-forwarded socket at `~/.local/share/passlet/passlet.sock`:

```sh
passlet pair --code <code>
```

Approve the Agent's suggested `user@hostname` label on your Mac with Touch ID. Only approve a pairing you initiated. Pairing does not unlock or inspect provider credentials. Each credential request still needs its own approval.

The random pairing code pins the Approver's Ed25519 public key and carries a 256-bit secret. It expires after ten minutes, is consumed before approval, and can be used once. Creating a new code invalidates the previous code. A denied or interrupted pairing needs a fresh code. Treat the code as a temporary secret, share it only with your intended Agent, and avoid recording it in logs or screenshots.

Passlet creates missing private keys using Go's standard library and preserves existing keys. Each user on each machine has a separate key in `~/.local/share/passlet/agent.pem`; the Mac's key is `approver.pem`. These files stay private and never enter Git or the Nix store. The Agent saves its endpoint and Approver pin atomically in `connection.json`. The Approver saves trusted Agent labels and keys in `pairings.json`. All connections use mutually authenticated TLS 1.3, including local and SSH sockets. Pairing identifies possession of a key, not a particular executable. Other processes running under that account, and root, remain trusted.

Manage paired Agents on the Mac:

```sh
passlet approver agents list
passlet approver agents revoke alice@cruncher
```

Revocation takes effect on subsequent requests without restarting the service, including requests on an existing connection. It does not revoke GitHub tokens already issued to that Agent. Revoke an existing name before pairing a different key under the same name.

Commands select their endpoint in this order: `--endpoint`, `PASSLET_ENDPOINT`, the saved pairing, then the local socket. An endpoint override does not change the pinned Approver key. Unset `PASSLET_ENDPOINT` before pairing. Use `passlet doctor` to check an existing pairing.

When upgrading from v0.5.0, remove `programs.passlet.agent`, `programs.passlet.service.agents`, and `programs.passlet.service.privateKeyFile` from Home Manager. Remove `agents` and `private_key_file` from manually managed `approver.toml`. Keep service and Tailscale settings. Upgrade both ends. Successful GitHub setup pairs the local Agent automatically; pair other Agents explicitly. Old `agent.toml` and `endpoint` files are unused and may be removed. Existing private keys at the default paths are retained.

## SSH forwarding

Local and SSH Agents use the same `~/.local/share/passlet/passlet.sock` on the Mac. The Approver creates it on startup with mode `0600` in a private directory. There are no per-tunnel sockets. The paired key determines the displayed name on every transport.

For an account named `alice`, add this to `~/.ssh/config` on the Approver Mac:

```sshconfig
Host cruncher
    User alice
    RemoteForward /home/alice/.local/share/passlet/passlet.sock /Users/alice/.local/share/passlet/passlet.sock
    ExitOnForwardFailure yes
```

SSH creates the first socket on Cruncher, whose parent directory must exist with mode `0700`. Keep `ssh cruncher` open for a shell and tunnel, or use `ssh -NT cruncher` for just the tunnel. Other servers can forward to the same Mac socket and authenticate using their own keys.

To share the tunnel across SSH sessions, add `ControlMaster auto`, `ControlPersist 10m`, and `ControlPath ~/.ssh/master-%C` to that host block. The master connection owns the forward and remains open for ten minutes after the last session exits. Start a new master connection when changing forwarding settings, since an existing master retains its previous configuration.

Resetting a disposable sandbox removes its Agent key and saved pairing. Pair the replacement sandbox again and revoke the old Agent on your Mac. Reusing a hostname does not restore its identity.

`passlet approver doctor` checks the shared local listener. It cannot determine whether an SSH tunnel is connected. Run `passlet doctor` on the remote machine to check the complete authenticated connection. Missing or stale forwarded sockets receive tunnel troubleshooting advice.

## SSH server configuration

Use a separate remote Unix account per person and a mode-0700 directory for each forwarded socket. The server's sshd must allow stream-local forwarding. An administrator can scope these settings to the intended users:

```text
Match User alice,bob
    AllowStreamLocalForwarding yes
    StreamLocalBindMask 0177
    StreamLocalBindUnlink yes
```

Validate sshd configuration before reloading it and retain an existing admin session. `StreamLocalBindUnlink` on the server allows a new tunnel to replace its stale remote socket after disconnect. The SSH client's setting alone does not configure remote sshd. When the server cannot enable it, inspect the old tunnel and remove only your own stale forwarded socket before reconnecting. Replacing a live forward would interrupt requests.

After pairing her Agent, Alice runs this from her Mac:

```sh
ssh -NT -o ExitOnForwardFailure=yes \
  -R /home/alice/.local/share/passlet/passlet.sock:"$HOME/.local/share/passlet/passlet.sock" alice@cruncher
```

Bob uses `/home/bob/.local/share/passlet/passlet.sock` and `bob@cruncher` from his Mac. Each remote account pairs with `passlet pair --endpoint unix:///home/alice/.local/share/passlet/passlet.sock --code <code>`, using Bob’s path for Bob. The paths are absolute, not `~`; Unix socket paths must be short enough for the platform (Passlet limits its local listener to 103 bytes). No new TCP listener is opened by this transport.

Unix permissions isolate Alice from Bob, but not either from remote root or other processes sharing their account. Never describe this as attesting the agent executable. Root can request approval through either socket and read delivered tokens. A compromised same-user process can also request approval; examine the scopes in the native prompt.

## Tailscale

Enable `programs.passlet.service.tailscale = true;` with Home Manager, or merge `tailscale = true` into the Homebrew Approver configuration. Restart after changing configuration. Tailscale must be running and signed in, and the service must be allowed to configure Serve. Passlet never invokes sudo or changes tailnet policy.

Passlet launches `tailscale` as an executable, so a shell alias is insufficient. When using the macOS GUI app, put an executable wrapper named `tailscale` in a directory on your PATH:

```sh
#!/bin/sh
exec /Applications/Tailscale.app/Contents/MacOS/Tailscale "$@"
```

Use a wrapper rather than symlinking the app executable: launching it through a symlink can prevent it from recognizing its app bundle. The login service has its own PATH, independent of your interactive shell. A successful `tailscale status` in your terminal does not establish that the Approver service can find the command.

The Approver binds `127.0.0.1:17401` and configures `tailscale serve --bg --tcp=17400 tcp://127.0.0.1:17401`. [Serve forwards the encrypted TCP stream](https://tailscale.com/docs/reference/tailscale-cli/serve) without terminating TLS. Passlet authenticates both peers and encrypts tokens itself, so it works with the GUI Tailscale app without giving its sandbox access to Unix sockets. Tailnet HTTPS certificates are not needed. Agents still use `https://your-mac.your-tailnet.ts.net:17400`, with the Approver public key pinned in their pairing configuration.

Startup reuses matching forwarding, creates it when absent, and refuses conflicts without changing unrelated ports. If port 17400 still has an old Passlet HTTPS proxy, verify it belongs to Passlet and remove only that route with `tailscale serve --https=17400 off` before restarting. Never reset unrelated Serve configuration. Funnel and TLS termination on this route are rejected.

Permit intended devices on TCP 17400 in tailnet policy. Pairing additionally distinguishes users on a shared server. Caller IP headers and Tailscale device names do not establish identity. Approval prompts show the trusted key's configured label; audit events record that label and its key fingerprint.

`passlet approver doctor` checks Tailscale status, the loopback listener, and Serve forwarding. It explains missing or mismatched configuration without changing it. Run `passlet doctor` on the Agent to verify end-to-end connectivity and authentication.

Disabling Tailscale stops the loopback listener after restart. The saved Serve route remains but cannot reach Passlet. To remove it, first verify it still belongs to Passlet, then use `tailscale serve --tcp=17400 off`.

## Coding-agent sandboxes

For HTTPS endpoints, Passlet uses the environment's proxy settings, including `HTTPS_PROXY` and `NO_PROXY`. Pairing and authenticated requests retain end-to-end TLS with the Approver key pinned. The proxy can see the destination and interrupt the connection, but cannot read credentials or impersonate the Approver. Unix sockets bypass proxies.

When Codex reports disabled networking and a connection is denied, Passlet identifies the sandbox restriction. Missing sockets, refused connections, and timeouts retain their own troubleshooting messages. Configure the required endpoint access, or approve running the individual Passlet command outside the sandbox. Do not automatically enable unrestricted networking or exclude every Passlet command.

A sandbox must allow connections to the selected Unix socket as well as access to the Agent's pairing files under `~/.local/share/passlet`. On a local Mac, the socket is `~/.local/share/passlet/passlet.sock`. Use the absolute path when configuring an allowlist. For Claude Code, this belongs in `sandbox.network.allowUnixSockets`. Do not make the socket world-accessible to work around a sandbox denial.

Run `passlet doctor` inside the same sandbox as the failing command. A permission denial can come from filesystem permissions or sandbox policy; it does not prove the Approver is stopped. Compare with a check outside the sandbox when diagnosing policy restrictions, remembering that doctor requests Touch ID approval.

Successful pairing and doctor checks establish access to the Approver. Git and `gh` still need their own network access. On macOS, sandbox restrictions on system trust services can also prevent `gh` from validating HTTPS certificates. Consult the coding agent's sandbox configuration and allow the required access without disabling TLS verification. Opening all network access is not a Passlet requirement.

`passlet exec` removes inherited Git configuration to prevent ambient credential helpers from supplying credentials. It preserves Git's effective `http.proxyAuthMethod` for GitHub through `GIT_HTTP_PROXY_AUTHMETHOD`, accepting only Git's supported authentication methods. An explicitly set `GIT_HTTP_PROXY_AUTHMETHOD` takes precedence. Proxy addresses must remain available through the command's environment, such as `HTTPS_PROXY`; other inherited Git configuration is not copied. Never include proxy credentials in diagnostic output.

When managing plugins through Nix, avoid having both Nix and a plugin manager own the same cache directory. Codex can replace a managed symlink with a directory, causing Home Manager's link-target check to fail on the next rebuild. Resolve only the conflicting plugin cache entry before activation; preserve pairing files, grants, and writable agent configuration. This is a plugin installation conflict, not an Approver failure.

## Diagnostics

`passlet doctor` checks the selected Approver, requests native approval on its Mac, unlocks Keychain, refreshes authorization when needed, saves rotated credentials, and verifies the GitHub identity. It uses the same approval lock and deadline as credential requests, including when called remotely. It returns a failure for denied approval, invalid credentials, or failed verification, and never issues an agent token. Both the Agent and Approver must support the diagnostic endpoint; an older Approver reports that an update is required.

Common failures:

| Failure | Next step |
| --- | --- |
| Approver unavailable | Start the Mac login service; for SSH verify the tunnel and remote socket path; for HTTPS verify Serve and network policy. |
| Directory is not private | Check ownership and set the socket directory to 0700. Do not share it between users. |
| Helper unavailable | Use the signed app bundle and keep CLI/helper together. |
| GitHub setup missing | Run `passlet approver github setup` on the approving Mac. |
| Repository unavailable | Check the app installation, pending organization approvals, and the user's repository access. |
| GitHub denies permission | Confirm the operation and app's maximum permissions; request only the specific missing scope. |
| Approver busy | Complete or cancel the existing request; do not poll or retry automatically. |
| Approval timeout/disconnect | Restore connectivity and make a new explicit request when the user is ready. |
| Authorization expired/invalid | Perform fresh GitHub authorization; do not fall back to a PAT. |

`passlet approver audit --limit 20` shows recent events in a readable format. Add `--json` to output the complete structured records for scripts. An agent using a remote endpoint cannot fetch the Mac's audit history through the transport. The Approver stores its audit log at `~/.local/share/passlet/audit.sqlite`. The audit command reads that file and never creates it. Keep support excerpts limited to needed metadata, even though credentials are excluded.

Corrupted entries are skipped with a warning on stderr; stdout remains JSON. The
limit bounds the recent rows inspected, so corruption can result in fewer entries.
An unreadable database still reports an error.

## Repository groups

Request multiple repositories from the same owner with a comma-separated
`--repos` list. One approval grants access to exactly those repositories, using
the same permissions for each. Different owners require separate grants.

Requests accept up to 100 repositories, subject to transport size limits.
When the repository list exceeds the approval text budget, the last repository
line reads "and X additional repositories". The owner and every permission stay
visible. The token, grant file, and audit record retain the full explicit list;
the abbreviated display does not broaden or reduce access.

## Administrative access

Repository administration uses `--repos owner/repository --permissions administration:write`. Organization permissions require `--org organization`, for example `--org teamniteo --permissions members:write`. Account permissions require `--account your-handle`, for example `--account alice --permissions git_ssh_keys:write`. These three scopes are mutually exclusive. Account requests must match the authenticated GitHub user. Levels are validated per permission, including `admin` where GitHub supports it.

Organization and account requests explicitly approve wide access plus metadata read access to all repositories in that target's installation. They cannot include repository content permissions. The prompt identifies the scope and requested permissions. The intrinsic `metadata: read` permission is omitted from the prompt but retained in the grant, client validation, and audit record. All grants still require exact permission matching, an expiring token, and native approval. Narrow requests remain bounded to fit the native prompt; broad app permissions do not mean an agent can request every permission at once.

`internal/passlet/github_permissions.json` is generated from GitHub's public `app-permissions` OpenAPI schema using `python3 scripts/github-permissions.py path/to/api.github.com.json`. Review the catalog diff and scope classifications before committing updates. The source hash is recorded. Metadata is overridden to read-only; single-file access needs a separate path design, and enterprise-only permissions are excluded. Account permissions are authorized by the user, not an installation's permission map. New organization/account scoped-token combinations require live GitHub validation; API fixtures alone do not establish provider support.

GitHub’s token API schema and app registration accept different permission sets. `manifestExcludedPermissions` records known registration incompatibilities separately; `organization_packages` is omitted from setup and rejected if explicitly requested during setup. Schema generation must not remove these exclusions. The broad defaults still require live registration validation.

### Terminal-led onboarding

`passlet approver github setup` suggests an editable handle from Git's `github.user`, then GitHub CLI account metadata if needed. It never obtains or uses a token from those tools. Press Enter to accept the handle, read the explanation, then press Enter to open GitHub. The terminal also prints a fallback URL. The local page automatically POSTs the manifest to GitHub; create and authorize the app, then install it for an account or organization. Installation verification and Keychain storage proceed automatically after GitHub's callback. Ctrl-C cancels setup and stops the localhost listener. No credentials are saved before verification.

For an existing account, setup unlocks the protected credentials with native approval, starts the same temporary localhost listener, and opens the existing app’s installation page. It waits for GitHub’s callback and verifies the installation without replacing credentials. Recovery after installing while Passlet was stopped remains a separate follow-up scenario. The random OAuth callback is only for the original authorization.

### Reset GitHub credentials

Run `passlet approver github reset` to remove the protected GitHub credential item. Confirm removal with Touch ID. There is no separate terminal confirmation. The GitHub App and installations are not deleted; the command prints the app settings link and instructions for starting setup again. If no credentials exist, reset exits successfully without prompting.

Setup reserves localhost port 17402 before prompting for a handle or opening Keychain. If it is occupied, close the application using the port and retry. Denied authorization, an unexpected GitHub identity, and failed Keychain storage never report completion. Errors include recovery instructions without exposing credentials. Organization approval remains planned in the roadmap until the live callback has been verified.
