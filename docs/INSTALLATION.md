# Installation

Passlet has two parts:

- Conductor holds your credentials and asks you to approve access requests.
- Agent runs alongside your coding tools, requests access from Conductor, and supplies the approved credentials to Git and `gh`.

Install Conductor on the machine where you approve requests. Agent is included, so you can use Passlet locally without another installation.

These instructions target the upcoming CLI-only release. Its signed archives and package metadata must be published before installation works. The published v0.2.0 release uses the older menu-bar app and command interface. See [package maintenance](../packaging/README.md).

## Install Conductor on macOS

Conductor requires macOS 14 or later and a logged-in graphical session. Apple Silicon and Intel Macs are supported. Choose one package manager for Conductor and use it for installation, updates, and removal.

### Nix / Home Manager

Home Manager installs Conductor and Agent, starts Conductor at graphical login, and restarts it after upgrades. Remove any existing Homebrew-managed Passlet service before enabling it through Home Manager.

#### Add the flake

Add Passlet to your configuration's `flake.nix`:

```nix
inputs.passlet = {
  url = "github:ParetoSecurity/passlet";
  inputs.nixpkgs.follows = "nixpkgs";
};
```

Your `flake.lock` pins the repository revision and its release metadata. Passlet downloads the release archives using the recorded SHA-256 hashes. It preserves the macOS app's signature rather than rebuilding or modifying its executables.

#### Enable Home Manager

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

Replace `alice` with your configuration name and choose your machine's system: `aarch64-darwin` for Apple Silicon or `x86_64-darwin` for Intel Macs. Keep your existing Home Manager settings, including `home.username`, `home.homeDirectory`, and `home.stateVersion`.

If Home Manager is integrated with nix-darwin, add this to the system module where your flake's `passlet` input is in scope:

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
```

Activate while logged into your Mac’s graphical desktop.

### Homebrew

If you use Homebrew instead of Nix, install Conductor and Agent together:

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install --cask paretosecurity/passlet/passlet
```

Run this from your graphical desktop as the user who will approve requests, then complete [GitHub setup](#set-up-github). The cask installs the signed bundle and starts Conductor at login.

### Set up GitHub

After activation, run these commands as your normal user:

```sh
passlet github setup
passlet doctor
```

Setup opens GitHub in your browser to create and authorize your personal GitHub App. Conductor keeps its credentials in your Mac's protected Keychain. Installation and activation never unlock Keychain or authorize GitHub automatically. Each access request still requires your approval.

## Install Conductor on Linux

Coming soon. Conductor currently requires macOS. You can already run Agent on Linux and connect it to Conductor on your Mac.

## Use Agent locally

Agent is installed alongside Conductor. Once GitHub setup is complete, your coding tools can request access on the same Mac. No SSH connection, Tailscale configuration, or separate Agent install is needed.

Agent uses `unix://$HOME/.local/share/passlet/passlet.sock` by default. If you previously configured a remote Conductor, clear that override:

```sh
unset PASSLET_ENDPOINT
passlet doctor
```

Install the [Claude Code or Codex plugin](../README.md#agent-plugins) and start a new coding-agent session. When it needs GitHub access, it can request the repositories and permissions for its task. Review the request and approve with Touch ID or your password.

You can also [request and use access manually](../README.md#request-and-use-access). Delivered tokens keep working until expiry, even if Conductor later goes offline.

## Install Agent on a remote machine or VM

Install Agent on the macOS or Linux machine where your coding tools run, and connect it to Conductor on your Mac. The remote machine does not need its own GitHub setup or approval service. Both ARM64 and x86_64 are supported.

### Nix / Home Manager

Add the [Passlet flake input](#add-the-flake) to the remote machine's configuration. Import the module in that user's Home Manager configuration:

```nix
{
  imports = [ passlet.homeManagerModules.default ];
  programs.passlet = {
    enable = true;
    service.enable = false;
  };
}
```

Pass the `passlet` flake input into the module's scope, as in the [Conductor configuration](#enable-home-manager). Setting `service.enable = false` installs Agent without starting Conductor, including on macOS. On Linux, this is already the default.

Activate with `home-manager switch --flake .#alice`, or rebuild the existing nix-darwin/NixOS configuration if it manages Home Manager. For NixOS, use `sudo nixos-rebuild switch --flake .#your-host`.

### Nix without Home Manager

To install Agent in your user profile on macOS or Linux:

```sh
nix profile add github:ParetoSecurity/passlet
```

For a temporary session, use `nix shell github:ParetoSecurity/passlet`.

Alternatively, add the package to your NixOS configuration where the `passlet` input is in scope:

```nix
environment.systemPackages = [ passlet.packages.${pkgs.stdenv.hostPlatform.system}.default ];
```

These options install the package without registering Conductor. The macOS package still includes the signed helper, but no local approval service is started.

### Homebrew on Linux

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install paretosecurity/passlet/passlet-agent
```

### Connect Agent to Conductor

Follow the [SSH forwarding instructions](SETUP.md#ssh-server-configuration) to forward your Mac's Conductor socket into the remote account. Use a separate Unix account and private socket directory for each person. The SSH connection must remain open when requesting new credentials.

On the remote machine, point Agent at the forwarded socket. For example, with a Linux account named `alice`:

```sh
export PASSLET_ENDPOINT=unix:///home/alice/.local/share/passlet/mac.sock
passlet doctor
```

Use the actual absolute socket path. On a remote Mac, a typical account path starts with `/Users/alice` instead of `/home/alice`.

[Tailscale HTTPS](SETUP.md#tailscale) is an alternative to SSH. Enable its backend on the Conductor Mac with `programs.passlet.service.tailscaleListen = "127.0.0.1:8484";`, reactivate, and configure Tailscale Serve as described in the setup guide.

Install the [Claude Code or Codex plugin](../README.md#agent-plugins) on the remote account and start a new coding-agent session with `PASSLET_ENDPOINT` set. Requests appear on your Conductor Mac for approval. Keep GitHub setup and long-lived credentials on that Mac.

## Update or uninstall

For a flake-managed installation, update Passlet's input and activate again:

```sh
nix flake update passlet
```

Use the same activation command as during installation. Home Manager reloads Conductor with the new package. For a profile installation, use `nix profile list` to find the installed name, then `nix profile upgrade <name>`.

To uninstall a Home Manager installation, remove `programs.passlet` settings and its module import, then reactivate. For a system package, remove it from `environment.systemPackages` and rebuild. For a profile, use `nix profile remove <name>`. Credentials and audit history remain in place.

### Homebrew updates

Use `brew upgrade --cask paretosecurity/passlet/passlet` for Conductor on macOS or `brew upgrade paretosecurity/passlet/passlet-agent` for Agent on Linux. Replace `upgrade` with `uninstall` to remove the corresponding package. Removing Conductor stops its login service while retaining credentials and audit history.

## Without flakes

Pin the repository with your existing source manager and import `${passletSource}/packaging/nix/home-manager.nix`. The package remains available through `pkgs.callPackage "${passletSource}/packaging/nix/package.nix" { }`.
