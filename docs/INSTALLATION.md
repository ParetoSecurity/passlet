# Installation

Passlet supports Homebrew and Nix/Home Manager. They manage updates and background service startup. GitHub release archives are package-manager inputs, not a separate manual installation path.

The first CLI-only release's package metadata must be published before these commands work. See [package maintenance](../packaging/README.md).

## Homebrew on your Mac

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install --cask paretosecurity/passlet/passlet
passlet setup github
passlet doctor
```

Install from the graphical session of the user who will approve requests. The cask keeps the signed helper bundle in Homebrew’s Caskroom, puts `passlet` on PATH and starts the per-user approver. Startup does not unlock Keychain, and every token request still needs native approval.

```sh
brew upgrade --cask paretosecurity/passlet/passlet
brew uninstall --cask paretosecurity/passlet/passlet
```

Upgrading restarts the service with the new signed build. Uninstalling stops the service and removes its LaunchAgent, while keeping protected credentials and audit history.

## Homebrew on a Linux agent

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install paretosecurity/passlet/passlet-agent
```

Linux runs only the requesting CLI. Set up your SSH-forwarded socket or Tailscale endpoint as described in [setup](SETUP.md). Use `brew upgrade` and `brew uninstall` for the agent package.

## Nix / Home Manager

Pin this repository using your normal Nix source management, for example a non-flake input:

```nix
inputs.passlet = {
  url = "github:ParetoSecurity/passlet";
  flake = false;
};
```

Import its module in your Home Manager configuration:

```nix
imports = [ "${inputs.passlet}/packaging/nix/home-manager.nix" ];
programs.passlet.enable = true;
```

On macOS this installs the signed release bundle and enables the user LaunchAgent. On Linux it installs the requesting CLI only. For a Mac used only as a requesting agent, set `programs.passlet.service.enable = false`.

Activate your Home Manager configuration, then run `passlet setup github` and `passlet doctor` on the approving Mac. Update the pinned Passlet source and reactivate to upgrade. Set `programs.passlet.enable = false` and reactivate to remove the package and service; credentials and audit history remain.

For CLI-only Nix profiles or NixOS hosts, use `pkgs.callPackage "${inputs.passlet}/packaging/nix/package.nix" { }`. An approving Mac must use the Home Manager module to manage its service.

Choose one package manager for a given user's approver. Remove its service using that manager before switching to the other.
