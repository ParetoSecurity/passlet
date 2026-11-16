# FAQ

## I have `gh` configured to save my token in macOS Keychain, why should I care about Passlet?

On macOS, `gh` retrieves your token by calling `/usr/bin/security`. When you select “Always Allow,” Keychain remembers permission for `/usr/bin/security`, not exclusively for `gh`. Any malicious process running as your user can call that same program to retrieve the token, with its full configured permissions.

Passlet requires Touch ID or your Mac password before accessing your long-lived credentials. The agent receives a short-lived token limited to the repositories and permissions you approve.

Another process under the agent’s Unix account can still read that delivered token, but its access is limited by the approved scope and expiry.
