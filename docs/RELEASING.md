# Releasing Passlet

Use the repository-local [release-passlet skill](../.agents/skills/release-passlet/SKILL.md) for the release checklist, including package metadata publication.

## Conventions

Following pareto-mac, distribution uses Developer ID Application signing,
hardened runtime, notarization and stapling. Following canario/trak/vend and
pareto-linux, CI uses pinned actions, minimal job permissions, concurrency
control, tests, coverage artifacts, and dependency update PRs. Pushing a version tag triggers the release workflow. It builds and signs the artifacts, creates a draft release, uploads the complete asset set and package metadata, then publishes the release. Keep release immutability enabled.

Do not copy product-specific Fly deployments, Setapp distribution, Sentry symbol
uploads, or automatic dependency merging into this CLI project.

## One-time repository setup

1. The public repository is `paretosecurity/passlet`, licensed MIT. Do not
   publish development evidence, credentials, provisioning profiles, or builds.
2. Require CI's Linux and macOS test jobs on main; require reviewed PRs. Disable
   force pushes and branch deletion. Enable secret scanning/push protection where
   available. Use a `release` environment restricted to release tags (`v*`). Review the v0.2 [maintainer-policy settings proposal](REVIEW-POLICY.md) before enabling release signing. Repository settings have not been applied by this branch.
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

## Prepare and validate a release

After CI passes on main, push an unused `vX.Y.Z` tag pointing to the exact reviewed commit. Do not publish an empty release in the GitHub UI: immutable releases cannot accept assets after publication.

The Release workflow verifies that the tag points to a commit on main and passes that exact SHA to the signing job. Its verification job uses the runner's Git and Python without loading the development environment. GitHub pauses signing for approval under the `release` environment rules before making credentials available. The signing job uses the pinned Nix environment, repeats tests, builds universal macOS binaries, verifies Developer ID signing, notarizes, staples, assesses Gatekeeper, and builds Linux archives and SHA-256 sums.

After checking that the tag still resolves to the verified SHA, the workflow creates a draft release with generated notes, attaches all signed assets, uploads the `passlet-package-updates` workflow artifact, and publishes the draft. Publication makes the assets and tag immutable. Existing releases and assets are not overwritten. If a run fails after draft creation, inspect the draft and uploaded assets before choosing a recovery; a blind rerun will not replace them.

For a published release with missing assets, use a new version. Do not move or reuse an immutable tag or disable immutability to work around the failure. Release titles and notes can still be edited after publication.

Verify the uploaded release artifacts and checksums, then
publish the generated Homebrew/Nix package metadata in a reviewed PR after verifying the assets are available. The release workflow uploads the `passlet-package-updates` artifact for this purpose. See [package maintenance](../packaging/README.md). End users install through Homebrew or Nix/Home Manager, not by extracting archives manually.

Keep the CLI and helper together inside the signed bundle.
`PassletHelper` stays the main executable because macOS accepts the Keychain
entitlement and provisioning profile only there. The bundle contains the CLI and approval helper only.
The build signs the CLI before the outer helper bundle.

On a clean Mac, verify Gatekeeper acceptance and the real protected-Keychain
approval/denial/cancellation flow. On Linux, verify `passlet version`, then the
Git/gh workflows and scope denials against disposable repositories. Test both
architectures on actual hardware where possible; cross-compilation is not a
runtime test. Confirm Mac-offline use after token delivery.

Run production signing/Keychain validation before announcing the release.
Pushing a version tag is the explicit trigger; signing, draft creation, asset attachment, and publication then run automatically. Keep
GitHub parent credentials, refresh tokens, and app client secrets out of all CI
secrets, artifacts, logs, and screenshots.

## Release validation

The v0.1.0 release workflow successfully built, signed, notarized, and attached
artifacts using the configured distribution credentials:
[successful release run](https://github.com/ParetoSecurity/passlet/actions/runs/41110201864).
That historical run used the previous publish-first trigger. The current workflow retains the signing environment, credentials, and Developer ID authority check while publishing only after asset upload. New toolchain changes still need validation
in the release workflow; local checks do not establish production Keychain behavior.

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
- Developer ID signing and notarization passed in the release run linked above.
  Production Keychain interaction is a separate manual check.

For the packaged Claude skill, configure `PASSLET_ENDPOINT` on the agent machine
to the approving user's Mac before starting Claude. Existing installed personal
skills are not modified by editing the release source.

## v0.2 controls

See [REVIEW-POLICY.md](REVIEW-POLICY.md) for built-in PR/release approval settings, protected paths, and remaining administrator limitations. A matching CODEOWNERS list or roadmap statement is not enforcement. PR CI receives no signing credentials or cache-write secret. Release signing remains behind its protected environment. Do not publish v0.2 until the manual [validation checklist](VALIDATION.md) and administrative settings review are complete. The historical v0.1 results above do not establish v0.2 validation.
