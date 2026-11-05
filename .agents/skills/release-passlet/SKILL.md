---
name: release-passlet
description: Prepare and publish a Passlet release, validate signed artifacts, and deliver the Homebrew/Nix package metadata PR. Use for releasing Passlet or checking release readiness, not for installing it on a user's machine.
---

# Release Passlet

Work from the Passlet repository. A release is installable only after both the signed GitHub release artifacts and the generated package metadata are published. A merged implementation PR or a successful signing job alone is not enough.

Creating or editing this skill does not authorize a release. For a readiness request, perform checks and report outstanding work. For a release request, establish the intended unused `vX.Y.Z` version and target commit. If publication is not already authorized, prepare the version, exact SHA, release notes, and check results before asking for approval. Do not publish, merge PRs, or activate someone's machine merely to test the workflow.

## Sources and access

Read these files from the release candidate, rather than relying on historical release results:

- `docs/RELEASING.md`: signing credentials, environment protection, and production validation.
- `docs/VALIDATION.md`: manual checks for protected Keychain and native approval.
- `.github/workflows/release.yml` and `scripts/release.sh`: actual trigger, signing, asset creation, and upload behavior.
- `packaging/README.md` and `scripts/package-release.py`: package metadata generation and validation.

Use `skills/passlet/SKILL.md` for authenticated GitHub operations. Reuse a valid grant for `ParetoSecurity/passlet`. Release inspection needs `contents:read`; workflow status and artifact downloads need `actions:read`. Publishing a release and pushing package metadata need `contents:write`; opening the metadata PR needs `pull_requests:write`. Request only permissions required by the authorized work. Do not request workflow write access unless an authorized recovery operation actually requires it.

Pass all authenticated `gh` and Git operations through `passlet exec --grant <grant-path> -- ...`. Never use ambient credentials as a fallback. Keep tokens and signing secrets out of logs and generated files.

## 1. Prepare the candidate

- [ ] Inspect repository status, current releases, and `origin/main`. Preserve unrelated local work. Use an isolated checkout when needed.
- [ ] Select an unused `vX.Y.Z` tag and record the full candidate SHA. Do not reuse a published version, move a release tag, or overwrite release assets.
- [ ] Verify the candidate is on `origin/main` and its required CI checks passed. Confirm it includes the intended Approver, Agent, and package definitions.
- [ ] Review release notes against changes since the previous release. Describe supported behavior and migrations accurately. Store multiline notes in a file for `gh --notes-file`.
- [ ] Run applicable checks in `nix-shell`: `make check-all`, `nix-build nix/packaging-tests.nix --no-out-link`, and the relevant macOS/integration checks. Existing successful CI for the exact candidate can supply platform checks that cannot run locally. Distinguish passed, failed, and unperformed checks.
- [ ] Verify production signing and manual validation prerequisites in `docs/RELEASING.md` and `docs/VALIDATION.md`. Do not treat preview signatures, fixtures, or older releases as proof of production Keychain behavior.

The current release workflow accepts `vX.Y.Z` tags whose commits are ancestors of main. Pushing a version tag is the trigger. The workflow uploads signed assets to a draft and publishes it only after the package metadata artifact is saved. Keep release immutability enabled. Never publish an empty release first: assets cannot be added afterward.

## 2. Publish and monitor

- [ ] With publication authorized, create the agreed tag locally at the reviewed full SHA, then push that tag through the scoped grant. Do not create or publish the release manually. The workflow creates a draft with generated notes and publishes it after uploads succeed. Apply the reviewed notes afterward using `gh release edit <tag> --notes-file <notes-file>` through the grant.
- [ ] Identify the Release workflow run for that tag and SHA. Do not accidentally inspect or download artifacts from another run.
- [ ] If the protected `release` environment is waiting for a reviewer, provide the run link and state what approval is pending. Do not weaken protection rules or bypass the reviewer.
- [ ] Monitor through completion. The workflow tests, builds universal macOS binaries, signs with Developer ID, notarizes, staples, checks Gatekeeper, builds Linux archives, and uploads assets.
- [ ] Verify these exact assets exist for the selected version: `passlet_X.Y.Z_macos_universal.zip`, `passlet_X.Y.Z_linux_amd64.tar.gz`, `passlet_X.Y.Z_linux_arm64.tar.gz`, and `passlet_X.Y.Z_SHA256SUMS`.
- [ ] Download the artifacts to a private task directory and verify their SHA-256 checksums. On macOS, extract the bundle and verify its signature and stapled ticket. Complete the applicable production validation before announcing the release.

Apple's `stapler` attaches and validates notarization tickets so Gatekeeper can verify notarization offline. A Nix shell can set `DEVELOPER_DIR` to an SDK that lacks this tool, making `xcrun stapler` report it missing even when Apple Command Line Tools are installed. Check `/Library/Developer/CommandLineTools/usr/bin/stapler` before installing anything. If present, validate the downloaded bundle directly:

```sh
/Library/Developer/CommandLineTools/usr/bin/stapler validate /absolute/path/to/Passlet.app
```

Alternatively, use `env -u DEVELOPER_DIR /usr/bin/xcrun stapler validate /absolute/path/to/Passlet.app` to use the system-selected developer tools. Keep the Nix SDK configuration unchanged. If neither route finds the tool, report local ticket validation as not run and distinguish it from successful CI validation and local `codesign`/Gatekeeper checks.

For a failed run, report the failing stage and any draft or published assets. If the release is already immutable, use a newly authorized version; do not disable immutability or try to reuse the tag. Stop before destructive recovery. Uploads intentionally refuse overwrites, so rerunning a partially uploaded release may fail again. Do not delete assets, retag, or loop retries without an explicit recovery decision.

## 3. Publish package metadata

This is the step that enables Nix and Homebrew installation.

- [ ] Download `passlet-package-updates` from the successful Release workflow run, for example `gh run download <run-id> --repo ParetoSecurity/passlet --name passlet-package-updates --dir <metadata-directory>` through the grant.
- [ ] Inspect the generated `Casks/passlet.rb`, `Formula/passlet-agent.rb`, and `packaging/release.json`. Verify version, exact release URLs, and every SHA-256 hash against the published archives. Do not invent hashes or substitute `latest`, `:no_check`, unsigned previews, or older incompatible binaries.
- [ ] On a branch based on current `origin/main`, copy those three files to their matching repository paths. If the workflow artifact is unavailable, regenerate them from the verified downloaded archives with `python3 scripts/package-release.py X.Y.Z <archive-directory> <output-directory>` inside the development shell.
- [ ] Run `make check` and `nix-build nix/packaging-tests.nix --no-out-link`. Build the real release package with `nix build .#packages.aarch64-darwin.default --no-link` on Apple Silicon, or the corresponding supported system. Verify that the installed CLI reports the selected version and the macOS bundle retains its signature.
- [ ] Exercise Approver installation and service lifecycle in a disposable Mac account as described in `packaging/README.md`. Verify Agent on Linux. Do not replace the maintainer's working installation or credentials as an implicit test.
- [ ] Open a draft package-update PR with the release/run links and validation results. Follow repository commit/PR conventions. Do not merge it without authorization.

The release tag precedes this metadata commit. Consumers must pin a repository revision containing the metadata, such as main after the package-update PR is merged. Pinning the release tag itself may still select missing or previous package metadata.

## 4. Verify availability and hand off

- [ ] After the metadata PR is merged, verify main contains the intended `packaging/release.json` and package definitions.
- [ ] Check that the documented Nix and Homebrew entry points resolve the intended release. For Nix users, `nix flake update passlet` advances their pin before their normal activation command. Do not update or activate user configurations unless requested.
- [ ] Report the tag, candidate SHA, release URL, workflow result, metadata PR state, validation performed, and any remaining blockers. Say explicitly whether the release is installable through the package managers yet.

If work stops awaiting environment approval, metadata review, or manual validation, preserve those exact next steps. Do not call the release complete while package metadata is missing or installation remains unverified.
