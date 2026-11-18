# What is Passlet?

Passlet gives coding agents short-lived, scoped GitHub access after you approve a request on your Mac. Choose the repositories and permissions for each request instead of handing an agent a credential with permanent access.

## Agent and Approver

- Approver runs on your Mac, keeps the long-lived GitHub credentials in the protected Keychain, and asks you to approve requests.
- Agent runs alongside your coding tools, on the same Mac or on another macOS or Linux machine. It requests access and passes the approved credentials to Git and `gh`.

Agent and Approver authenticate each other using paired keys. They can connect locally, through SSH forwarding, or over Tailscale.

## What approval grants

The agent receives an expiring GitHub token scoped to the requested repositories and permissions. GitHub operations retain your user attribution. Once delivered, the token can be used until it expires, even if your Mac goes offline.

Issuing a new credential needs your approval. Sessions under the same OS user can reuse an unexpired grant from the Agent’s private file cache, including while the Approver is offline. Use `request --no-cache` to bypass cache lookup and storage. Passlet does not provide unattended approval or grant GitHub access you do not already have.

## Get started

Follow the [Quick start](quick-start.mdx) to try Passlet on your Mac, or read [How Passlet works](overview.mdx) for the credential flow.
