---
name: release-passlet
description: Prepare and publish a Passlet release, validate signed artifacts, and review code and package metadata in one release PR. Use for releasing Passlet or checking release readiness, not for installing it on a user's machine.
---

# Release Passlet

Work from the Passlet repository. A release is installable only after the signed GitHub release artifacts, generated package metadata, and matching plugin manifests are published. The app and both plugins always share the same release version. A merged implementation PR or a successful signing job alone is not enough.

Creating or editing this skill does not authorize a release. For a readiness request, perform checks and report outstanding work. For a release request, establish the intended unused `vX.Y.Z` version and target commit. If publication is not already authorized, prepare the version, exact SHA, release notes, and check results before asking for approval. Do not publish, merge PRs, or activate someone's machine merely to test the workflow.

## Sources and access

Read these files from the release candidate, rather than relying on historical release results:

- `docs/RELEASING.md`: signing credentials, environment protection, and production validation.
- `docs/VALIDATION.md`: manual checks for protected Keychain and native approval.
- `.github/workflows/release.yml` and `scripts/release.sh`: actual trigger, signing, asset creation, and upload behavior.
- `packaging/README.md` and `scripts/package-release.py`: package metadata generation and validation.

Use `skills/passlet/SKILL.md` for authenticated GitHub operations. Reuse a valid grant for `ParetoSecurity/passlet`. Release inspection needs `contents:read`; workflow status and artifact downloads need `actions:read`. Pushing preparation/final tags and package metadata need `contents:write`; opening the metadata PR needs `pull_requests:write`. Request only permissions required by the authorized work. Request workflow write access only when the authorized release PR changes workflows or an authorized recovery operation requires it.

Pass all authenticated `gh` and Git operations through `passlet exec --grant <grant-path> -- ...`. Never use ambient credentials as a fallback. Keep tokens and signing secrets out of logs and generated files.

## 1. Prepare the candidate

- [ ] Inspect repository status, current releases, and `origin/main`. Preserve unrelated local work. Use an isolated checkout when needed.
- [ ] Select an unused `vX.Y.Z` tag and record the full candidate SHA. Do not reuse a published version, move a release tag, or overwrite release assets.
- [ ] Open a draft release PR in this repository against main, or reuse the existing implementation PR. Verify the candidate is its current head and its latest PR CI run passed. Confirm it includes the intended Approver, Agent, and package definitions.
- [ ] Write concise, user-facing release notes from changes since the previous published release. Explain what changed for users, including relevant migrations, instead of listing PR titles or internal workflow steps. Include a full changelog link. Commit the notes to `docs/releases/vX.Y.Z.md` in the release PR before pushing the preparation tag. Both workflow phases require this nonempty file, and source verification prevents changing it after preparation.
- [ ] Run applicable checks in `nix-shell`: `make check-all`, `nix-build nix/packaging-tests.nix --no-out-link`, and the relevant macOS/integration checks. Existing successful CI for the exact candidate can supply platform checks that cannot run locally. Distinguish passed, failed, and unperformed checks.
- [ ] Verify production signing and manual validation prerequisites in `docs/RELEASING.md` and `docs/VALIDATION.md`. Do not treat preview signatures, fixtures, or older releases as proof of production Keychain behavior.

The Release workflow has two phases. `vX.Y.Z-prepare` builds and signs the CI-verified head of an open same-repository release PR without publishing. After its generated package metadata is added to that same PR, reviewed, and squash-merged, `vX.Y.Z` publishes those exact prepared artifacts. Preparation checks the exact PR head and passing PR CI; the final tag must be on main. Both phases retain the protected `release` environment. Review the exact preparation commit before approving signing, especially changes to workflows and build scripts. Never move tags, rebuild approved artifacts during publication, or publish an empty release.

## 2. Prepare signed artifacts

- [ ] With release preparation authorized, push the unused `vX.Y.Z-prepare` tag at the reviewed full candidate SHA through the scoped grant.
- [ ] Identify the Release workflow run for that exact tag and SHA. If the protected environment is waiting, provide the run link and explain the approval needed. Never weaken or bypass environment protection.
- [ ] Monitor preparation through completion. It tests, signs, notarizes and staples the macOS app, builds Linux archives, and generates checksums and metadata. It does not create a GitHub release.
- [ ] Download `passlet-release-assets` and `passlet-package-updates` from that successful run into a private task directory. Retain the run ID and candidate SHA.
- [ ] Verify the exact three archives and `passlet_X.Y.Z_SHA256SUMS`, all checksums, the macOS signature, and its stapled ticket. Perform applicable production validation before final publication. Report manual checks not performed; do not substitute previous releases or fixture tests for them.

Apple's `stapler` attaches and validates notarization tickets so Gatekeeper can verify notarization offline. A Nix shell can set `DEVELOPER_DIR` to an SDK that lacks this tool, making `xcrun stapler` report it missing even when Apple Command Line Tools are installed. Check `/Library/Developer/CommandLineTools/usr/bin/stapler` before installing anything. If present, validate the downloaded bundle directly:

```sh
/Library/Developer/CommandLineTools/usr/bin/stapler validate /absolute/path/to/Passlet.app
```

Alternatively, use `env -u DEVELOPER_DIR /usr/bin/xcrun stapler validate /absolute/path/to/Passlet.app` to use the system-selected developer tools. Keep the Nix SDK configuration unchanged. If neither route finds the tool, report local ticket validation as not run and distinguish it from successful CI validation and local `codesign`/Gatekeeper checks.

For a failed run, report the failing stage and any draft or published assets. If the release is already immutable, use a newly authorized version; do not disable immutability or try to reuse the tag. Stop before destructive recovery. Uploads intentionally refuse overwrites, so rerunning a partially uploaded release may fail again. Do not delete assets, retag, or loop retries without an explicit recovery decision.

## 3. Add metadata to the release PR

- [ ] Inspect all five generated files: `Casks/passlet.rb`, `Formula/passlet-agent.rb`, `packaging/release.json`, `plugin.json`, and `.claude-plugin/plugin.json`. Verify exact archive URLs, hashes, matching versions, and the preparation run ID and SHA.
- [ ] Copy them onto the existing release PR branch. Do not open a second metadata PR. If regeneration is necessary, run `python3 scripts/package-release.py X.Y.Z <archives> <output> --run-id <run-id> --source-sha <candidate-sha>` in the development shell. Never invent provenance or hashes.
- [ ] Run `make check` and `nix-build nix/packaging-tests.nix --no-out-link`. Before publication, the final archive URLs are not public, so use the downloaded signed archives for installation validation. Never replace the maintainer's working installation or credentials as an implicit test.
- [ ] Update the existing PR to describe the implementation and release together. Keep one final commit where practical. Have the code and generated metadata reviewed, then squash-merge only with authorization.
- [ ] After merge, verify only these five generated files differ between the prepared source tree and intended final tag tree. The preparation SHA need not be an ancestor after a squash merge. Other source changes require a new preparation, never an unverified rebuild or moved tag.

The metadata URLs refer to the pending release. Merge immediately before final publication; do not deploy this intermediate main revision while its URLs are unavailable.

## 4. Publish the final tag

- [ ] With publication authorized and the reviewed release PR squash-merged, push the unused `vX.Y.Z` tag at that merged commit on main. Do not create the release manually or reuse the preparation tag as the final tag.
- [ ] Identify the exact publication workflow run. Provide its link if environment approval is pending. The publication job checks the recorded successful preparation run, exact source-tree equality apart from generated metadata, checksums, and exact regenerated metadata before uploading anything.
- [ ] Monitor draft creation, upload, and publication. This phase downloads the already signed artifacts and never rebuilds them. If preparation artifacts have expired, stop and prepare again with freshly reviewed metadata. If a draft or upload fails, inspect it before choosing recovery. Do not delete artifacts or blindly retry a partially uploaded release.
- [ ] Verify the published three archives and checksum file match the approved preparation artifacts. The workflow sets the reviewed notes from `docs/releases/vX.Y.Z.md` when creating the draft, before uploading assets and publishing the immutable release. Read back the published notes and compare them with that file.

## 5. Verify availability and hand off

- [ ] Verify the final tag and main contain the intended package metadata and both plugin manifests at the release version.
- [ ] Build the real published package with `nix build .#packages.aarch64-darwin.default --no-link` (or the corresponding supported system). Verify `passlet --version` and that the macOS bundle retains its signature. Verify Linux Agent behavior and applicable production service lifecycle checks, distinguishing tests not run.
- [ ] Confirm the final `github:ParetoSecurity/passlet/vX.Y.Z` input resolves to the same release version. Consumers may pin this tag and update their lock file. Do not activate user machines unless requested.
- [ ] Report preparation and final SHAs, release URL, workflow results, release PR state, checks performed, and any remaining blockers. Do not call the release complete before its package/plugin metadata and installation have been verified.

If work stops awaiting environment approval, metadata review, or manual validation, preserve those exact next steps.
