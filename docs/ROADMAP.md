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

Private Unix sockets are the default transport. Users can forward their Mac's
socket to a remote server or VM over SSH. Tailscale HTTPS remains an explicit
opt-in. No client pairing or session keys are included in this release.

Support multiple users on Cruncher: each Unix account connects to its owner's
Mac, uses that person's GitHub authorization, and receives approval on that Mac.
Private socket directories isolate Unix accounts; the remote host's root is
trusted. Socket access does not prove that Claude or a particular remote process
initiated a request. Every token request still requires native user-presence
approval, and parent credentials remain in the owner's protected Keychain.

Add `passlet doctor` to diagnose endpoint configuration, socket permissions,
connectivity, helper availability, and GitHub setup. Report actionable fixes
without exposing credentials or minting tokens. Distinguish checks that can run
without authentication from checks requiring user interaction; do not weaken
Keychain protection for diagnostics.

Make onboarding errors actionable, including missing setup, incomplete GitHub
installation or organization approval, and unavailable helper or transport.
Explain the next step without logging secrets or raw credential responses.

Update the Claude skill and setup documentation for both transports and multiple
users. When the Mac is unavailable, agents should continue work that needs no
token and ask the user to bring the Mac online before requesting again. No
automatic retries or queues.

Start Passlet automatically at Mac login and restart it after a crash using a
per-user launch agent. Provide documented enable, disable, and troubleshooting
commands. Run in the logged-in user's session so native approval remains
available; startup must not unlock credentials or bypass user presence.

Add `passlet status` to show service state, configured transport, and configured
GitHub account without revealing tokens or unlocking protected credentials.
Distinguish locally recorded account information from freshly verified GitHub
state.

Add `passlet audit` to inspect the existing local SQLite audit trail: recent
requests, caller identity and transport, requested and granted scopes, approval
decisions, expiry, and delivery failures. Never display credential values.

Validate the release with a complete Alice/Bob onboarding flow using separate
Unix accounts and each person's own Mac and GitHub account. Check correct
approval routing and GitHub attribution, socket isolation, login startup and
crash recovery, and agent use after token delivery with the Mac offline. Keep
automated checks separate from manual approval tests and report unperformed
manual tests explicitly. Prioritize reliable startup, then finish this scope
before adding further v0.2 features.

Ship a NixOS VM integration suite in v0.2, wired into the pinned flake and Linux
CI alongside the Go tests. Use real OpenSSH, Unix accounts, socket forwarding,
and Passlet transport and CLI code on a simulated Cruncher and two Linux hosts
standing in for the Mac-side services. Keep fake GitHub and approval components
isolated in the test harness, with no production approval bypass.

Cover Alice/Bob request routing, cross-user socket and token-file isolation,
concurrent requests, SSH disconnect and timeout cancellation, and continued use
of delivered test tokens after the tunnel closes. These tests do not validate
Touch ID, Keychain protection, launchd, signing, or Mac sleep/wake; retain
separate native macOS validation for those behaviors.

Ship automated macOS helper tests for prompt text and scope formatting,
approval and denial handling, timeouts, and disconnect cancellation. Keep any
simulated authentication isolated in the test harness.

Add UI smoke tests in a logged-in macOS desktop session using the signed app:
trigger a request, verify the native authentication prompt appears with the
expected text where system UI inspection permits it, and verify timeout or
disconnect dismisses it. Document required UI automation permissions and runner
limitations; report unavailable checks as skipped, never passed.

Maintain a manual hardware checklist for genuine Touch ID and password
approval, denial, protected Keychain retrieval using the same authentication
context without a second prompt, and sleep/wake behavior. UI automation must
not bypass user-presence protection or claim to validate biometric approval.

Before v1.0, make breaking changes directly and update documentation and tests
alongside them. Do not add compatibility shims, deprecated aliases, migration
code, or preserve old defaults.

Integrate `teamniteo/agents` conventions using the existing sibling-project
pattern (for example Canario): a non-flake `niteo-agents` input pinned in
`nix/flake.lock`, resolved through the Nix development environment, with upstream
rules and skills linked into the project rather than hardcode-copied. Preserve
Passlet-specific instructions and skills. Include the input in the existing
weekly upstream-update workflow so advancing its pin refreshes the linked
conventions on environment entry. Document this integration and verify that
generated links are not committed or packaged as machine-specific store paths.

Harden the release pipeline in v0.2: review workflow permissions, action pins,
signing credential access, and release publishing permissions. Require agreement
from two distinct maintainers from `zupo`, `dz0ny`, and `am-on` for release and
security changes. A PR author who is one of these three counts toward the quorum
and needs approval from one of the other two. A PR authored by anyone else needs
approvals from two of the three. Protect the review policy itself, dismiss stale
approvals after changes, and ensure release/tag paths cannot bypass reviewed
code. Check GitHub's actual ruleset capabilities rather than assuming a list of
three CODEOWNERS enforces a two-person quorum. Record any administrator bypass
or rule-editing powers that remain; do not claim protection against account
takeover where a single administrator can remove the controls.
