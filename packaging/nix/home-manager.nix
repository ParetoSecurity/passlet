{ config, lib, pkgs, ... }:
let
  cfg = config.programs.passlet;
  executable = "${cfg.package}/bin/passlet";
  helper = "${cfg.package}/Applications/Passlet.app/Contents/MacOS/PassletHelper";
  settings = {
    private_key_file = cfg.service.privateKeyFile;
    agents = lib.mapAttrs (_: agent: { public_key = agent.publicKey; }) cfg.service.agents;
    tailscale = cfg.service.tailscale;
  };
  plist = "${config.home.homeDirectory}/Library/LaunchAgents/dev.passlet.agent.plist";
in {
  options.programs.passlet = {
    enable = lib.mkEnableOption "Passlet";
    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      description = "Passlet release package, including the signed macOS helper.";
    };
    service.enable = lib.mkOption {
      type = lib.types.bool;
      default = pkgs.stdenv.hostPlatform.isDarwin;
      description = "Run the approving service at graphical login on this Mac.";
    };
    agent.endpoint = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Approver HTTPS endpoint or absolute Unix socket URL.";
    };
    agent.privateKeyFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = if cfg.agent.approverPublicKey == null then null else "${config.home.homeDirectory}/.local/share/passlet/agent.pem";
      description = "Runtime path to a user-owned mode-0600 Ed25519 PKCS#8 PEM key. Passlet generates it if missing. Never put the private key in the Nix store.";
    };
    agent.approverPublicKey = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Pinned Approver Ed25519 public key in PEM format.";
    };
    service.privateKeyFile = lib.mkOption {
      type = lib.types.str;
      default = "${config.home.homeDirectory}/.local/share/passlet/approver.pem";
      description = "Runtime path to the Approver's user-owned mode-0600 Ed25519 PKCS#8 PEM key. Passlet generates it if missing.";
    };
    service.agents = lib.mkOption {
      type = lib.types.attrsOf (lib.types.submodule {
        options.publicKey = lib.mkOption {
          type = lib.types.str;
          description = "Trusted Agent Ed25519 public key in PEM format. The attribute name is its approval label.";
        };
      });
      default = { };
      description = "Agents allowed to request approval, including local Agents.";
    };
    service.tailscale = lib.mkEnableOption "Expose the Approver through Tailscale Serve on port 17400";
    # macOS names a background item after the program launchd spawns and groups
    # it under the app with the same signing team. Home Manager's launchd.agents
    # wraps every program in /bin/sh, which shows up as "sh" from an unidentified
    # developer, so the agent runs the signed binary directly.
    service.plist = lib.mkOption {
      type = lib.types.package;
      internal = true;
      readOnly = true;
      default = pkgs.writeText "dev.passlet.agent.plist" (lib.generators.toPlist { escape = true; } {
        AssociatedBundleIdentifiers = [ "dev.passlet.app" ];
        EnvironmentVariables.PATH = lib.concatStringsSep ":"
          (lib.optional cfg.service.tailscale "${pkgs.tailscale}/bin" ++ [ "/usr/bin" "/bin" ]);
        KeepAlive.SuccessfulExit = false;
        Label = "dev.passlet.agent";
        LimitLoadToSessionType = "Aqua";
        # A settings change must restart the service even when its arguments match.
        PassletConfiguration = builtins.toJSON settings;
        ProgramArguments = [ executable "approver" "start" "--helper" helper ];
        RunAtLoad = true;
        ThrottleInterval = 10;
        Umask = 63;
      });
    };
  };
  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    xdg.configFile."passlet/approver.toml" = lib.mkIf cfg.service.enable {
      source = (pkgs.formats.toml { }).generate "passlet-approver.toml" settings;
    };
    xdg.configFile."passlet/endpoint" = lib.mkIf (cfg.agent.endpoint != null) {
      text = cfg.agent.endpoint + "\n";
    };
    xdg.configFile."passlet/agent.toml" = lib.mkIf (cfg.agent.privateKeyFile != null && cfg.agent.approverPublicKey != null) {
      source = (pkgs.formats.toml { }).generate "passlet-agent.toml" {
        private_key_file = cfg.agent.privateKeyFile;
        approver_public_key = cfg.agent.approverPublicKey;
      };
    };
    assertions = [{
      assertion = (cfg.agent.privateKeyFile == null) == (cfg.agent.approverPublicKey == null);
      message = "Configure both Passlet Agent privateKeyFile and approverPublicKey.";
    } {
      assertion = builtins.all (path: path == null || (lib.hasPrefix "/" path && !(lib.hasPrefix "/nix/store/" path)))
        ([ cfg.agent.privateKeyFile ] ++ lib.optional cfg.service.enable cfg.service.privateKeyFile);
      message = "Passlet private keys must use absolute runtime paths outside the Nix store.";
    } {
      assertion = !cfg.service.enable || pkgs.stdenv.hostPlatform.isDarwin;
      message = "The Passlet approver requires macOS. Linux installs the agent CLI only.";
    }];
    home.activation.passletOwnership = lib.mkIf cfg.service.enable (lib.hm.dag.entryBefore [ "writeBoundary" ] ''
      plist=${lib.escapeShellArg plist}
      if [ -f "$plist" ] && [ "$(/usr/bin/plutil -extract PassletPackageManager raw -o - "$plist" 2>/dev/null || true)" = homebrew ]; then
        echo "Uninstall the Homebrew Passlet cask before enabling its Home Manager service." >&2
        exit 1
      fi
    '');
    home.activation.passletAgent = lib.mkIf cfg.service.enable (lib.hm.dag.entryAfter [ "linkGeneration" ] ''
      plist=${lib.escapeShellArg plist}
      if ! cmp -s ${cfg.service.plist} "$plist"; then
        run /bin/launchctl bootout --wait "gui/$UID/dev.passlet.agent" 2>/dev/null || true
        run install -Dm444 ${cfg.service.plist} "$plist"
        run /bin/launchctl bootstrap "gui/$UID" "$plist"
      fi
    '');
  };
}
