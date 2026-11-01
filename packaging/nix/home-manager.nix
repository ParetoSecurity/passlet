{ config, lib, pkgs, ... }:
let
  cfg = config.programs.passlet;
  executable = "${cfg.package}/bin/passlet";
  helper = "${cfg.package}/Applications/Passlet.app/Contents/MacOS/PassletHelper";
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
    service.tailscaleListen = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Opt-in loopback listener for Tailscale Serve PROXY v2.";
    };
  };
  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    assertions = [{
      assertion = !cfg.service.enable || pkgs.stdenv.hostPlatform.isDarwin;
      message = "The Passlet approver requires macOS. Linux installs the agent CLI only.";
    }];
    home.activation.passletOwnership = lib.mkIf cfg.service.enable (lib.hm.dag.entryBefore [ "writeBoundary" ] ''
      plist=${lib.escapeShellArg "${config.home.homeDirectory}/Library/LaunchAgents/dev.passlet.agent.plist"}
      if [ -f "$plist" ] && [ "$(/usr/bin/plutil -extract PassletPackageManager raw -o - "$plist" 2>/dev/null || true)" = homebrew ]; then
        echo "Uninstall the Homebrew Passlet cask before enabling its Home Manager service." >&2
        exit 1
      fi
    '');
    launchd.agents.passlet = lib.mkIf cfg.service.enable {
      enable = true;
      config = {
        Label = "dev.passlet.agent";
        ProgramArguments = [ executable "serve" "--helper" helper "--tailscale-listen" cfg.service.tailscaleListen ];
        EnvironmentVariables.PATH = lib.concatStringsSep ":"
          (lib.optional (cfg.service.tailscaleListen != "") "${pkgs.tailscale}/bin" ++ [ "/usr/bin" "/bin" ]);
        KeepAlive.SuccessfulExit = false;
        LimitLoadToSessionType = "Aqua";
        RunAtLoad = true;
        ThrottleInterval = 10;
        Umask = 63;
      };
    };
  };
}
