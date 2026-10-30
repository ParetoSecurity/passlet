# Releasing Passlet

## Conventions

Following pareto-mac, distribution uses Developer ID Application signing,
hardened runtime, notarization and stapling. Following canario/trak/vend and
pareto-linux, CI uses pinned actions, minimal job permissions, concurrency
control, tests, coverage artifacts, and dependency update PRs. Publishing a GitHub release triggers the release workflow, which builds and
attaches signed, notarized artifacts to that release automatically.

Do not copy product-specific Fly deployments, Setapp distribution, Sentry symbol
uploads, or automatic dependency merging into this CLI project.

## One-time repository setup

1. The public repository is `paretosecurity/passlet`, licensed GPL-3.0-only. Do not
   publish development evidence, credentials, provisioning profiles, or builds.
2. Require CI's Linux and macOS test jobs on main; require reviewed PRs. Disable
   force pushes and branch deletion. Enable secret scanning/push protection where
   available. Use a `release` environment restricted to release tags (`v*`). Do not require an environment reviewer if
   signing should start automatically when a release is published.
3. Register an explicit production Apple App ID such as `dev.passlet.app` on the
   signing team. Create a Developer ID provisioning profile authorizing its
   Keychain access group. The existing Mac Development profile is not a release
   credential and cannot be used for distribution.
4. Export the Developer ID Application certificate and its private key as a
   password-protected p12, using the team's approved credential handling process.
   Set environment secrets `APPLICATION_CERTIFICATE_DATA` (base64 p12),
   `APPLICATION_CERTIFICATE_PASSPHRASE`, `PROVISIONING_PROFILE_DATA` (base64 profile),
   `NOTARIZATION_USERNAME`, and `NOTARIZATION_PASSWORD` (Apple app-specific password).
   Set environment variables `PASSLET_SIGNING_IDENTITY` (certificate fingerprint)
   and `APPLE_TEAM_ID`. No GitHub App credentials are needed in CI.
5. The production bundle ID has a separate Keychain access group. Import user
   credentials through the production signed helper; do not silently broaden the
   prototype's Keychain ACL or copy stale bootstrap tokens. Fresh GitHub user
   authorization may be needed. Confirm approval, denial, cancellation, and refresh
   storage using the production identity before release.

## Local checks

`make check` runs module verification, formatting, vet, race tests and coverage.
`make build` builds the local CLI and Linux amd64 CLI. Run these commands inside
`nix-shell`; all CI and release commands use the same `nix/flake.lock`. Apple
SDK, signing, and notarization tools come from the macOS runner.

On macOS, `make macos` builds the development helper using
`PASSLET_PROVISIONING_PROFILE` and `PASSLET_SIGNING_IDENTITY`. The default keeps
the existing prototype identity. The icon source is `assets/passlet.png`;
`scripts/icon.sh` creates all required icon sizes and the icns bundle.

## Prepare and validate v0.1

After CI passes on main, publish a GitHub release tagged `v0.1.0`.
The **Release** workflow checks out the release tag, repeats tests, builds
universal arm64/x86_64 macOS binaries, verifies Developer ID signing, notarizes,
staples, assesses Gatekeeper, builds Linux amd64/arm64 archives and SHA-256 sums,
and uploads the artifacts to that existing release. Uploads refuse to overwrite
existing assets. The release will be visible before its assets finish building.

Download the release artifacts and verify checksums. Extract the macOS archive and
move Passlet.app to /Applications. The license, README and Claude skill are included in the signed app resources
and Linux archives. The CLI is inside the signed bundle at
`/Applications/Passlet.app/Contents/MacOS/passlet`; place a symlink to it in your
PATH. Use `/Applications/Passlet.app/Contents/MacOS/PassletHelper` for --helper.
Do not move the helper executable outside its app bundle.

On a clean Mac, verify Gatekeeper acceptance and the real protected-Keychain
approval/denial/cancellation flow. On Linux, verify `passlet version`, then the
Git/gh workflows and scope denials against disposable repositories. Test both
architectures on actual hardware where possible; cross-compilation is not a
runtime test. Confirm Mac-offline use after token delivery.

Run production signing/Keychain validation before announcing the release.
Publishing a release is the explicit trigger; signing and asset attachment then
run automatically, following the Pareto macOS distribution model. Keep
GitHub parent credentials, refresh tokens, and app client secrets out of all CI
secrets, artifacts, logs, and screenshots.

## Current release gates

The workflow and scripts are preparatory until executed in GitHub Actions with
real distribution credentials. A successful local build is not proof of
notarization, Gatekeeper acceptance, or production Keychain behavior. Signing credential configuration, production profile,
CI execution, real distribution tests, and publishing must be recorded separately.

## Local preparation results

- Go module verification, vet and race tests passed; internal package statement
  coverage was 77.4%. CLI entry-point coverage is currently 0%; existing workflow
  tests exercise the underlying client/transport/credential components.
- Five signing-metadata tests passed, including rejection of development,
  expired, wildcard and prototype profiles for production releases.
- actionlint and shellcheck passed. Redacted gitleaks scans of cmd, internal,
  macos, scripts, skills and .github found no secrets. Local ignored bootstrap
  credentials still exist and must never be added to Git.
- The arm64 development helper builds, embeds the icon, and passes codesign
  verification with the existing development identity.
- Linux arm64 CLI, macOS Intel CLI, and macOS Intel Swift helper cross-builds
  passed. They have not been runtime-tested on those architectures.
- Developer ID signing, notarization and production Keychain tests remain release
  gates. Local passing tests do not establish those outcomes.

For the packaged Claude skill, configure `PASSLET_ENDPOINT` on the agent machine
to the approving user's Mac before starting Claude. Existing installed personal
skills are not modified by editing the release source.
