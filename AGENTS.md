# Project instructions

Read README.md for project context. Before working in this repository, read and follow `.agents/rules/conventions.md` and `.agents/rules/tooling.md`. Read other files in `.agents/rules` when their path filters match the work.

These rules and the shared skills in `.agents/skills` come from the pinned `teamniteo/agents` input and are linked by the development shell. If the links are missing or broken, run `nix-shell --run true` to regenerate them. Keep upstream instructions linked rather than copying them into this repository. `.claude` is a committed symlink to `.agents`, because Claude Code looks for skills and rules under `.claude/`.

Use `plugins/passlet-dev/skills/passlet/SKILL.md` for authenticated GitHub operations. Its scoped-grant requirements take precedence over upstream guidance about existing GitHub authentication.

Before v1.0, do not preserve backward compatibility. Remove obsolete commands, aliases, and formats instead of keeping compatibility paths.

Workflow `uses:` lines carry version tags, and `.github/workflows/actions.lock` records the commit each one runs. Never edit `actions.lock` by hand. After any `uses:` change or a new workflow, run `gh actions-lock` to regenerate it; install the version `verify-lockfile.yml` pins, with `gh extension install github/gh-actions-lock --pin <version>`. The `Verify GitHub Actions lockfile` check fails a pull request until the lockfile matches.
