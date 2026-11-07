{ config, lib, pkgs, ... }:
let
  cfg = config.programs.passlet;
  executable = "${cfg.package}/bin/passlet";
  helper = "${cfg.package}/Applications/Passlet.app/Contents/MacOS/PassletHelper";
  settings = {
    tailscale = cfg.service.tailscale;
  } // lib.optionalAttrs (cfg.service.sshAgentSocket != null) {
    ssh_agent_socket = cfg.service.sshAgentSocket;
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
    sshAgent.enable = lib.mkEnableOption "Run an on-demand SSH-agent proxy on Linux";
    service.enable = lib.mkOption {
      type = lib.types.bool;
      default = pkgs.stdenv.hostPlatform.isDarwin;
      description = "Run the approving service at graphical login on this Mac.";
    };
    service.sshAgentSocket = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Absolute upstream SSH-agent socket on the Approver Mac. Null disables forwarding.";
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
    home.sessionVariables = lib.mkIf cfg.sshAgent.enable {
      SSH_AUTH_SOCK = "${config.home.homeDirectory}/.local/share/passlet/ssh-agent.sock";
    };
    systemd.user.sockets.passlet-ssh-agent = lib.mkIf (cfg.sshAgent.enable && pkgs.stdenv.hostPlatform.isLinux) {
      Unit.Description = "Passlet SSH-agent socket";
      Socket = {
        Accept = true;
        DirectoryMode = "0700";
        ListenStream = "${config.home.homeDirectory}/.local/share/passlet/ssh-agent.sock";
        MaxConnections = 32;
        RemoveOnStop = true;
        SocketMode = "0600";
      };
      Install.WantedBy = [ "sockets.target" ];
    };
    systemd.user.services."passlet-ssh-agent@" = lib.mkIf (cfg.sshAgent.enable && pkgs.stdenv.hostPlatform.isLinux) {
      Unit.Description = "Passlet SSH-agent connection";
      Service = {
        ExecStart = "${executable} ssh-agent --stdio";
        StandardInput = "socket";
        StandardOutput = "journal";
        UMask = "0077";
      };
    };
    xdg.configFile."passlet/approver.toml" = lib.mkIf cfg.service.enable {
      source = (pkgs.formats.toml { }).generate "passlet-approver.toml" settings;
    };
    assertions = [{
      assertion = !cfg.service.enable || pkgs.stdenv.hostPlatform.isDarwin;
      message = "The Passlet approver requires macOS. Linux installs the agent CLI only.";
    } {
      assertion = !cfg.sshAgent.enable || pkgs.stdenv.hostPlatform.isLinux;
      message = "The managed Passlet SSH-agent proxy service requires Linux.";
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
