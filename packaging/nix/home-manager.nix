{ config, lib, pkgs, ... }:
let
  cfg = config.programs.passlet;
  executable = "${cfg.package}/bin/passlet";
  helper = "${cfg.package}/Applications/Passlet.app/Contents/MacOS/PassletHelper";
  settings = { ssh = cfg.service.ssh; tailscale = cfg.service.tailscale; };
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
    service.ssh = lib.mkOption {
      type = lib.types.listOf (lib.types.strMatching "[a-z0-9][a-z0-9-]{0,31}");
      default = [ ];
      description = "SSH tunnel labels, each creating a private ssh-<label>.sock beside the local socket.";
    };
    service.tailscale = lib.mkEnableOption "Expose the Conductor through Tailscale Serve on port 17400";
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
        ProgramArguments = [ executable "conductor" "--helper" helper ];
        RunAtLoad = true;
        ThrottleInterval = 10;
        Umask = 63;
      });
    };
  };
  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    xdg.configFile."passlet/conductor.json" = lib.mkIf cfg.service.enable {
      text = builtins.toJSON settings;
    };
    assertions = [{
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
