# Package maintenance

Homebrew and Nix/Home Manager are the supported installation paths. They own service registration, upgrades and removal. The CLI does not write LaunchAgent files. Runtime credentials, account metadata and audit history belong to the user and survive package removal.

## Release metadata

The `vX.Y.Z-prepare` workflow builds and signs the CI-verified head of an open release PR, then uploads `passlet-release-assets` and `passlet-package-updates`. The latter contains `Casks/passlet.rb`, `Formula/passlet-agent.rb`, `packaging/release.json`, `plugin.json`, and `.claude-plugin/plugin.json`, all generated from the same archives. The release JSON records the preparation run ID and source SHA, and both plugin manifests carry the release version.

Download the artifacts from the successful preparation run, verify the signed archives, and add all five generated files to the same release PR. These URLs deliberately refer to the pending release. Squash-merge the reviewed code and metadata as one commit immediately before pushing the final `vX.Y.Z` tag. Do not deploy the intermediate main revision before publication, because its new artifact URLs are not public yet.

Publication downloads the recorded preparation artifacts, verifies that the merged source tree matches the prepared PR apart from generated metadata, verifies checksums, and regenerates the metadata to check exact equality with the final tag. It then uploads and publishes the immutable release. Do not rebuild between preparation and publication, use `latest` or `:no_check`, invent hashes, or change either tag. A final release tag therefore contains matching Nix/Homebrew metadata and plugin versions.

For local generation from downloaded preparation archives, preserve the preparation identity:

```sh
python3 scripts/package-release.py "$PASSLET_VERSION" dist build/package-updates \
  --run-id "$PREPARATION_RUN_ID" --source-sha "$PREPARATION_SHA"
```

Omitting the preparation arguments is useful for local packaging fixtures but produces metadata that cannot be published by the release workflow.

## Homebrew

This repository is the tap. macOS uses the `passlet` cask; Linux uses the `passlet-agent` formula. The cask retains the complete signed bundle in its versioned Caskroom and links its CLI into Homebrew's bin directory. Its hooks invoke the script inside the signed bundle to register a user LaunchAgent at install, stop it before replacement/removal, and start the new version after upgrade. Installation must run as the intended logged-in Mac user. It never requests access to Keychain or starts provider onboarding.

The cask uses Homebrew's installer-script and uninstall-script mechanisms because sandboxed flight steps cannot register launchd jobs. It does not move an app into /Applications.

The service script rejects a plist owned by another installer rather than overwriting it. Do not enable Homebrew and Home Manager for the same user's approver. Uninstall from the existing manager before switching.

## Nix / Home Manager

The root flake exposes `packages.<system>.default` (also named `passlet`) for macOS and Linux on ARM64 and x86_64, plus `homeManagerModules.default` (also named `passlet`). Its `flake.lock` pins stable nixpkgs independently of the development environment in `nix/`. Consumers can follow their own nixpkgs input. Keep both lock files updated when refreshing dependencies.

`nix/package.nix` unpacks pinned release archives. It performs no stripping, patching or wrapping of the signed macOS executables. The CLI symlink resolves into the original app bundle so it can locate the helper. Linux installs only the standalone CLI.

`nix/home-manager.nix` installs the package and, on macOS, declares the per-user LaunchAgent through Home Manager's launchd module. A package change changes the agent's executable path, so activation reloads it. Removing the module's configuration removes the agent. The package never runs setup or creates credentials during a Nix build or activation.

## Validation

`make check` validates release digests and runs the macOS lifecycle script against an isolated launchctl fixture using native plutil. It covers install, repeated install, upgrade restart, uninstall, ownership conflicts and retained data. Run `python3 scripts/test-launchd-packaging.py` in a logged-in Mac session to test real launchd registration, crash recovery, upgrade restart and removal with a disposable sleep job. These tests do not prove Homebrew accepts an actual signed release. Before publishing the first package definitions, test a signed release in a disposable Mac user account through both package managers, including login, upgrade, crash recovery and removal. Never replace the working user's credentials for a packaging test.

To exercise Homebrew itself without changing your normal installation, clone it into the validation prefix and run the disposable-app test:

```sh
git clone --depth 1 https://github.com/Homebrew/brew.git build/homebrew-validation
python3 scripts/test-homebrew-packaging.py
```

This test accepts only that repository-local Homebrew prefix. It uses a dummy sleep process, a unique launchd label and a temporary user directory. It installs, reinstalls, upgrades to a new version and uninstalls through the actual cask hooks. The upgrade check verifies the running service path and CLI version. The generated fixture runs through the system shell so it does not need Gatekeeper approval. It does not access Passlet credentials or validate Developer ID/notarization. Production casks execute the signed CLI directly and retain Homebrew’s normal quarantine behavior.

Run `nix-build nix/packaging-tests.nix --no-out-link` for Home Manager configuration checks. After building the unsigned macOS preview or Linux CLI, run `scripts/check-package-artifact.sh` to verify Nix installation preserves the input files. On macOS it also verifies the preview's ad-hoc signature. CI runs these checks for both platforms.

Home Manager manages installation, service startup, and Tailscale settings. Passlet manages pairing at runtime through `passlet approver pair` and `passlet pair`. Pairing state and private keys stay in each user's `~/.local/share/passlet/`, outside the Nix store, and survive rebuilds. See [pairing setup](../docs/SETUP.md#pair-agents).
