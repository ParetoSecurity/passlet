# Roadmap

## v0.1

GitHub user attribution, explicit repository and permission scopes, native
user-presence approval, Tailscale caller identity, SQLite audit trail, and
independent Git/gh use after delivery. Manual GitHub App onboarding.

Release preparation includes Linux amd64/arm64 CLI archives, a universal macOS
app containing the CLI and helper, Developer ID signing, notarization, checksums,
CI tests, dependency updates, and the Passlet icon.

## v0.2

Guided `passlet setup github`: create a per-user GitHub App using GitHub's manifest
flow, guide installation and user authorization, store credentials directly in
protected Keychain, discard the unused signing key without persisting it, and
verify identity and scope. Browser confirmations and organization approvals stay
explicit. No broker or shared app credentials required.
