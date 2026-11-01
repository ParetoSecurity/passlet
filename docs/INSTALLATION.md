# Installation

Passlet supports Homebrew and Nix/Home Manager. They manage updates and background service startup. GitHub release archives are package-manager inputs, not a separate manual installation path.

The first CLI-only release's package metadata must be published before these commands work. See [package maintenance](../packaging/README.md).

## Homebrew on your Mac

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install --cask paretosecurity/passlet/passlet
passlet github setup
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

Use Home Manager on the Mac where you approve requests. It installs the signed app bundle and CLI, starts the approver at graphical login, and restarts it after upgrades. On Linux, the same module installs only the agent CLI. Both platforms support Intel/AMD and ARM64 machines.

### Release availability

These instructions target the CLI-only package. Its signed release and `packaging/release.json` must be published before installation works. The published v0.2.0 release uses the older menu-bar app and `passlet setup github` command, so it is not the release described here. Do not pin that tag for this configuration. Until the new package metadata is published, Nix reports `Passlet package metadata is not published yet`.

### Add the flake

Add Passlet to your configuration's `flake.nix`:

```nix
inputs.passlet = {
  url = "github:ParetoSecurity/passlet";
  inputs.nixpkgs.follows = "nixpkgs";
};
```

Your `flake.lock` pins the repository revision and its release metadata. Passlet downloads the release archives using the recorded SHA-256 hashes. It preserves the macOS app's signature rather than rebuilding or modifying its executables.

### Enable Home Manager

Pass `passlet` from your flake inputs to the configuration that declares your Home Manager modules. For standalone Home Manager, add it to the existing module list:

```nix
outputs = { nixpkgs, home-manager, passlet, ... }: {
  homeConfigurations.alice = home-manager.lib.homeManagerConfiguration {
    pkgs = nixpkgs.legacyPackages.aarch64-darwin;
    modules = [
      ./home.nix
      passlet.homeManagerModules.default
      { programs.passlet.enable = true; }
    ];
  };
};
```

Replace `alice` with your configuration name and choose your machine's system: `aarch64-darwin`, `x86_64-darwin`, `aarch64-linux`, or `x86_64-linux`. Keep your existing Home Manager settings, including `home.username`, `home.homeDirectory`, and `home.stateVersion`.

If Home Manager is integrated with nix-darwin or NixOS, add this to the system module where your flake's `passlet` input is in scope:

```nix
home-manager.users.alice = {
  imports = [ passlet.homeManagerModules.default ];
  programs.passlet.enable = true;
};
```

Activate through your existing configuration:

```sh
# Standalone Home Manager
home-manager switch --flake .#alice

# Home Manager integrated with nix-darwin
sudo darwin-rebuild switch --flake .#your-host

# Home Manager integrated with NixOS
sudo nixos-rebuild switch --flake .#your-host
```

On the approving Mac, activate while logged into your graphical desktop. Approvals require macOS 14 or later. Remove any existing Homebrew-managed Passlet service before enabling the Home Manager service.

### Set up GitHub on your Mac

After activation, run these commands as your normal user:

```sh
passlet github setup
passlet doctor
```

Setup opens GitHub in your browser to create and authorize your personal GitHub App. Installation and activation never unlock Keychain or authorize GitHub automatically. Each access request still requires native approval.

Local agents can use the default socket immediately after setup. For remote agents, configure [SSH forwarding](SETUP.md#ssh-server-configuration) or [Tailscale](SETUP.md#tailscale), set `PASSLET_ENDPOINT` on the agent, and run `passlet doctor` there. Install the [agent plugin](../README.md#agent-plugins) separately.

### Optional settings

For a Mac that only requests credentials from another Mac, disable its local approver:

```nix
programs.passlet.service.enable = false;
```

To enable the loopback backend for Tailscale Serve:

```nix
programs.passlet.service.tailscaleListen = "127.0.0.1:8484";
```

Reactivate, then follow the [Tailscale setup](SETUP.md#tailscale). This setting does not configure Tailscale Serve or network policy.

### CLI without Home Manager

On an agent machine, add the package to your existing NixOS configuration:

```nix
environment.systemPackages = [ passlet.packages.${pkgs.stdenv.hostPlatform.system}.default ];
```

Or use a Nix profile:

```sh
nix profile add github:ParetoSecurity/passlet
```

For a temporary CLI session:

```sh
nix shell github:ParetoSecurity/passlet
```

These commands install only the package. An approving Mac needs the Home Manager module to manage its background service.

### Update or uninstall

For a flake-managed installation, update Passlet's input and activate again:

```sh
nix flake update passlet
```

Use the same activation command as during installation. Home Manager reloads the approver with the new package. For a profile installation, use `nix profile list` to find the installed name, then `nix profile upgrade <name>`.

To uninstall a Home Manager installation, remove `programs.passlet` settings and its module import, then reactivate. For a system package, remove it from `environment.systemPackages` and rebuild. For a profile, use `nix profile remove <name>`. Credentials and audit history remain in place.

### Without flakes

Pin the repository with your existing source manager and import `${passletSource}/packaging/nix/home-manager.nix`. The package remains available through `pkgs.callPackage "${passletSource}/packaging/nix/package.nix" { }`.
