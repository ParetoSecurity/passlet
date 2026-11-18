---
name: release-passlet
description: Prepare and publish a Passlet release, validate signed artifacts, and review code and package metadata in one release PR. Use for releasing Passlet or checking release readiness, not for installing it on a user's machine.
---

# Release Passlet

Work from the Passlet repository. A release is installable only after the signed GitHub release artifacts, generated package metadata, and matching plugin manifests are published. The app and both plugins always share the same release version. A merged implementation PR or a successful signing job alone is not enough.

Creating or editing this skill does not authorize a release. For a readiness request, perform checks and report outstanding work. For a release request, establish the intended unused `vX.Y.Z` version and target commit. If publication is not already authorized, prepare the version, exact SHA, release notes, and check results before asking for approval. Do not publish, merge PRs, or activate someone's machine merely to test the workflow.

## Progress reports

Report milestones: candidate checked, artifacts prepared, metadata ready for review, published, and availability verified. Include the relevant link and any action needed from the user. Keep the candidate SHA and outstanding steps in handoffs. Do not repeat the full checklist on every update.

## Sources and access

Read these files from the release candidate, rather than relying on historical release results:

- `docs/RELEASING.md`: signing credentials and environment protection.
- `.github/workflows/release.yml` and `scripts/release.sh`: actual trigger, signing, asset creation, and upload behavior.
- `packaging/README.md` and `scripts/package-release.py`: package metadata generation and validation.

Use `skills/passlet/SKILL.md` for authenticated GitHub operations. Reuse a valid grant for `ParetoSecurity/passlet`. Release inspection needs `contents:read`; workflow status and artifact downloads need `actions:read`. Pushing preparation/final tags and package metadata need `contents:write`; opening the metadata PR needs `pull_requests:write`. Request only permissions required by the authorized work. Request workflow write access only when the authorized release PR changes workflows or an authorized recovery operation requires it.

Pass all authenticated `gh` and Git operations through `passlet exec --grant <grant-path> -- ...`. Never use ambient credentials as a fallback. Keep tokens and signing secrets out of logs and generated files.

## 1. Prepare the candidate

- [ ] Inspect repository status, current releases, and `origin/main`. Preserve unrelated local work. Use an isolated checkout when needed.
- [ ] Select an unused `vX.Y.Z` tag and record the full candidate SHA. Do not reuse a published version, move a release tag, or overwrite release assets.
- [ ] Open a draft release PR in this repository against main, or reuse the existing implementation PR. Confirm it includes the intended Approver, Agent, and package definitions.
- [ ] Write concise, user-facing release notes from changes since the previous published release. Explain what changed for users, including relevant migrations, instead of listing PR titles or internal workflow steps. Include a full changelog link. Commit the notes to `docs/releases/vX.Y.Z.md` in the release PR before pushing the preparation tag. Both workflow phases require this nonempty file, and source verification prevents changing it after preparation.
- [ ] Update user-facing documentation for changes since the previous release, including installation, setup, examples, and migration instructions where applicable. Mark unreleased features with `AddedIn` as described in `docs/authoring.mdx`. Finish these edits before preparing signed artifacts.
- [ ] Confirm the documentation preview build and link checks pass for the candidate. Website design, SEO and PageSpeed audits are outside the release gate. Production PageSpeed checks run monthly through [the ops domain-health audit](https://github.com/teamniteo/ops/tree/main/.agents/skills-local/domain-health).
- [ ] Use the successful CI run for the exact candidate as product-test evidence. Run local checks when developing fixes, not again merely to release the same SHA. Source changes require fresh CI.
- [ ] Verify production signing prerequisites in `docs/RELEASING.md`.
- [ ] Wait for the latest PR CI run for the final candidate SHA to finish successfully. Verify that SHA is still the release PR head before preparing signed artifacts.

The Release workflow has two phases. `vX.Y.Z-prepare` builds and signs the CI-verified head of an open same-repository release PR without publishing. After its generated package metadata is added to that same PR, reviewed, and squash-merged, `vX.Y.Z` publishes those exact prepared artifacts. Preparation checks the exact PR head and passing PR CI; the final tag must be on main. Both phases retain the protected `release` environment. Review the exact preparation commit before approving signing, especially changes to workflows and build scripts. Never move tags, rebuild approved artifacts during publication, or publish an empty release.

## 2. Prepare signed artifacts

- [ ] Confirm authorization to prepare the exact candidate SHA. Show the version, SHA, release PR, and passing CI before requesting any missing authorization.
- [ ] Push the unused `vX.Y.Z-prepare` tag at the reviewed full candidate SHA through the scoped grant.
- [ ] Identify the Release workflow run for that exact tag and SHA. Link the run and verify its tag and SHA.
- [ ] Wait for the preparation run’s protected `release` environment approval, linking the run when user action is needed. Never weaken or bypass environment protection.
- [ ] Wait for preparation CI to finish successfully. It signs, notarizes and staples the macOS app, builds Linux archives, and generates checksums and metadata. It does not create a GitHub release.
- [ ] Download `passlet-release-assets` and `passlet-package-updates` from that successful run into a private task directory. Retain the run ID and candidate SHA.
- [ ] Verify the exact three archives and `passlet_X.Y.Z_SHA256SUMS`, all checksums, the macOS signature, and its stapled ticket.

Apple's `stapler` attaches and validates notarization tickets so Gatekeeper can verify notarization offline. A Nix shell can set `DEVELOPER_DIR` to an SDK that lacks this tool, making `xcrun stapler` report it missing even when Apple Command Line Tools are installed. Check `/Library/Developer/CommandLineTools/usr/bin/stapler` before installing anything. If present, validate the downloaded bundle directly:

```sh
/Library/Developer/CommandLineTools/usr/bin/stapler validate /absolute/path/to/Passlet.app
```

Alternatively, use `env -u DEVELOPER_DIR /usr/bin/xcrun stapler validate /absolute/path/to/Passlet.app` to use the system-selected developer tools. Keep the Nix SDK configuration unchanged. If neither route finds the tool, report local ticket validation as not run and distinguish it from successful CI validation and local `codesign`/Gatekeeper checks.

For a failed run, report the failing stage and any draft or published assets. If the release is already immutable, use a newly authorized version; do not disable immutability or try to reuse the tag. Stop before destructive recovery. Uploads intentionally refuse overwrites, so rerunning a partially uploaded release may fail again. Do not delete assets, retag, or loop retries without an explicit recovery decision.

## 3. Add metadata to the release PR

- [ ] Inspect all five generated files: `Casks/passlet.rb`, `Formula/passlet-agent.rb`, `packaging/release.json`, `plugin.json`, and `.claude-plugin/plugin.json`. Verify exact archive URLs, hashes, matching versions, and the preparation run ID and SHA.
- [ ] Wait for the Release metadata workflow to add the generated files and docs labels to the same PR and dispatch CI. If the PR head moved, prepare the new candidate rather than overwriting changes. For recovery, regenerate with `python3 scripts/package-release.py X.Y.Z <archives> <output> --run-id <run-id> --source-sha <candidate-sha>`, inspect and commit the result to the same PR.
- [ ] Confirm metadata CI verifies preparation provenance, unchanged source, archive checksums and exact generated metadata. It reuses the preparation's product-test evidence, without running the platform matrix again. Any source change takes the full CI path.
- [ ] Update the existing PR to describe the implementation and release together. Keep one final commit where practical.
- [ ] Wait for the latest PR CI run for the updated metadata commit to finish successfully.
- [ ] Obtain code and generated metadata review and confirm merge/publication authorization, requesting only approvals not already given.
- [ ] Squash-merge the reviewed release PR immediately before final publication.
- [ ] After merge, verify only these five generated metadata files and exact generated docs release-label replacements differ between the prepared source tree and intended final tag tree. The preparation SHA need not be an ancestor after a squash merge. Other source changes require a new preparation, never an unverified rebuild or moved tag.

The metadata URLs refer to the pending release. Merge immediately before final publication; do not deploy this intermediate main revision while its URLs are unavailable.

Include generated `docs/` files from `passlet-package-updates` alongside the package metadata. These contain exact `AddedIn` release-label replacements. Do not edit them independently after preparation. Verify the website rebuild after publication, including the new version labels.

## 4. Publish the final tag

- [ ] With publication authorized and the reviewed release PR squash-merged, push the unused `vX.Y.Z` tag at that merged commit on main. Do not create the release manually or reuse the preparation tag as the final tag.
- [ ] Identify and link the exact publication workflow run.
- [ ] Wait for the publication run’s protected `release` environment approval, linking the run when user action is needed. The publication job checks the recorded successful preparation run, exact source-tree equality apart from generated metadata and docs release-label replacements, checksums, and exact regenerated metadata before uploading anything.
- [ ] Wait for publication CI to finish successfully, including draft creation, upload, and publication. This phase downloads the already signed artifacts and never rebuilds them. If preparation artifacts have expired, stop and prepare again with freshly reviewed metadata. If a draft or upload fails, inspect it before choosing recovery. Do not delete artifacts or blindly retry a partially uploaded release.
- [ ] Verify the published three archives and checksum file match the approved preparation artifacts. The workflow sets the reviewed notes from `docs/releases/vX.Y.Z.md` when creating the draft, before uploading assets and publishing the immutable release. Read back the published notes and compare them with that file.

## 5. Verify availability

- [ ] Verify the final tag and main contain the intended package metadata and both plugin manifests at the release version.
- [ ] Build the real published package with `nix build .#packages.aarch64-darwin.default --no-link` (or the corresponding supported system). Verify `passlet --version` and that the macOS bundle retains its signature. The preparation run's `linux-archives` jobs already ran both Linux archives and the amd64 archive end to end; link them as the Linux Agent evidence. Linux has no service lifecycle to check.
- [ ] Confirm the final `github:ParetoSecurity/passlet/vX.Y.Z` input resolves to the same release version. Consumers may pin this tag and update their lock file. Do not activate user machines unless requested.
- [ ] Wait for the documentation website rebuild to finish successfully and verify the published release labels.

## 6. Hand off

- [ ] Report preparation and final SHAs, release URL, workflow results, release PR state, checks performed, and any remaining blockers. Do not call the release complete before its package/plugin metadata and installation have been verified.

If work stops awaiting environment approval or metadata review, preserve those exact next steps.
