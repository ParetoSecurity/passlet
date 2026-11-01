# Setup and troubleshooting

Use the signed v0.2 bundle on each approving Mac and the matching CLI on the agent. No machine-wide daemon or root service is needed on the Mac. Setup, approval, and login startup run as the logged-in person.

## GitHub onboarding

Run `passlet github setup` on the Mac and enter your GitHub handle, or use `passlet github setup --github-user alice`. The app name is pre-filled as `passlet-alice` and remains editable on GitHub. Long handles are shortened to fit GitHub’s 34-character app-name limit. Setup checks for an existing app with that name before opening the browser and stops with a settings link if found. GitHub still validates names during creation. The loopback listener runs until setup completes or the setup process stops. It uses independent state values for manifest conversion and OAuth, a random callback path, and PKCE. Browser confirmation is required for app registration, installation, and user authorization. The app belongs to that person; install it in any organizations where it should operate. Installing an app does not grant the user access they did not already have.

After app creation and authorization, GitHub's authorization callback redirects the same browser tab to the installation page. Choose **All repositories** for the account or organization. The full default permission set requires an organization owner to approve installation on an organization. GitHub redirects to `http://127.0.0.1:45821/github/installed`; Passlet verifies that installation's app, user, permissions, and accessible repositories before saving credentials and showing success. The listener exists only during setup. If the port is occupied, setup stops before opening the browser; it never silently changes ports. The predictable installation callback is a signal to verify access, not proof of authorization. Manifest conversion and OAuth retain separate random state values, session paths, and PKCE. If GitHub rejects saved authorization, setup directs you to reset and configure again.

If organization approval is pending, ask its administrator to approve the installation and permissions. If setup fails before saving credentials, delete the incomplete personal app from GitHub's Developer settings before restarting. Do not copy callback URLs, OAuth codes, or secret responses into support reports. If Keychain already contains credentials, setup asks for Touch ID/password to read the configured app identity and opens GitHub’s installation page without replacing credentials. Use `passlet github reset` to deliberately remove local credentials before starting over; protected items may not appear in Keychain Access.

App/account identity and tokens are stored together in one protected Keychain item. There is no separate metadata file. Noninteractive status checks do not unlock credentials or display account identity.


## Login startup

[Homebrew or Home Manager](INSTALLATION.md) installs the per-user LaunchAgent and manages updates and removal. It starts at graphical login and restarts failures with a ten-second throttle. Startup never runs provider setup or unlocks Keychain. Do not edit the generated plist or install the service through another manager.

Run `passlet status` or `passlet doctor` to check availability. For launchd details use `launchctl print gui/$(id -u)/dev.passlet.agent`. If startup fails, verify you are in the logged-in Mac's graphical session and check this output for its last exit status. Reinstall the Homebrew cask or reactivate Home Manager after correcting the problem.

To troubleshoot in the foreground, stop the managed service with `launchctl bootout gui/$(id -u)/dev.passlet.agent`, then run `passlet serve`. This is a temporary diagnostic command, not an installation method. Reinstall/reactivate the package when finished. A private lock prevents two approvers from replacing each other's active socket. Never delete a live socket or its lock to work around an error.

Uninstalling through the package manager removes startup while retaining credentials and audit history. Sleep temporarily makes the Mac unavailable; delivered tokens remain usable.

## SSH server configuration

Use a separate remote Unix account per person and a mode-0700 directory for each forwarded socket. The server's sshd must allow stream-local forwarding. An administrator can scope these settings to the intended users:

```text
Match User alice,bob
    AllowStreamLocalForwarding yes
    StreamLocalBindMask 0177
    StreamLocalBindUnlink yes
```

Validate sshd configuration before reloading it and retain an existing admin session. `StreamLocalBindUnlink` on the server allows a new tunnel to replace its stale remote socket after disconnect. The SSH client's setting alone does not configure remote sshd. When the server cannot enable it, inspect the old tunnel and remove only your own stale forwarded socket before reconnecting. Replacing a live forward would interrupt requests.

Alice runs this from her Mac:

```sh
ssh -NT -o ExitOnForwardFailure=yes \
  -R /home/alice/.local/share/passlet/mac.sock:"$HOME/.local/share/passlet/passlet.sock" alice@remote-box
```

Bob uses `/home/bob/.local/share/passlet/mac.sock` and `bob@remote-box` from his Mac. Each remote account sets its matching `PASSLET_ENDPOINT`. The paths are absolute, not `~`; Unix socket paths must be short enough for the platform (Passlet limits its local listener to 103 bytes). No new TCP listener is opened by this transport.

Unix permissions isolate Alice from Bob, but not either from remote root or other processes sharing their account. Never describe this as attesting the agent executable. Root can request approval through either socket and read delivered tokens. A compromised same-user process can also request approval; examine the scopes in the native prompt.

## Tailscale

For Home Manager, set `programs.passlet.service.tailscaleListen = "127.0.0.1:8484"` and reactivate. For Homebrew, create `~/.config/passlet/serve.json` containing `{"tailscale_listen":"127.0.0.1:8484"}`, then reinstall the cask to restart it. The configuration file accepts only this setting; it contains no credentials. Home Manager passes an explicit setting so its Nix configuration takes precedence over this file. An empty setting disables TCP. For foreground diagnostics, use `passlet serve --tailscale-listen 127.0.0.1:8484`.

Then use Tailscale Serve's TLS-terminated TCP forwarding with PROXY protocol v2. Do not point HTTP reverse proxy mode at this listener. The local socket remains available and both transports share one active-approval limit.

Use the Mac's actual `.ts.net` HTTPS hostname and port. WhoIs must be accessible to the serving process on the package-managed service PATH. Tailscale network policy controls which nodes can connect. A caller-provided hostname, description, or HTTP header cannot replace WhoIs identity. A shared server's node identity does not distinguish its Unix users; prefer per-user SSH sockets for that case.

## Diagnostics

`passlet doctor` checks the selected endpoint without minting a token. On the approving Mac's default endpoint it additionally checks directory permissions, helper availability and Keychain item status without allowing UI. A protected or unavailable result means the item cannot be confirmed without authentication; it is not proof of a working credential. A healthy socket only establishes that the approver is listening. GitHub authorization is verified during setup and real requests, not by an unattended diagnostic.

Common failures:

| Failure | Next step |
| --- | --- |
| Approver unavailable | Start the Mac login service; for SSH verify the tunnel and remote socket path; for HTTPS verify Serve and network policy. |
| Directory is not private | Check ownership and set the socket directory to 0700. Do not share it between users. |
| Helper unavailable | Use the signed app bundle and keep CLI/helper together. |
| GitHub setup missing | Run `passlet github setup` on the approving Mac. |
| Repository unavailable | Check the app installation, pending organization approvals, and the user's repository access. |
| GitHub denies permission | Confirm the operation and app's maximum permissions; request only the specific missing scope. |
| Approver busy | Complete or cancel the existing request; do not poll or retry automatically. |
| Approval timeout/disconnect | Restore connectivity and make a new explicit request when the user is ready. |
| Authorization expired/invalid | Perform fresh GitHub authorization; do not fall back to a PAT. |

`passlet audit --limit 20` reads recent structured records from the Mac. An agent using a remote endpoint cannot fetch the Mac's audit history through the transport. Use `--path /absolute/path/audit.sqlite` for another existing database; the command never creates one. Keep support excerpts limited to needed metadata, even though credentials are excluded.

Corrupted entries are skipped with a warning on stderr; stdout remains JSON. The
limit bounds the recent rows inspected, so corruption can result in fewer entries.
An unreadable database still reports an error.

## Administrative access

Repository administration uses `--repos owner/repository --permissions administration:write`. Organization permissions require `--org organization`, for example `--org teamniteo --permissions members:write`. Account permissions require `--account your-handle`, for example `--account alice --permissions git_ssh_keys:write`. These three scopes are mutually exclusive. Account requests must match the authenticated GitHub user. Levels are validated per permission, including `admin` where GitHub supports it.

Organization and account requests explicitly approve wide access plus metadata read access to all repositories in that target's installation. They cannot include repository content permissions. The prompt, delivered grant, client validation, and audit record retain the scope. All grants still require exact permission matching, an expiring token, and native approval. Narrow requests remain bounded to fit the native prompt; broad app permissions do not mean an agent can request every permission at once.

`internal/passlet/github_permissions.json` is generated from GitHub's public `app-permissions` OpenAPI schema using `python3 scripts/github-permissions.py path/to/api.github.com.json`. Review the catalog diff and scope classifications before committing updates. The source hash is recorded. Metadata is overridden to read-only; single-file access needs a separate path design, and enterprise-only permissions are excluded. Account permissions are authorized by the user, not an installation's permission map. New organization/account scoped-token combinations require live GitHub validation; API fixtures alone do not establish provider support.

GitHub’s token API schema and app registration accept different permission sets. `manifestExcludedPermissions` records known registration incompatibilities separately; `organization_packages` is omitted from setup and rejected if explicitly requested during setup. Schema generation must not remove these exclusions. The broad defaults still require live registration validation.

### Terminal-led onboarding

`passlet github setup` suggests an editable handle from Git's `github.user`, then GitHub CLI account metadata if needed. It never obtains or uses a token from those tools. Press Enter to accept the handle, read the explanation, then press Enter to open GitHub. The terminal also prints a fallback URL. The local page automatically POSTs the manifest to GitHub; create and authorize the app, then install it for an account or organization. Installation verification and Keychain storage proceed automatically after GitHub's callback. Ctrl-C cancels setup and stops the localhost listener. No credentials are saved before verification.

For an existing account, setup unlocks the protected credentials with native approval, starts the same temporary localhost listener, and opens the existing app’s installation page. It waits for GitHub’s callback and verifies the installation without replacing credentials. Recovery after installing while Passlet was stopped remains a separate follow-up scenario. The random OAuth callback is only for the original authorization.

### Reset GitHub credentials

Run `passlet github reset` to remove the protected GitHub credential item. Confirmation defaults to no. Confirming requires native approval before deletion. The GitHub App and installations are not deleted; the command prints the app settings link and instructions for starting setup again. If no credentials exist, reset exits successfully without prompting.

Setup reserves localhost port 45821 before prompting for a handle or opening Keychain. If it is occupied, close the application using the port and retry. Denied authorization, an unexpected GitHub identity, and failed Keychain storage never report completion. Errors include recovery instructions without exposing credentials. Organization approval remains planned and is excluded from automated BDD execution until the live callback has been verified.
