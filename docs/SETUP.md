# Setup and troubleshooting

Use the signed v0.2 bundle on each approving Mac and the matching CLI on the agent. No machine-wide daemon or root service is needed on the Mac. Setup, approval, and login startup run as the logged-in person.

## GitHub onboarding

Run `passlet setup github` on the Mac. The loopback browser session expires after ten minutes. It uses independent state values for manifest conversion and OAuth, a random callback path, and PKCE. Browser confirmation is required for app registration, installation, and user authorization. The app belongs to that person; install it in any organizations where it should operate. Installing an app does not grant the user access they did not already have.

If organization approval is pending, ask its administrator to approve the installation and permissions. If setup fails before saving credentials, delete the incomplete personal app from GitHub's Developer settings before restarting. Do not copy callback URLs, OAuth codes, or secret responses into support reports. If Keychain already contains credentials, setup refuses to overwrite them. Use Keychain Access to remove only the Passlet item when deliberately resetting an account, revoke the old GitHub authorization, and perform fresh setup. This is an explicit reset, not an automatic migration.

The local `account.json` contains only the last verified login, app ID/URL, and verification time. It is informational; editing it cannot change authorization. `status` does not claim these details are currently verified with GitHub. If metadata cannot be saved after Keychain import, credentials remain protected; correct the local filesystem problem and reset deliberately rather than repeatedly running setup.

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

`passlet doctor` checks the selected endpoint without minting a token. On the approving Mac's default endpoint it additionally checks directory permissions, helper availability, locally saved account metadata, and Keychain item status without allowing UI. A protected or unavailable result means the item cannot be confirmed without authentication; it is not proof of a working credential. A healthy socket only establishes that the approver is listening. GitHub authorization is verified during setup and real requests, not by an unattended diagnostic.

Common failures:

| Failure | Next step |
| --- | --- |
| Approver unavailable | Start the Mac login service; for SSH verify the tunnel and remote socket path; for HTTPS verify Serve and network policy. |
| Directory is not private | Check ownership and set the socket directory to 0700. Do not share it between users. |
| Helper unavailable | Use the signed app bundle and keep CLI/helper together. |
| GitHub setup missing | Run `passlet setup github` on the approving Mac. |
| Repository unavailable | Check the app installation, pending organization approvals, and the user's repository access. |
| GitHub denies permission | Confirm the operation and app's maximum permissions; request only the specific missing scope. |
| Approver busy | Complete or cancel the existing request; do not poll or retry automatically. |
| Approval timeout/disconnect | Restore connectivity and make a new explicit request when the user is ready. |
| Authorization expired/invalid | Perform fresh GitHub authorization; do not fall back to a PAT. |

`passlet audit --limit 20` reads recent structured records from the Mac. An agent using a remote endpoint cannot fetch the Mac's audit history through the transport. Use `--path /absolute/path/audit.sqlite` for another existing database; the command never creates one. Keep support excerpts limited to needed metadata, even though credentials are excluded.
