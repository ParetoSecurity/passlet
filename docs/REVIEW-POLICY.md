# GitHub review and release protections

Use GitHub's built-in PR reviews, required CI checks, tag rules, and environment approvals. There is no custom maintainer status check or author-dependent approval count.

## Verified settings, 2026-10-31

The `release` environment requires approval from one of `zupo`, `dz0ny`, or `am-on`. It prevents self-review and disables administrator bypass. Deployment is limited to tags matching `v*`. The user confirmed signing/notarization credentials are environment secrets. Check that no duplicate repository or accessible organization secrets expose those credentials outside this gate.

The rulesets API confirms that [Protect main](https://github.com/ParetoSecurity/passlet/rules/26273079) and [Protect release tags](https://github.com/ParetoSecurity/passlet/rules/26273084) are active, with empty bypass lists. Their configured reviews, required checks, and tag restrictions match the import files below. Classic branch protection cannot be inspected with the scoped Passlet grant. This document does not apply settings automatically.

## Importable rulesets

- [Main branch](../.github/rulesets/main.json)
- [Release tags](../.github/rulesets/release-tags.json)

Import these JSON files through Settings → Rules → Rulesets → New ruleset → Import a ruleset. They contain no repository IDs or credentials. Keep them synchronized with settings changes, including renamed CI checks. GitHub may add default fields when importing. The files describe the intended configuration, while the live ruleset links above show what GitHub currently enforces.

## Main branch settings

Create an active ruleset targeting `main`, with an empty bypass list. Require pull requests, one approving review, code-owner approval, dismissal of stale approvals on new commits, and resolution of conversations. Block force pushes and deletion. Require the current Linux/macOS test checks and NixOS SSH integration. Linux tests include workflow linting and secret scanning. Select GitHub Actions as the expected check source where offered.

The wildcard CODEOWNERS lists the three maintainers and covers workflow, policy, dependency, and signing changes. GitHub treats those owners as alternatives: one owner's approval satisfies code-owner review. A PR author cannot approve their own PR. External authors follow the same rule and need one code-owner approval, not a special second approval. This deliberately replaces the former author-dependent policy with native controls.

Do not require the removed `Maintainer quorum` status. Remove that context from any existing required-check configuration before merging this change so later PRs are not blocked waiting for a deleted workflow.

## Signing and release tags

Keep all signing/notarization credentials only in the protected `release` environment. Required reviewers are `zupo`, `dz0ny`, and `am-on`; one must approve. Enable Prevent self-review and disable Allow administrators to bypass configured protection rules. This gate checks the release initiator, not the PR author, and is separate from PR approval.

Create an active `v*` tag ruleset restricting updates and deletion, with no bypass actors. If tag creation also needs restricting, use a separate creation-only ruleset with the intended release team allowed to create tags, so that permission does not also permit moving or deleting them.

The release workflow verifies that the tag resolves to a main ancestor, builds that exact commit SHA, and checks again before uploading that the tag has not moved. It does not count reviews itself. The environment gate is enforced by GitHub before the signing job starts. Ordinary PR CI has no signing credentials and does not sign or notarize releases.

## Limits and validation

A single code-owner approval is not two approvals. Listing three environment reviewers also requires only one of them. The PR author and release initiator are different identities for these rules, so the result is not an exact two-of-three approval policy across every operation.

Disabling administrator bypass prevents bypassing an approval on an individual run; it does not stop sufficiently privileged administrators or organization owners from editing these settings, workflows, or secret access. Organization-managed rules can restrict repository administrators, but organization owners retain control. Do not describe these settings as protection against every administrator-account compromise.

Verify with a disposable PR that direct pushes are blocked, code-owner approval and CI are required, and a new commit dismisses old approval. Before releasing, verify the reviewer list, self-review setting, bypass setting, tag restrictions, and secret placement in GitHub. Confirm the release initiator cannot approve their own signing job. No signing or publication should be performed merely to test these settings without release authorization.
