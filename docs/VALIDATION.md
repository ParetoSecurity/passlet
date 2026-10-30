# v0.2 validation

## Automated checks

Inside `nix-shell`, run `make check-all`. On macOS also run `make check-macos`. The latter compiles the native helper, runs its lifecycle/cancellation/rotation state tests, and builds the UI smoke runner. Go tests cover prompt/scoping logic, provider responses, browser setup state and PKCE, identity, Unix sockets, cancellation, diagnostics, and credential handling.

Run `make integration` on a Linux KVM machine. On Apple Silicon with the configured aarch64 Linux KVM builder:

```sh
nix-build nix/tests.nix --argstr system aarch64-linux --no-out-link
```

The test uses three isolated NixOS VMs: Alice and Bob's approvers plus a shared remote host. Real SSH reverse socket forwarding, Unix users, Git HTTP and gh operate against a test-only GitHub/approval fixture. It covers routing, concurrent independent requests, cross-user socket/file denial, read/write and repository scoping, busy/denied requests, cancellation, crash recovery, audit redaction, token use after tunnel closure, and provider expiry. Fixture keys and certificates are synthetic Nix build outputs, not production secrets. No fixture flags exist in the shipped CLI or Swift helper.

## Native UI smoke

Run from a logged-in Mac desktop with the signed test app available:

```sh
make check-macos
build/ui-smoke /absolute/path/Passlet.app/Contents/MacOS/PassletHelper
```

The runner requires Accessibility access for its launching application/process and system permission to inspect the authentication dialog. It does not request permission automatically. Exit 77 means skipped because permission/UI inspection is unavailable; it is not a pass. Exit 0 means the marked native prompt appeared and disappeared after closing the helper's input. The runner never approves authentication or reads a credential value. It uses a distinct smoke-test prompt and leaves real biometric approval to the human checklist. Do not authenticate a smoke-test prompt.

GitHub-hosted macOS runners compile these tests and exercise the pure helper state machine. They do not establish real Touch ID, protected Keychain, login startup, or sleep/wake behavior.

## Prepare the development app

From this checkout, enter `nix-shell`, set `PASSLET_PROVISIONING_PROFILE` to your explicit development profile and `PASSLET_SIGNING_IDENTITY` to its signing identity fingerprint, then run:

```sh
make macos
export PATH="$PWD/build/Passlet.app/Contents/MacOS:$PATH"
passlet version
passlet setup github
passlet service install
passlet doctor
```

Run onboarding in a dedicated Mac test account if the existing Keychain identity is already configured. The development bundle includes both executables, so no separate `--helper` path is needed. Signing may itself request access to the signing key; this is separate from Passlet's per-token approval. Do not replace the installed production app or remove its credentials to run an isolated test.

## Tomorrow's manual checklist

Use disposable GitHub repositories only for mutation tests. Record each item as pass, fail, or not run, with the OS/app version. Do not paste grant contents or credentials into the report.

1. Build a development-signed v0.2 app with its explicit profile, or use the reviewed signed release candidate. Verify codesign and the expected Keychain identity. Use a dedicated macOS test account for fresh onboarding so the existing production authorization is not overwritten.
2. Run `passlet setup github`. Review the browser's app permissions, install on the two disposable repositories, authorize the same GitHub user, and confirm `passlet status` reports that locally recorded user. Confirm no private signing key or credential file was saved by setup. Check incomplete/pending organization approval produces an actionable error.
3. Run `passlet service install`, then `passlet doctor`. Confirm no default TCP listener. Log out and back in; confirm service startup and no unsolicited credential prompt. Kill only this test service's Go process and confirm launchd restarts it and the socket works again.
4. Start a request with explicit repositories and permissions and a private `--out` path. Verify the native prompt shows the honest socket caller, complete repositories/permissions, and approximate provider expiry. Approve with Touch ID: exactly one prompt must release the protected credential and produce a scoped token. Repeat using password, then denial. Denial must create no grant.
5. Leave a request unapproved for the 120-second server deadline. Confirm the native prompt disappears and no grant is delivered. Repeat by cancelling the requesting CLI or closing its SSH tunnel. A late interaction must never produce a grant for a cancelled request.
6. On the remote host, set up separate Alice and Bob Unix accounts and separate SSH tunnels from their own Macs. Each user onboards their own GitHub App/account. Request concurrently; each prompt must appear on the correct Mac and GitHub actions must be attributed to the corresponding user. Bob must be unable to connect to Alice's socket or read her grant as an ordinary user. Root is deliberately trusted.
7. Use a read grant for `gh api` and Git clone in disposable repository A. Verify writing fails and repository B content is denied. Use a separately approved write grant to push a disposable branch and create a PR; confirm user attribution. Reuse the grant after closing SSH and taking the Mac offline; Git/gh must continue until expiry without contacting the Mac.
8. Run local Claude on the Mac with no `PASSLET_ENDPOINT`; verify it uses the private socket. Enable opt-in Tailscale with PROXY v2 and verify WhoIs caller text and the same scopes/approval behavior. Confirm neither request bodies nor headers can claim a different caller.
9. Put the Mac to sleep, confirm an agent handles unavailability without retries, wake it, and make a fresh explicit request. Verify login service and the chosen transport recover; re-establish SSH explicitly if it closed.
10. Check `passlet audit --limit 30`: request, requested/granted scope, approval/denial/cancellation, expiry, and delivery outcomes must be present without tokens. Run `status` and `doctor` while Keychain is locked; they must not prompt or claim fresh GitHub verification.
11. Run `passlet service uninstall` and confirm login startup stops while protected credentials and audit history remain. Remove only test grants, disposable branches/PRs, and deliberately created test apps/accounts when finished.

Signing/notarization of the final v0.2 release and its production Keychain behavior remain release-time checks. The prior v0.1 release is not evidence that a new build passed those checks.
