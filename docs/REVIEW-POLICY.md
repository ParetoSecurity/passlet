# Maintainer agreement: settings proposal

This is a reviewable settings proposal, not a claim that repository or organization settings have been applied. No live rulesets, environment reviewers, teams, or bypass lists were changed by this implementation.

## Observed settings, 2026-10-31

Read-only inspection with the approved scoped Passlet grant returned no repository rulesets. The `release` environment allows the `v*` tag pattern and reported only a branch-policy protection rule, with no reviewer gate. Classic main branch protection returned HTTP 403 for this grant, so it remains unverified. No alternate credential was used. These observations do not establish administrator bypass permissions, and no setting was changed.

## Decision rule

Two distinct people from `zupo`, `dz0ny`, and `am-on` must agree on release/security changes. A maintainer PR author counts as one; another maintainer must approve the current head commit. An external author needs two maintainer approvals on that commit. Comments, stale approvals, dismissed reviews, and change requests do not count. The checker applies this rule to all PRs into main, avoiding a mutable classification of which changes are security-sensitive.

`scripts/review-quorum.py` implements the decision and has focused tests. The privileged Maintainer review workflow checks out main, never PR code or artifacts, and queries GitHub's authoritative reviews. It runs on PR updates, review-activity completion, an exact `/passlet-review` PR comment, or manual dispatch. Its token can publish statuses but cannot merge or release. A fresh check first changes the status to pending, so an API failure cannot preserve an old success once rechecking starts.

The Review activity workflow is unprivileged and only wakes the trusted workflow. It is not an authority: any wakeup causes a fresh API evaluation. If a fork run needs approval or the event cannot be associated with a PR, a maintainer can comment `/passlet-review` to re-evaluate. Do not treat asynchronous event delivery as an atomic merge-time review check.

## Settings for administrator review

Configure an organization-managed ruleset for main:

- Require pull requests, current CI Linux/macOS checks, workflow/secret checks, NixOS SSH integration, and the `Maintainer quorum` status.
- Require at least one native approving review, dismiss stale approvals on new commits, require code-owner review, and resolve conversations. CODEOWNERS routes to the three maintainers but does not enforce two of them.
- Block force pushes and deletion. Remove routine actor bypasses, including administrator bypass where supported. Restrict who can change Actions permissions, rulesets, repository ownership, collaborators, teams, and environments.
- Protect `.github/**`, `scripts/review-quorum.py`, its tests, `scripts/release.sh`, signing/build scripts, Nix/devenv inputs, and policy documentation under the same rules. The wildcard CODEOWNERS includes those paths; organization controls must prevent replacing the required checker with PR-controlled code.
- Restrict `v*` tag creation to the intended release maintainers or a dedicated release automation identity; prohibit tag updates/deletion. Only reviewed main merge commits are eligible. Keep release-environment signing/notarization credentials out of repository-level and PR-accessible secrets.
- Restrict the `release` environment to release tags and its approved workflow. Consider a separate maintainer environment reviewer with self-review prevention as an additional signing gate. That gate counts the release initiator, not the PR author, so it does not replace the PR quorum rule.

Verify each exact required check name and source after the workflows exist on main. This PR cannot enforce its own newly introduced trusted base policy before it is reviewed and installed. Have two of the named maintainers review this bootstrap change explicitly.

## Enforcement limits requiring an administrative decision

A commit status posted by GitHub Actions is not a tamper-proof policy authority: another workflow with `statuses:write` can imitate its context. Likewise, PR-controlled review notification workflows can be removed, and asynchronous review-dismissal events can race with merging. Native required review counts cannot express “one other maintainer for these authors, two for everyone else.” A list of three CODEOWNERS requires only one of them.

For strict merge-time enforcement, use an organization-controlled required workflow/check integration with a distinct trusted identity, protected outside this repository, and ensure it evaluates current reviews at the merge boundary. If the available GitHub plan cannot enforce that, restrict merges to an audited maintainer-operated process that rechecks the API immediately before merging. This proposal does not silently introduce a new hosted service or claim those controls are active. Until that decision is implemented, the custom status is a useful check with the limitations above, not complete protection against a malicious maintainer workflow.

The release workflow independently re-evaluates quorum before entering the signing job. It checks that the tag resolves to a main ancestor and the merge commit of exactly one reviewed PR, then passes that exact SHA to the build job rather than checking out a mutable tag. It cannot defend against an administrator who changes the workflow, environment, ruleset, or tag controls. A signed artifact proves the configured signing identity, not that no administrator bypass occurred.

Organization owners and sufficiently privileged administrators may retain the ability to edit or remove these protections, grant themselves access, replace required integrations, or rotate signing credentials. Record the actual remaining actors and powers when applying the settings. Do not describe this as eliminating the risk of a single administrator account takeover.

## Validation before enabling

Use disposable PRs to check: maintainer author plus another approval passes; maintainer alone fails; external author plus one fails; external author plus two passes; new commits and dismissals remove old votes; duplicate reviews count once; non-maintainers never count. Confirm a forged status from a different workflow cannot satisfy the chosen organization-level control. Attempt a tag at an unreviewed commit and confirm signing is blocked. Review the environment/tag bypass lists directly in GitHub and record them here after administrative approval.
