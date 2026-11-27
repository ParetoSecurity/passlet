# Passlet

Short-lived GitHub credentials for your coding agents. The agent asks for specific repositories and permissions, you approve on your Mac with Touch ID, and the agent gets a token that expires in hours. Your long-lived credentials stay in your Mac's Keychain.

This plugin adds the Passlet skill to Claude Code. The skill runs Git, `gh`, and GitHub API calls through `passlet exec`, reuses access you already approved, and asks for approval only when it needs more.

## Requirements

- The Passlet CLI, set up with a GitHub App: see [Installation](https://passlet.dev/docs/getting-started/installation/).
- A Mac with macOS 26 or later on Apple Silicon to approve requests.
- Agents can run on the same Mac or on Linux and macOS machines connected over SSH or Tailscale.

Installing the plugin does not mint a token or bypass approval.

## Links

- Documentation: https://passlet.dev/docs/
- Source and issues: https://github.com/ParetoSecurity/passlet
- License: MIT
