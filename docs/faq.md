# FAQ

## I have `gh` configured to save my token in macOS Keychain, why should I care about Passlet?

On macOS, `gh` retrieves your token by calling `/usr/bin/security`. When you select “Always Allow,” Keychain remembers permission for `/usr/bin/security`, not exclusively for `gh`. Any malicious process running as your user can call that same program to retrieve the token, with its full configured permissions.

Passlet requires Touch ID or your Mac password before accessing your long-lived credentials. The agent receives a short-lived token limited to the repositories and permissions you approve.

Another process under the agent’s Unix account can still read that delivered token, but its access is limited by the approved scope and expiry.

## What about scheduled tasks?

A scheduled task your agent runs every day or every week cannot use Passlet grants because they have a short validity.

For these tasks, create a dedicated [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new). Choose the resource owner that owns the repository, select only the repositories the task needs, and grant only the permissions required for that task. Set an expiration and configure the token through your scheduler’s secret storage.

The task can run without you, and if its token leaks, the damage is limited to the scope you defined for it. Configure this explicitly for the scheduled task, rather than letting an agent fall back to another credential when Passlet approval is unavailable.
