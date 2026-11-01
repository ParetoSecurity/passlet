# Project instructions

Read README.md for project context. Before working in this repository, read and follow `.agents/rules/conventions.md` and `.agents/rules/tooling.md`. Read other files in `.agents/rules` when their path filters match the work.

These rules and the shared skills in `.agents/skills` come from the pinned `teamniteo/agents` input and are linked by the development shell. If the links are missing or broken, run `nix-shell --run true` to regenerate them. Keep upstream instructions linked rather than copying them into this repository. `.claude` is a committed symlink to `.agents`, because Claude Code looks for skills and rules under `.claude/`.

Use `skills/passlet/SKILL.md` for authenticated GitHub operations. Its scoped-grant requirements take precedence over upstream guidance about existing GitHub authentication.
