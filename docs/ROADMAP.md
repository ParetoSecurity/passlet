# Roadmap

Ideas for future features, in no particular order. Inclusion here is not a commitment to delivery, timing, or a release version.

- Organization approval during GitHub setup. Recognize when an installation needs an organization owner's approval, explain what happens next, and let the user complete setup after approval without recreating their app.
- GitHub reauthorization. Renew revoked or expired authorization through the browser while preserving the user's GitHub App and its installations. Update the protected Keychain credentials after verifying the authorized identity.
- 100% test coverage for the Swift codebase, with meaningful tests for the native helper.
- Fly.io, AWS, Cloudflare, and Vercel providers.
- Native approval on Linux and Windows. Agents already run on macOS and Linux, while approval currently requires macOS.
- iOS and Android companion apps for reviewing and approving requests.
- Agent authentication with replay protection. Pair each Agent with the Approver and verify signed requests bound to that Approver, the requested scope, and a fresh single-use challenge. Reject reused or expired requests before prompting for approval, including after an Approver restart. Show the authenticated Agent in approval prompts and audit history. Design key storage, rotation, and revocation explicitly; processes sharing an Agent’s Unix account remain within the same trust boundary.
- Encrypt token responses for the requesting Agent. Bind the recipient key and encrypted response to the authenticated request so only that Agent can decrypt the token, even when Tailscale Serve or another proxy terminates HTTPS. Authenticate the Approver’s response and reject responses belonging to another request.
- Detect ambient credentials with `passlet doctor`. Check the machine running doctor for tokens in environment variables, dotfiles, and credential configuration used by `gh`, Git, and similar tools. Report variable names and file locations without printing token values, explain how these credentials could bypass Passlet’s scoped grants, and suggest removal or revocation without changing credentials automatically.
- Harden and document the release process, including signing approvals, Nix cache configuration, artifact validation, package metadata publication, and recovery from failed or partial releases.

## Codebase improvements with the feature set frozen

Three review rounds over `main` at d993394 (2026-11-01). Nothing below adds, removes, or changes a user-visible capability. Items that touch observable behavior (exit codes, usage text) are marked as judgment calls. Each round covers tooling, code quality and tests, security, and dead or stale code and docs. Later rounds assume the earlier ones are done and do not repeat them. File references point at the code as it is today.

### Round 1

#### Tooling

- Widen `.golangci.yml` beyond `default: standard`. Add `gosec`, `errorlint`, `gocritic`, `revive`, `misspell`, `bodyclose`, `noctx`, `nilerr`, `unparam`, `unconvert`, `prealloc`, `predeclared`, `copyloopvar`, `intrange`, `usetesting`, `thelper`, `tparallel`, `nolintlint`, `godot`, `goconst`, and `dupl`, and enable the `govet` `shadow` analyzer. Known hits today: `close` shadows the builtin in `internal/passlet/client.go:18` and `internal/passlet/diagnostics.go:33`, and `text` shadows its own parameter in `cmd/passlet/setup_format.go:51`.
- Add the `gofumpt` and `goimports` (or `gci`) formatters. `cmd/passlet/main.go`, `cmd/passlet/main_test.go`, and `cmd/passlet/setup_edge_cases_test.go` interleave the module import with the standard library while every other file groups them.
- Run `govulncheck ./...` in `scripts/check.sh` and in a weekly scheduled workflow. Add `go mod tidy -diff` next to the existing `go mod verify`.
- Python: run `ruff check`, `ruff format --check`, and `mypy` (or `pyright`) over `scripts/*.py` from the devenv `lint` script. The nine scripts have no linter or formatter today and mix quoting styles and shebang conventions.
- Nix: add `nixfmt`, `statix`, and `deadnix` to `lint`. Give the root flake a `checks` output that wraps `nix/packaging-tests.nix` so `nix flake check` covers the consumer-facing flake.
- Shell: add `shfmt -d` beside shellcheck.
- Swift: add a `Package.swift` so `swift build` and `swift test` replace the four hand-maintained `xcrun swiftc` command lines in `scripts/build-macos.sh` and `scripts/check-macos.sh`. Lint with `swift-format lint`. Port `macos/tests/main.swift` from the hand-rolled `check()` to XCTest or swift-testing.
- Docs and data files: `markdownlint` (or `mdformat`), `yamllint`, `prettier --check` for JSON, and `lychee` for links and anchors across README and `docs/`.
- Add `.editorconfig` with `editorconfig-checker`, and `typos` (or `codespell`) over the tree. There is no `.editorconfig` today, and Go, shell, Nix, and Python each use a different indentation.
- GitHub Actions: lint workflows with `zizmor`. Limit `cancel-in-progress` to pull requests (`${{ github.event_name == 'pull_request' }}`) so a quick follow-up push to `main` does not cancel the previous run and lose its coverage artifact.
- Wire the whole lint set into devenv `git-hooks` (pre-commit) so local and CI checks are one list.

#### Code quality, tests, and DRY

- `run()` in `cmd/passlet/main.go` is a 160-line switch that reads `os.Args`, `os.Stdin`, and `os.Stdout` directly. Tests (`invokeCLICommand` in `dispatch_test.go`, the `init()` fixture in `main_test.go`) swap process globals and can never run in parallel. Refactor to `run(ctx, args, stdin, stdout, stderr) error` with one function per subcommand and move fixture mode into `TestMain`.
- Errors are strings. There are 123 distinct `errors.New("snake_case")` codes, compared with `err.Error() == "..."` in `cmd/passlet/setup.go:58,111`, `internal/passlet/setup.go:53`, and `internal/passlet/setup_errors.go:27-35`, plus a switch over code strings in `internal/passlet/client.go:104-111`. Introduce a `Code` error type with sentinels and `errors.Is`, and one table mapping code to HTTP status to terminal text. Keep wire codes separate from prose-shaped codes such as `invalid_endpoint_use_unix_absolute_socket_or_Tailscale_HTTPS_origin`.
- DRY: permission-list parsing exists twice (`cmd/passlet/main.go:100-109`, `cmd/passlet/setup.go:132-138`). Strict JSON decoding exists three ways (`strictJSON` in `types.go`, `json.Valid` plus `DisallowUnknownFields` in `serve_config.go`, `LimitReader` plus `DisallowUnknownFields` in `main.go:64-68`). `strings.TrimPrefix(account.AppURL, "https://github.com/apps/")` appears four times in `cmd/passlet/setup.go` and belongs on `AccountInfo` beside `GitHubAppSettingsURL`. Owner and scope derivation is repeated in `Request.Validate`, `Request.Prompt`, and `GitHub.Mint` and belongs on `Request`.
- Name the byte limits (65536/65537, 8192, 4096/4097, 1<<20, 4<<20 spread over `main.go`, `serve_config.go`, `helper.go`, `client.go`, `server.go`, `transport.go`, `github.go`). Derive `WriteTimeout: 125 * time.Second` in `cmd/passlet/serve.go:81` from `ServerDeadline`. The Swift helper's 120-second fail-closed timer (`macos/main.swift:82`) and the README's 5/120/130-second numbers encode the same constants. Cross-reference them in comments.
- Use `http.Status*` constants in `internal/passlet/server.go` (ten literal status codes, including the non-standard 499) and `client.go`.
- Test robustness: wall-clock assertions in `internal/passlet/helper_test.go:110` (fails above 2 s) and `server_test.go:343` (fails below 80 ms) and the sleep-polling loops in `local_test.go` and `cmd/passlet/serve_test.go` are flake sources. Use `synctest` or channels. Extract one helper for the three `GORACE=atexit_sleep_ms=0 ... exec self -test.run` launcher scripts and one for the five `os.MkdirTemp("/tmp", ...)` short-socket-path helpers (`internal/passlet` already has `socketPath`).
- `setupWithDependencies` binds the production port 45821 itself, so the BDD suite binds the real port and collides with a running `passlet approver github setup` or a parallel `go test`. Inject the listener (or address) through `setupDependencies` so tests use `127.0.0.1:0`.
- Put the test-only fixture processes behind a build tag: `TestHelperProcess`, `TestSetupHelperFixture`, `TestVMFixture` (which pulls `net/http/cgi` into every unit-test binary), and the `PASSLET_CLI_FIXTURE` `init()`. This makes the "no fixture flags exist in production" claim structural rather than conventional.
- Missing tests: `plugin.json` and `.claude-plugin/plugin.json` share version and description (README says to bump both together, nothing checks). The committed `Casks/passlet.rb` and `Formula/passlet-agent.rb` equal `scripts/package-release.py` output for `packaging/release.json` (drift detection). `.github/rulesets/main.json` required-check names equal the `ci.yml` job names. `scripts/github-permissions.py` has no test at all (a small OpenAPI fixture is enough).

#### Security

- Release assets: the Linux tarballs and `*_SHA256SUMS` are unsigned and unattested (only the macOS zip is notarized). Add `actions/attest-build-provenance` for every asset, sign the checksum file (cosign keyless or minisign), and attach an SBOM (syft). The release job needs `id-token: write`.
- Add CodeQL (Go, Swift, Python, Actions), OpenSSF Scorecard, and `dependency-review-action` on pull requests. Dependabot version PRs are disabled by `open-pull-requests-limit: 0`. Confirm Dependabot alerts and security updates remain enabled in repository settings.
- Add `SECURITY.md` (reporting channel, supported versions) and enable private vulnerability reporting.
- `NewGitHub()` builds an `http.Client` without a Transport, so the Mac-side GitHub client follows `HTTPS_PROXY`/`NO_PROXY` from the environment, while the agent client in `EndpointClient` explicitly sets `Proxy: nil`. Make the policy explicit on both and set `tls.Config{MinVersion: tls.VersionTLS13}` on both.
- `WhoIs` runs `tailscale` from `PATH` on every Tailscale request, including `/v1/health`, so any tailnet node can cause one process spawn per request. Resolve the binary once at startup (`exec.LookPath` or a configured absolute path), log which one is used, and cache WhoIs results per peer for a few seconds.
- `OpenAudit` creates its directory with `MkdirAll(0700)` but skips the owner and mode check that `PrivateDir` applies to the socket directory. Reuse `PrivateDir`.
- `release.yml` passes secrets on command lines (`security import -P`, `notarytool --password`), which are visible in the runner's process list. Use `notarytool store-credentials` with `--keychain-profile`. Document the remaining `security import` exposure if there is no alternative.

#### Dead code, rough edges, and stale comments or docs

- README: the preview-comment bot paragraph (line 222) describes `.github/workflows/preview-comment.yml`, removed in #25. "The first CLI-only release must publish package metadata before these installation commands are available" (line 57, and `packaging/README.md:9`) predates the committed v0.3.1 metadata. "At most three repositories" (line 194) contradicts the code, `docs/SETUP.md`, and the skill, which all say 100. "`setup github`" (line 59) is `passlet approver github setup`. "`updates/projects.yaml` still needs a Passlet entry" (line 214) is done in teamniteo/ops. "### Download a development build" is nested under "## License".
- Version-era leftovers: `docs/INSTALLATION.md:10` ("upcoming CLI-only release ... v0.2.0 uses the older menu-bar app"), `docs/SETUP.md:3` ("signed v0.2 bundle"), `docs/VALIDATION.md` ("# v0.2 validation", "Tomorrow's manual checklist", v0.2 steps), `docs/RELEASING.md` ("Repository settings have not been applied by this branch", "Local preparation results" quoting 77.4% and 0% coverage against today's enforced 100%, "v0.2 controls", "maintainer-policy settings proposal" although `docs/REVIEW-POLICY.md` now records verified settings).
- `packaging/README.md:29,31` point at `nix/package.nix` and `nix/home-manager.nix`. The files live in `packaging/nix/`.
- `docs/release-notes.md` is referenced by nothing: `release.yml` uses `--generate-notes` and the release skill writes its own notes file. Wire it in or delete it.
- `runSetupBrowser` (`internal/passlet/setup.go:487`) is only called from tests. Move it to a `_test.go` file.
- Stale version defaults: `scripts/build-macos.sh:5` defaults to `0.2.0` (development builds report it), `nix/test-package.nix:4` says `0.2.0-test`.
- `.gitignore` lists eight paths that do not exist (`prototypes/`, `RESULTS.md`, `scripts/import-test-credentials.py`, `scripts/live-remote.py`, `scripts/test-cruncher.sh`, `internal/passlet/live_test.go`, `packaging/cruncher.nix`, `passlet-zupo-test-client-secret`). Ignoring a `_test.go` path can hide a real test from Git. Move personal ignores to `.git/info/exclude`.
- Seven remote branches from the menu-bar era are unmerged and stale (`feat/menu-logo`, `fix/updater-recovery`, `feat/install-command-line-tool`, `feat/doctor-ambient-tokens`, `feat/doctor-bdd`, `chore/readme-logo-alignment`, `dependabot/go_modules/go-dependencies-087a0af233`). Close or delete them.
- `Makefile`: `.PHONY` is declared in three places, `lint-go` and `test` duplicate `scripts/check.sh`, and `check-all` calls the devenv-only `lint` script and fails obscurely outside the shell.
- Small dead code: the first `lines` assignment in `Request.Prompt` (`internal/passlet/types.go:140`) is overwritten by every branch reachable after `Validate`. `strictJSON` checks for trailing data twice (`d.More()` then a `Decode` expecting EOF). `installationStarted: nil` in the `NewSetupFlow` literal and the one-line `writeSetupIntroduction` wrapper add nothing.

### Round 2

#### Tooling

- Toolchain alignment: `go.mod` declares `go 1.26.0` while `devenv.nix`, `nix/test-package.nix`, and `nix/go-test-coverage.nix` pin `go_1_27`, which is the only reason `nix/packages.nix` overlays `golangci-lint` from a separate `nixpkgs-unstable` input. No 1.27-only API is used (`testing/synctest` is 1.25). Either bump `go.mod` to 1.27 or build with stable Go 1.26 and stable golangci-lint and delete the unstable input and the overlay.
- The ops Sunday updater entry lists `flakes: [nix]`, so the root `flake.lock` (the consumer-facing nixpkgs pin added in #20) is never refreshed. Both locks pin `fa1373130d` today and will drift. Add `.` to the updater's flake list or add a check that both locks resolve the same nixpkgs revision.
- Complexity linters (`gocognit`, `gocyclo`, `funlen`, `nestif`) with thresholds that flag `run`, `SetupFlow.ServeHTTP` (160 lines), `GitHub.Mint`, `GitHub.VerifySetup`, and `setupWithDependencies`.
- `go test -shuffle=on -count=1` in `scripts/check.sh`. Run `FuzzProxyHeader` and the Round 2 fuzzers for a bounded time in a weekly workflow. Use `gotestsum` for CI summaries.
- Homebrew: run `brew audit --strict --cask Casks/passlet.rb`, `brew audit --strict Formula/passlet-agent.rb`, and `brew style` in the macOS CI job. The committed tap files are only exercised by the generation tests today.
- A weekly job that regenerates `internal/passlet/github_permissions.json` from GitHub's OpenAPI description and opens a PR (or fails) on drift. The recorded source `sha256` is otherwise never compared with anything.
- golangci `run.build-tags` for the Round 1 fixture tag so fixtures stay linted, `musttag` for JSON structs, and `forbidigo` to ban `fmt.Print*` and direct `os.Stdout`/`os.Stderr` writes inside `internal/`.

#### Code quality, tests, and DRY

- Split `internal/passlet`. One package holds the HTTP server, the agent client, the GitHub provider, the setup flow and its HTML pages, audit storage, helper IPC, and the PROXY/WhoIs transport. `cmd/` imports about a dozen identifiers. `PrivateDir`, `EndpointClient`, `WhoIs`, `NewSetupFlow`, `Approval`, `Minter`, `Recorder`, and others are exported without external use and can be unexported or regrouped.
- `SetupFlow.phase` is a string (`register`, `converting`, `install`, `authorize`, `saving`, `authorized`, `verifying`, `complete`, `failed`) compared ad hoc. Make it a typed enum with an explicit transition table. `RunInstallationSetup` sets `flow.pending`, `flow.phase`, and `flow.initialURL` from outside the constructor, so add a constructor for the installation-only flow.
- `SetupFlow.ServeHTTP` holds `s.mu` across provider network calls (`ConvertManifest`, `AuthorizeCode`, `VerifySetup`, each up to the 20-second client timeout). Narrow the lock to phase checks and transitions.
- `Request.Validate` mutates the caller's `Repositories` slice and `Permissions` map through a value-copied `Request`, and `GitHub.Mint` re-validates input the server already validated. Make normalization explicit (`Normalize() (Request, error)`) or document the aliasing.
- `GitHub.call` sends `Content-Type: application/json` and an empty body reader on GET requests, `User-Agent: Passlet` carries no version, and the API version header is a literal. Pass a nil body for bodiless calls and move the headers into constants.
- Pagination over `/user/installations` is written twice (`Mint`, `VerifySetup`). Extract `listInstallations`. `manifestPermissions` and `normalizeRegistrationPermissions` hard-code the same two aliases in opposite directions. Derive both from one alias table.
- `cmd/passlet/diagnostics.go` writes twelve `fmt.Println` lines straight to process stdout, so tests must redirect `os.Stdout` to read them. Pass an `io.Writer`. Make `Fprint` error handling consistent: `serve.go:86-88` and `main.go:116,189` are bare while the rest use `_, _ =`.
- `exec` flattens any child failure to exit code 1 (`command_failed`), and on Ctrl-C `exec.CommandContext` SIGKILLs the child. Propagate the child's status from `*exec.ExitError` and set `cmd.Cancel` and `WaitDelay` so Git and gh receive SIGTERM first. Judgment call: exit codes are observable, but today's behavior discards information agents act on.
- Tests: `TestGitHubSetupFeatures` is a 400-line closure with a dozen captured variables. Turn it into a step-definition struct. Two helper fixtures (`TestHelperProcess`, `TestSetupHelperFixture`) model the same Swift protocol, so keep one. The `*_errors_test.go` split scatters one subject's tests across files. Regroup by subject.
- New tests: `Request.Prompt` on an unvalidated request (today's dead branch), `readProxy` with TLVs and more IPv6 cases beyond the two happy paths, `safeLabel` with multibyte input, `ReadAudit` at the limit boundary of 1000, `StateDir` and `DefaultEndpoint` with `HOME` unset. Fuzzers for `Request.Validate` plus `Prompt` (invariants: prompt within 650 bytes and the helper's 1000-character limit), `GitHubAppName`, and `endpointURL`.
- Swift: move `credentialData`, the `--status`/`--import` argument handling, and Keychain query construction out of top-level script code into types compiled into `macos/tests`, as `ApprovalState` already is. This feeds the Swift-coverage item above.

#### Security

- Consolidate the threat model into one `docs/SECURITY-MODEL.md`. It is currently spread over README "Trust and failure boundaries", the transport sections of `docs/SETUP.md`, the trust notes in `skills/passlet/SKILL.md`, and the limits section of `docs/REVIEW-POLICY.md`. Include that the fixed `/github/installed` callback is reachable by any local process while setup is in the `authorized` phase, and what `VerifySetup` does and does not guarantee for an attacker-chosen `installation_id`.
- `OpenAudit` passes the raw path to `sql.Open("sqlite", path)` while `ReadAudit` builds a `file:` URL with `mode=ro`. A path containing `?` or `#` is parsed as DSN options. Use the URL form with explicit `_pragma` options in both.
- `exec` scrubs credential and config variables but inherits TLS-weakening Git variables (`GIT_SSL_NO_VERIFY`, `GIT_SSL_CAINFO`, `GIT_SSL_CAPATH`, `GIT_PROXY_SSL_*`) and proxy variables. Either scrub them or state in the docs that the agent's environment is fully trusted. Keep the scrub list as one named slice shared with `TestExecReplacesAmbientCredentials`, which hard-codes the same names today.
- `passlet request --out` creates the file 0600 with `O_EXCL` but never looks at the parent directory, while README and the skill tell agents to `mktemp -d` and `chmod 700`. Warn on stderr when the parent is group- or world-accessible, mirroring `CheckPrivateDir`.
- CI: give every job an explicit `permissions:` block (`ci.yml` relies on the workflow default) and add `timeout-minutes` to the steps that may build Nix closures from source.

#### Dead code, rough edges, and stale comments or docs

- Terminology drift: `docs/INSTALLATION.md` says Approver and Agent (28 and 16 mentions), README uses each once and otherwise says service and helper, and `docs/SETUP.md`, `skills/passlet/SKILL.md`, and `docs/release-notes.md` never use them. Pick one vocabulary and add a glossary.
- `Event.Approver` is always `"human"` (`internal/passlet/server.go:102`). Keep it for schema stability and document it, or stop writing it.
- `docs/REVIEW-POLICY.md` still carries the "replace `Test (ubuntu-24.04)` with `Test (ubuntu-26.04)`" migration note although `ci.yml` and `rulesets/main.json` already use 26.04. Confirm the live ruleset and delete the note. `docs/VALIDATION.md` mixes dated log entries ("2026-10-31: live browser testing ...") into a checklist. Move logs into issues or a dated validation log.
- README "Development and validation" is one 400-word paragraph covering the ops updater, caches, Cachix, and `vendorHash`. Move it to `CONTRIBUTING.md` or `docs/DEVELOPMENT.md` and keep README for usage.
- Shell duplication: `DEVELOPER_DIR`/`SDKROOT` resolution appears in `build-macos.sh`, `check-macos.sh`, and `release.sh` (and `build-macos.sh` unsets `SDKROOT` twice), so extract `scripts/apple-env.sh`. The `vX.Y.Z` regex appears four times (`release.yml` twice, `release.sh`, `package-release.py`). The golangci path list `./assets/... ./cmd/... ./internal/...` appears in `check.sh`, `Makefile`, and `docs/VALIDATION.md`. Exclude `build/` in `.golangci.yml` and use `./...`.
- `nix/integration.nix` hard-codes `192.168.1.3` for cruncher, which only holds because cruncher is the third declared node. Derive it from `nodes.cruncher.networking.primaryIPAddress` or comment the coupling.

### Round 3

#### Tooling

- `govet` `fieldalignment` and a struct-ordering pass: `Event` in `audit.go` and `Grant` in `types.go` put `GrantedOrganization`/`Organization` ahead of `RequestID`/`ID`, and `SetupFlow` mixes 14 fields of five kinds. Group fields by role.
- `depguard` to keep `net/http/cgi`, `os/exec`, and `database/sql` inside the packages that own them after the Round 2 split, and `gochecknoglobals` with an allowlist for the embedded catalog, templates, and CSP hash.
- `vale` with a project vocabulary (Passlet, Approver, Agent, Touch ID, Keychain, Home Manager) so the Round 2 terminology decision holds in CI.
- A `CHANGELOG.md` maintained per release (`docs/release-notes.md` is a static blurb), generated with `git-cliff` or written by hand and applied through `gh release edit --notes-file`.
- Swift coverage from `swift test --enable-code-coverage`, uploaded as a CI artifact beside the Go coverage.
- A self-documenting `make help` and a single `make lint` that maps one-to-one onto CI so README and `docs/VALIDATION.md` stop listing individual commands.

#### Code quality, tests, and DRY

- User-facing setup text (`setupIntroduction` and a dozen `writeSetupText` literals in `cmd/passlet/setup.go`) is duplicated verbatim in `cmd/passlet/features/github_setup.feature`. Keep one source (embedded text files or a messages table) and have the feature file reference keys or be generated from it.
- `internal/passlet/setup_pages.go` embeds about 2 KB of CSS and HTML as Go string constants. Move them to files under `assets/` with `//go:embed`, as the favicon already is, and compute the CSP hashes at init.
- `Request.Prompt` rebuilds the whole body in a shrinking loop (quadratic for 100 repositories). Build it in one pass against a byte budget.
- Two login validators coexist: the `githubHandle` regex in `setup.go` and the `component` regex in `types.go` (also used for `AccountInfo.Login`) accept different sets. Use one `validLogin`. `AccountInfo.Validate` detects a bad app URL by comparing `GitHubAppSettingsURL(...)` against the fallback string. Return `(slug string, ok bool)` instead.
- `StateDir` ignores `os.UserHomeDir` errors and returns a relative path when `HOME` is unset, which surfaces later as `socket_requires_absolute_path...`. Return an error or fail fast in `main`.
- `githubCommand` and `setupWithDependencies` both validate `setup|reset` (`cmd/passlet/setup.go:23-25` and `66-68`).
- `exec` strips variables with `strings.HasPrefix(k, "GH_HOST")` (`cmd/passlet/main.go:158`). Use exact matches from the shared scrub list.
- `helperSession.Close` hard-codes a one-second kill grace, and `ClientDeadline` (130 s) should be derived from `ServerDeadline` with the reasoning in one comment.
- Python: add type hints. `scripts/github-permissions.py` writes to a cwd-relative path and embeds its scope classification sets inline. Use a `ROOT`-relative path like `package-release.py` and move the classification into a JSON file beside the catalog. `test-launchd-packaging.py` and `test-homebrew-packaging.py` use bare `assert` statements (stripped under `-O`). Use unittest assertions.
- Swift: `macos/main.swift` is 160 lines of top-level script mixing argument parsing, Keychain queries, LocalAuthentication, and the AppKit lifecycle. Split into `KeychainStore.swift`, `Helper.swift`, and `main.swift`. `reply()` exits 1 silently when JSON serialization fails.
- Nix: `AssociatedBundleIdentifiers = [ "dev.passlet.app" ]` is hard-coded while `scripts/app-metadata.py` derives the bundle id from the provisioning profile. Carry the bundle id in `packaging/release.json`.
- Test fixtures use real accounts and repositories (`zupo`, `teamniteo/ops`, and others) in `server_test.go`, `github_test.go`, and `repository_groups_test.go` while other tests use `alice` and `acme`. Standardize on neutral names.
- `devenv.nix` `enterTest` repeats `make check` and `make build` (which is `check-all` without `lint`). Point it at `check-all`.

#### Security

- The Git credential wrapper path is placed in `GIT_CONFIG_VALUE_1` unquoted (`cmd/passlet/main.go:163`). Git runs helper values through the shell, so a `TMPDIR` containing spaces or metacharacters breaks or misroutes the helper. Quote it (or use the `!'...'` form) and add a test with a spaced `TMPDIR`.
- The Homebrew LaunchAgent PATH (`packaging/homebrew/service.sh`) puts the user-writable `$prefix/bin` ahead of `/usr/bin:/bin`, so `tailscale` resolves from a user-writable directory. Resolve the absolute binary at install time once the Round 1 code change lands.
- Add a post-release job that downloads the published assets and runs `gh attestation verify` and the checksum signature check, and extend `scripts/check-package-artifact.sh` to verify the attestation of the artifact it installs.
- Reproducible builds: set `CGO_ENABLED=0` for the local `make build` as well as the cross builds, and add a CI step that builds the Linux CLI twice and compares hashes. This makes the SHA256SUMS independently verifiable from source.
- Evaluate replacing `github.com/peterh/liner` (one editable prompt) with `golang.org/x/term` plus a small line editor. It would drop three transitive dependencies from the production binary. Only proceed if the pre-filled, editable handle prompt stays identical.
- `EndpointClient` could reject IP-literal HTTPS origins and require a hostname (Tailscale Serve certificates are issued for `.ts.net` names only). Add a test that `https://100.64.0.1:8443` is refused.

#### Dead code, rough edges, and stale comments or docs

- `internal/passlet/vm_test.go:3-4` says the fixture is "compiled only by `go test -c` for NixOS VM tests", but it is compiled into every `go test ./internal/passlet` run and merely skips. Fix the comment or add the Round 1 build tag. `cmd/passlet/dispatch_test.go:16-17` ("No test using process-wide arguments or streams may run in parallel") becomes obsolete after the Round 1 `run` refactor.
- The usage string in `cmd/passlet/main.go:38` hand-lists commands in arbitrary order and omits `--help`. Generate it from the command table after the Round 1 refactor.
- `writeCommandError` prints a bare newline for `errGitHubAppNameTaken` (`cmd/passlet/main.go:30-33`) because the explanation was already printed. Comment why or print nothing.
- `docs/RELEASING.md` "Conventions" cites pareto-mac, canario/trak/vend, and pareto-linux, which mean nothing to public readers. Link them or drop the comparison. `.github/actionlint.yaml` carries a dated workaround for the `ubuntu-26.04` label. Remove it once the pinned actionlint knows the label.
- `plugin.json` (Codex) lacks the `author` and `icon` fields that `.claude-plugin/plugin.json` has, and both marketplace manifests repeat the description string. Generate the four manifests from one source or rely on the Round 1 consistency test.
- `nix/packages.nix` carries the comment "The stable linter is built with Go 1.26 and cannot lint Go 1.27 code", which describes the constraint created by the Round 2 toolchain mismatch. Delete it with the overlay.
- `docs/SETUP.md` "Repository groups" and "Administrative access" duplicate paragraphs from `skills/passlet/SKILL.md`. Keep operational detail in SETUP.md and link from the skill, which ships to agents and should stay short.
- Plugin installation instructions live in README "Agent plugins" and are linked back to from `docs/INSTALLATION.md` twice. Give them one home.
- `Casks/passlet.rb` and `Formula/passlet-agent.rb` are generated artifacts committed for the tap layout. Add a "generated by scripts/package-release.py, do not edit" header to the templates so the committed files carry it.

## Organization installation awaiting owner approval

Confirm the request callback in a live GitHub walkthrough before implementing this behavior. Reference: https://sparkwing.dev/docs/github-app/.

```gherkin
Scenario: Organization installation needs owner approval
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    And `ACME` requires an organization owner to approve app installations
    When I run `passlet approver github setup`
    Then Passlet asks me to unlock its Keychain item
    When I approve with Touch ID
    Then my browser opens "https://github.com/apps/passlet-alice/installations/new"
    When I request installation of `passlet-alice` for `ACME` on GitHub
    And GitHub redirects my browser to "http://127.0.0.1:17402/github/installed?setup_action=request" without an installation ID
    Then the terminal shows:
      """
      Organization owner approval is required. This request has not enabled repository access.
      You can close this terminal. Check the installation request on GitHub for approval.
      """
    And the browser shows:
      """
      Organization owner approval is required.
      You can close this tab. Check the installation request on GitHub for approval.
      """
    And the existing Keychain credentials remain unchanged
    And the command exits unsuccessfully
```
