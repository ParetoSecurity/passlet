# v0.2 validation

## Automated checks

In-process deadline tests use Go's `testing/synctest` fake clock. Socket and
Swift-helper process tests retain real deadlines. HTTP fixtures use the standard
`net/http/httptest` package.

`make check` runs golangci-lint from the pinned Nix environment. Run it separately
with `make lint-go`, or format Go code with `golangci-lint fmt ./assets/... ./cmd/... ./internal/...`.
The standard analyzers are enabled; `errcheck` excludes test fixtures, while the
other analyzers check both production and test code.

`make check` enforces 100% Go statement coverage per file and overall on each CI
platform. Cross-package coverage includes library code exercised by CLI tests;
local programs under `build/` are excluded. The threshold lives in
`.testcoverage.yml` and is enforced by `go-test-coverage`. This measures Go statements,
not branches or Swift code. Inspect uncovered lines with:

```sh
go tool cover -html=build/coverage.txt
```

Coverage exclusions use `// coverage-ignore: explanation` on the opening line of
a block or function. An explanation is required. These affect `go-test-coverage`'s
report and threshold; Go's raw profile and HTML report retain the excluded code.
Exclusions are limited to native terminal/browser adapters, process-exit wiring,
and defensive OS/resource-failure paths that cannot be induced deterministically
without disrupting the test process. Each exclusion explains its reason beside
the code. These paths remain in the raw report; 100% here means all non-excluded
statements, not every possible execution path or native UI behavior.

GitHub setup scenarios live in `cmd/passlet/features/*.feature`. Godog runs the
implemented scenarios against the real Go CLI, with test-only GitHub, browser,
and Swift-helper fixtures. Run them with:

```sh
go test -v -run '^TestGitHubSetupFeatures$' ./cmd/passlet
```

They also run in `make check` and CI. Linux scenarios verify that setup stops before asking for a handle, opening a browser or callback listener, or invoking the Keychain helper. Organization-owner approval is tracked in `docs/ROADMAP.md` until its live callback has been verified. These fixtures do not test real Touch ID or macOS Keychain protection.

Access scenarios in `cmd/passlet/features/access.feature` run the CLI in subprocesses against a real Approver over a private Unix socket, a fake approval helper, and a local GitHub API fixture. They cover saved grants, offline reuse, process substitution with preserved stdin, denied approval, and expired grants. The fixture checks provider authentication and requested scope, and the scenarios check credential delivery without exposing tokens in terminal output. Run them with `go test -v -run '^TestAccessFeatures$' ./cmd/passlet`. The NixOS suite separately exercises packaged commands through real SSH tunnels, Git, and gh.

Doctor scenarios in `cmd/passlet/features/doctor.feature` assert complete terminal output for successful verification, a stopped local service, missing or stale forwarded sockets, unhealthy forwarding, denied approval, and missing credentials. They use the real CLI dispatcher, Approver, and helper protocol with a local GitHub fixture, and verify that diagnostics never request a grant. Run them with `go test -v -run '^TestDoctorFeatures$' ./cmd/passlet`.

Inside `nix-shell`, run `make check-all`. On macOS also run `make check-macos`. The latter compiles the native helper for both Mac architectures, tests credential validation and lifecycle/cancellation/rotation state, runs the compiled helper against malformed and out-of-order pipe requests, and builds the UI smoke runner. The process tests exercise invalid imports, invalid prompts, store/delete before approval, non-pipe stdin, EOF, and duplicate error suppression. They never send a valid approval request or import real credentials. `./scripts/check-macos.sh --tests-only` compiles the helper for the current architecture and runs the same tests in macOS CI. Go tests cover prompt/scoping logic, provider responses, browser setup state and PKCE, identity, Unix sockets, cancellation, diagnostics, and credential handling.

Run `make integration` on a Linux KVM machine. On Apple Silicon with the configured aarch64 Linux KVM builder:

```sh
nix-build nix/tests.nix --argstr system aarch64-linux --no-out-link
```

The test uses three isolated NixOS VMs: Alice and Bob's approvers plus a shared remote host. Real SSH reverse socket forwarding, Unix users, Git HTTP and gh operate against a test-only GitHub/approval fixture. It covers routing, concurrent independent requests, cross-user socket/file denial, read/write and repository scoping, busy/denied requests, cancellation, crash recovery, audit redaction, token use after tunnel closure, and provider expiry. Fixture keys and certificates are synthetic Nix build outputs, not production secrets. No fixture flags exist in the shipped CLI or Swift helper.

Approval text scenarios in `cmd/passlet/features/approval.feature` run the real request CLI and Approver against a helper fixture. They assert the complete approval message sent over the helper pipe, including local, SSH, and Tailscale paired-key labels, repository, organization, and account scopes, long repository lists, and permissions. The expected text includes the English macOS prefix `Passlet is trying to ` and final period. The test adds that documented wrapper to the captured helper message for comparison, it does not inspect native rendering or verify other system languages. The fixture always denies the request, so these tests do not open Touch ID, access Keychain, or issue a token. macOS controls the surrounding dialog title, buttons, and rendering; those remain part of the native UI checks. Run these scenarios with `go test -v -run '^TestApprovalFeatures$' ./cmd/passlet`.

Command-help scenarios in `cmd/passlet/features/help.feature` assert the full output for each command group and every command. The fixture supplies invalid service configuration and an invalid endpoint, checks successful exit, and rejects creation of local service state. GitHub setup/reset help is available on Linux, while those operations still require a Mac. Run the scenarios with `go test -v -run '^TestHelpFeatures$' ./cmd/passlet`.

## Native UI smoke

Run from a logged-in Mac desktop with the signed test app available:

```sh
make check-macos
build/ui-smoke /absolute/path/Passlet.app/Contents/MacOS/PassletHelper
```

The runner requires Accessibility access for its launching application/process and system permission to inspect the authentication dialog. It does not request permission automatically. Exit 77 means skipped because permission/UI inspection is unavailable; it is not a pass. Exit 0 means the marked native prompt appeared and disappeared after closing the helper's input. The runner never approves authentication or reads a credential value. It uses a distinct smoke-test prompt and leaves real biometric approval to the human checklist. Do not authenticate a smoke-test prompt.

GitHub-hosted macOS runners run the Swift validation, state-machine, and helper process tests. They do not establish real Touch ID, protected Keychain, login startup, or sleep/wake behavior.

## Prepare the development app

From this checkout, enter `nix-shell`, set `PASSLET_PROVISIONING_PROFILE` to your explicit development profile and `PASSLET_SIGNING_IDENTITY` to its signing identity fingerprint, then run:

```sh
make macos
export PATH="$PWD/build/Passlet.app/Contents/MacOS:$PATH"
passlet --version
passlet approver github setup
passlet doctor
```

Run onboarding in a dedicated Mac test account if the existing Keychain identity is already configured. The development bundle includes both executables, so no separate `--helper` path is needed. Signing may itself request access to the signing key; this is separate from Passlet's per-token approval. Do not replace the installed production app or remove its credentials to run an isolated test.

## Tomorrow's manual checklist

Use disposable GitHub repositories only for mutation tests. Record each item as pass, fail, or not run, with the OS/app version. Do not paste grant contents or credentials into the report.

1. Build a development-signed v0.2 app with its explicit profile, or use the reviewed signed release candidate. Verify codesign and the expected Keychain identity. Use a dedicated macOS test account for fresh onboarding so the existing production authorization is not overwritten.
2. Run `passlet approver github setup`. Review the browser's app permissions, install on the two disposable repositories, authorize the same GitHub user, and confirm setup reports the verified user and `passlet doctor` requests native approval and verifies the GitHub identity. Confirm no private signing key or credential file was saved by setup. Organization approval handling is planned; do not count it as implemented.
3. Install through Homebrew or enable the Home Manager module, then run `passlet approver doctor`. Confirm no default TCP listener. Log out and back in; confirm service startup and no unsolicited credential prompt. Kill only this test service's Go process and confirm launchd restarts it and the socket works again.
4. Start a request with explicit repositories and permissions and a private `--out` path. Verify the native prompt shows the honest socket caller, complete repositories/permissions, and approximate provider expiry. Approve with Touch ID: exactly one prompt must release the protected credential and produce a scoped token. Repeat using password, then denial. Denial must create no grant.
5. Leave a request unapproved for the 120-second server deadline. Confirm the native prompt disappears and no grant is delivered. Repeat by cancelling the requesting CLI or closing its SSH tunnel. A late interaction must never produce a grant for a cancelled request.
6. On the remote host, set up separate Alice and Bob Unix accounts and separate SSH tunnels from their own Macs. Each user onboards their own GitHub App/account. Request concurrently; each prompt must appear on the correct Mac and GitHub actions must be attributed to the corresponding user. Bob must be unable to connect to Alice's socket or read her grant as an ordinary user. Root is deliberately trusted.
7. Use a read grant for `gh api` and Git clone in disposable repository A. Verify writing fails and repository B content is denied. Use a separately approved write grant to push a disposable branch and create a PR; confirm user attribution. Reuse the grant after closing SSH and taking the Mac offline; Git/gh must continue until expiry without contacting the Mac.
8. Pair local and remote Agents with separate keys. Request over the shared Unix socket and Tailscale TCP forwarding. Verify trusted key labels in approval prompts and key fingerprints in audit events. Confirm unknown keys and wrong Approver pins fail before Touch ID, on both transports. Revoke an Agent with `passlet approver agents revoke <name>` and verify subsequent requests fail without restarting, including on an existing connection. Caller identity headers must have no effect.
9. Put the Mac to sleep, confirm an agent handles unavailability without retries, wake it, and make a fresh explicit request. Verify login service and the chosen transport recover; re-establish SSH explicitly if it closed.
10. Check `passlet approver audit --limit 30`: request, requested/granted scope, approval/denial/cancellation, expiry, and delivery outcomes must be present without tokens. Run `doctor` while Keychain is locked, approve with Touch ID, and confirm it verifies GitHub identity without issuing an agent token. Denial must fail the check.
11. Uninstall through Homebrew or disable Passlet in Home Manager and reactivate and confirm login startup stops while protected credentials and audit history remain. Remove only test grants, disposable branches/PRs, and deliberately created test apps/accounts when finished.

Signing/notarization of the final v0.2 release and its production Keychain behavior remain release-time checks. The prior v0.1 release is not evidence that a new build passed those checks.

## GitHub onboarding

2026-10-31: live browser testing completed personal app creation, manifest permission verification, OAuth authorization, installation/identity verification, and replacement of existing credentials through the installed signed helper after native approval. Replacement used an ignored local test harness; production fresh import and later scoped-token use remain separate checks.

Pending live validation: run `passlet approver github setup` from an interactive terminal with no Passlet credentials saved. Check the prefilled handle is editable. Accept it, read the introduction, and confirm the browser remains closed until Enter. Create and authorize the app, then install it for a test organization with All repositories selected. Verify automatic return to localhost, the installation verification message, Keychain import, and browser/terminal success. Check the fixed Setup URL in GitHub App settings. Deny authorization in a separate run and confirm no credentials are saved. Automated fixtures inspect the auto-submit form/script; actual browser execution and native Keychain protection still require this manual walkthrough.

After setup exits, install the same app in a second test organization or change its selected repositories. GitHub should redirect to the saved localhost Setup URL; when setup is not running, that redirect will fail even though installation succeeds. Verify a scoped request on an installed disposable repository still requires native approval. The failed redirect does not undo the GitHub installation. Automated fixtures do not substitute for these live browser and Keychain checks.

## Package lifecycle

Use a disposable Mac account for both package managers separately. Install a signed release, confirm the CLI is on PATH and the service runs before provider setup, then complete setup with disposable repositories. Confirm one native approval unlocks Keychain. Upgrade through the package manager and verify the running process uses the new version. Log out/in and test crash recovery. Uninstall and verify the LaunchAgent and process are gone while credentials and audit history remain. Repeat after a failed installation and verify errors are actionable. Attempt to enable both managers and verify an ownership conflict is reported.

Automated fixture tests and Home Manager evaluation do not substitute for this signed-release lifecycle test. Record unperformed checks as not run.

For the CLI setup changes, use a disposable app and Keychain item to check `passlet approver github reset`: native approval deletes only Passlet’s credentials, and a second reset exits without prompting. Denying native approval must retain the item. Run setup again after reset and verify the existing app name is detected before the browser opens. These native checks have not been performed by the automated BDD suite.

`TestExecProcessSubstitutionPreservesStdin` runs the real request and exec dispatchers in Bash process substitution against a local Approver HTTP fixture. It verifies that the child receives both the scoped token and its original stdin, and that a failed request prevents the child from running.

Audit scenarios in `cmd/passlet/features/audit.feature` assert complete terminal output for readable events, newest-first ordering, limits, JSON export, empty logs, missing databases, and corrupted-record warnings. Warnings stay on stderr so JSON stdout remains parseable. They use a private SQLite fixture with fixed timestamps and an invalid Approver endpoint. Successful exit is checked after each scenario unless failure is explicitly expected. Run them with `go test -v -run '^TestAuditFeatures$' ./cmd/passlet`.

Pairing tests exercise real TLS 1.3 over TCP and Unix sockets, reject unknown keys and incorrect Approver pins, and replay captured TLS records against a fresh connection to verify rejection before the request handler runs. Enrollment tests also verify one-time code consumption, expiry, denial, concurrent attempts, identity pinning, malformed responses, and revocation on existing connections. Run `go test -run "TestPaired|TestCaptured|TestPairing|TestPrivateKey|TestEnrollment|TestInvitation|TestConcurrentEnrollment" ./internal/passlet`.
