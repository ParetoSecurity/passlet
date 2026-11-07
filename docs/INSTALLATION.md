# Installation

Passlet has two parts:

- Approver holds your credentials and asks you to approve access requests.
- Agent runs alongside your coding tools, requests access from Approver, and supplies the approved credentials to Git and `gh`.

Install Approver on the machine where you approve requests. Agent is included, so you can use Passlet locally without another installation.

These instructions target the upcoming CLI-only release. Its signed archives and package metadata must be published before installation works. The published v0.2.0 release uses the older menu-bar app and command interface. See [package maintenance](../packaging/README.md).

## Install Approver on macOS

Approver requires macOS 14 or later and a logged-in graphical session. Apple Silicon and Intel Macs are supported. Choose one package manager for Approver and use it for installation, updates, and removal.

### Nix / Home Manager

Home Manager installs Approver and Agent, starts Approver at graphical login, and restarts it after upgrades. Remove any existing Homebrew-managed Passlet service before enabling it through Home Manager.

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

#### Configure Tailscale forwarding

Install Tailscale 1.104.4 or newer and sign in on both machines. On the Approver Mac, enable Tailscale in the Home Manager module alongside the Passlet module import:

```nix
{
  programs.passlet = {
    enable = true;
    service.tailscale = true;
  };
}
```

[Pair the Agents and Approver](SETUP.md#pair-agents), then activate with the same Home Manager or nix-darwin command used above. Home Manager supplies the Tailscale CLI on the service's PATH. Passlet opens a mutually authenticated TLS listener on `127.0.0.1:17401` and configures raw TCP forwarding from Tailscale port `17400`. Matching forwarding is reused, conflicts stop startup, and unrelated routes are preserved.

Tailscale must already be running and signed in, with permission to configure Serve. The GUI app works with this loopback backend. Tailnet HTTPS certificates are not required because Passlet authenticates its own TLS connection using paired public keys. Passlet never invokes sudo or changes tailnet policy.

Run `passlet approver pair` on the Mac and use its printed command on the Agent. Permit intended callers on TCP 17400 in network policy. The temporary GitHub setup callback uses localhost port 17402.

To disable remote access, set `programs.passlet.service.tailscale = false;` and reactivate. The saved Serve rule remains but its backend is stopped. See [Tailscale troubleshooting](SETUP.md#tailscale) for inspecting and removing only Passlet's rule.


### Homebrew

If you use Homebrew instead of Nix, install Approver and Agent together:

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install --cask paretosecurity/passlet/passlet
```

Run this from your graphical desktop as the user who will approve requests, then complete [GitHub setup](#set-up-github). The cask installs the signed bundle and starts Approver at login.

### Set up GitHub

After activation, run these commands as your normal user:

```sh
passlet approver github setup
passlet doctor
```

Setup opens GitHub in your browser to create and authorize your personal GitHub App. Approver keeps its credentials in your Mac's protected Keychain. Installation and activation never unlock Keychain or authorize GitHub automatically. Each access request still requires your approval.

## Install Approver on Linux

Coming soon. Approver currently requires macOS. You can already run Agent on Linux and connect it to Approver on your Mac.

## Use Agent locally

Agent is installed alongside Approver. After pairing the local Agent and completing GitHub setup, your coding tools can request access on the same Mac. No SSH connection, Tailscale configuration, or separate Agent install is needed.

Agent uses `unix://$HOME/.local/share/passlet/passlet.sock` by default. If you previously configured a remote Approver, clear the environment override and pair locally using a fresh code from `passlet approver pair`:

```sh
unset PASSLET_ENDPOINT
passlet pair --code <code>
passlet doctor
```

Install the [Claude Code or Codex plugin](../README.md#agent-plugins) and start a new coding-agent session. When it needs GitHub access, it can request the repositories and permissions for its task. Review the request and approve with Touch ID.

You can also [request and use access manually](../README.md#request-and-use-access). Delivered tokens keep working until expiry, even if Approver later goes offline.

## Install Agent on a remote machine or VM

Install Agent on the macOS or Linux machine where your coding tools run, and connect it to Approver on your Mac. The remote machine does not need its own GitHub setup or approval service. Both ARM64 and x86_64 are supported.

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

Pass the `passlet` flake input into the module's scope, as in the [Approver configuration](#enable-home-manager). Setting `service.enable = false` installs Agent without starting Approver, including on macOS. On Linux, this is already the default.

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

These options install the package without registering Approver. The macOS package still includes the signed helper, but no local approval service is started.

### Homebrew on Linux

```sh
brew tap paretosecurity/passlet https://github.com/ParetoSecurity/passlet.git
brew install paretosecurity/passlet/passlet-agent
```

### Connect Agent to Approver

[Pair the Agent](SETUP.md#pair-agents), then follow the [SSH forwarding instructions](SETUP.md#ssh-server-configuration) to forward the shared Approver socket into the remote account. Use a separate Unix account and private socket directory for each person. The SSH connection must remain open when requesting new credentials.

On the remote machine, point Agent at the forwarded socket. For example, with a Linux account named `alice`:

```sh
passlet pair unix:///home/alice/.local/share/passlet/passlet.sock --code <code>
passlet doctor
```

Use the actual absolute socket path. On a remote Mac, a typical account path starts with `/Users/alice` instead of `/home/alice`.

[Tailscale HTTPS](#configure-tailscale-forwarding) is an alternative to SSH. Configure both the listener and forwarding on the Approver Mac through Home Manager, then pair the remote Agent using the command printed by `passlet approver pair`.

Install the [Claude Code or Codex plugin](../README.md#agent-plugins) on the remote account and start a new coding-agent session after saving the endpoint. Requests appear on your Approver Mac for approval. Keep GitHub setup and long-lived credentials on that Mac.

## Update or uninstall

For a flake-managed installation, update Passlet's input and activate again:

```sh
nix flake update passlet
```

Use the same activation command as during installation. Home Manager reloads Approver with the new package. For a profile installation, use `nix profile list` to find the installed name, then `nix profile upgrade <name>`.

To uninstall a Home Manager installation, remove `programs.passlet` settings and its module import, then reactivate. For a system package, remove it from `environment.systemPackages` and rebuild. For a profile, use `nix profile remove <name>`. Credentials and audit history remain in place.

### Homebrew updates

Use `brew upgrade --cask paretosecurity/passlet/passlet` for Approver on macOS or `brew upgrade paretosecurity/passlet/passlet-agent` for Agent on Linux. Replace `upgrade` with `uninstall` to remove the corresponding package. Removing Approver stops its login service while retaining credentials and audit history.

## Without flakes

Pin the repository with your existing source manager and import `${passletSource}/packaging/nix/home-manager.nix`. The package remains available through `pkgs.callPackage "${passletSource}/packaging/nix/package.nix" { }`.
