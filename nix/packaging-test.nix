{ pkgs, home-manager, source }:
let
  lib = pkgs.lib;
  tomlFormat = (import pkgs.path { system = "aarch64-darwin"; }).formats.toml { };
  flake = (import (source + "/flake.nix")).outputs {
    nixpkgs = { outPath = pkgs.path; inherit lib; };
  };
  fakePackage = pkgs.runCommand "passlet-package-fixture" { } ''
    mkdir -p "$out/bin" "$out/Applications/Passlet.app/Contents/MacOS"
    printf '#!/bin/sh\nexit 0\n' > "$out/bin/passlet"
    chmod +x "$out/bin/passlet"
    cp "$out/bin/passlet" "$out/Applications/Passlet.app/Contents/MacOS/passlet"
    cp "$out/bin/passlet" "$out/Applications/Passlet.app/Contents/MacOS/PassletHelper"
  '';
  evaluate = system: enabled: service: tailscale: package:
    let platformPkgs = import pkgs.path { inherit system; };
    in (home-manager.lib.homeManagerConfiguration {
      pkgs = platformPkgs;
      modules = [ flake.homeManagerModules.default {
        home.username = "alice";
        home.homeDirectory = if platformPkgs.stdenv.hostPlatform.isDarwin then "/Users/alice" else "/home/alice";
        home.stateVersion = "25.05";
        programs.passlet = { enable = enabled; inherit package; service.tailscale = tailscale; service.sshAgentSocket = if tailscale then "/Users/alice/.ssh/agent.sock" else null; sshAgent.enable = platformPkgs.stdenv.hostPlatform.isLinux; }
          // lib.optionalAttrs (service != null) { service.enable = service; };
      } ];
    }).config;
  mac = evaluate "aarch64-darwin" true null false fakePackage;
  tailscale = evaluate "aarch64-darwin" true null true fakePackage;
  updated = evaluate "aarch64-darwin" true null false (fakePackage.overrideAttrs (_: { name = "passlet-package-fixture-new"; }));
  disabled = evaluate "aarch64-darwin" false false false fakePackage;
  linux = evaluate "x86_64-linux" true null false fakePackage;
  client = evaluate "aarch64-darwin" true false false fakePackage;
  invalidLinux = evaluate "x86_64-linux" true true false fakePackage;
  pluginVersion = (builtins.fromJSON (builtins.readFile (source + "/plugin.json"))).version;
  pluginConfig = system: enabled: xdg:
    (home-manager.lib.homeManagerConfiguration {
      pkgs = import pkgs.path { inherit system; config.allowUnfree = false; };
      modules = [ flake.homeManagerModules.default {
        home.username = "alice";
        home.homeDirectory = "/home/alice";
        home.preferXdgDirectories = xdg;
        home.stateVersion = "26.05";
        programs.passlet = {
          enable = enabled;
          package = fakePackage;
          plugins.claude-code.enable = true;
          plugins.codex.enable = true;
          service.enable = false;
        };
        programs.claude-code.package = pkgs.writeShellScriptBin "claude" "exit 0";
        programs.codex.package = null;
        programs.codex.settings.plugins."other@other".enabled = true;
      } ];
    }).config;
  plugins = pluginConfig pkgs.stdenv.hostPlatform.system true false;
  pluginsMac = pluginConfig "aarch64-darwin" true false;
  pluginsXdg = pluginConfig pkgs.stdenv.hostPlatform.system true true;
  pluginsDisabled = pluginConfig "x86_64-linux" false false;
  cache = ".codex/plugins/cache/passlet/passlet/${pluginVersion}";
  plist = mac.programs.passlet.service.plist.text;
  activation = mac.home.activation.passletAgent.data;
  result = { inherit plist; } // lib.optionalAttrs pkgs.stdenv.hostPlatform.isDarwin {
    inherit activation;
    tailscalePlist = tailscale.programs.passlet.service.plist.text;
    package = fakePackage;
    plistFile = mac.programs.passlet.service.plist;
  };
in
assert plugins.programs.claude-code.enable;
assert builtins.all (a: a.assertion) plugins.assertions;
assert builtins.all (a: a.assertion) pluginsMac.assertions;
assert builtins.all (a: a.assertion) pluginsXdg.assertions;
assert plugins.programs.codex.enable;
assert plugins.programs.codex.settings.plugins."passlet@passlet".enabled;
assert plugins.programs.codex.settings.plugins."other@other".enabled;
assert plugins.programs.codex.settings.marketplaces.passlet.source_type == "local";
assert toString plugins.home.file.${cache}.source == plugins.programs.codex.settings.marketplaces.passlet.source;
assert builtins.elem plugins.programs.claude-code.marketplaces.passlet plugins.programs.claude-code.plugins;
assert toString plugins.programs.claude-code.marketplaces.passlet == toString plugins.home.file.${cache}.source;
assert builtins.all (path: builtins.pathExists (plugins.home.file.${cache}.source + "/${path}")) [
  ".agents/plugins/marketplace.json"
  ".claude-plugin/marketplace.json"
  ".claude-plugin/plugin.json"
  "assets/passlet.png"
  "docs/SETUP.md"
  "plugin.json"
  "skills/passlet/SKILL.md"
];
assert pluginsMac.home.file ? ${cache};
assert pluginsXdg.home.file ? ".config/codex/plugins/cache/passlet/passlet/${pluginVersion}";
assert !(pluginsDisabled.home.file ? ${cache});
assert !pluginsDisabled.programs.claude-code.enable;
assert !pluginsDisabled.programs.codex.enable;
assert !linux.programs.codex.enable;
assert !linux.programs.claude-code.enable;
assert builtins.all (a: a.assertion) mac.assertions;
assert builtins.all (a: a.assertion) linux.assertions;
assert linux.home.sessionVariables.SSH_AUTH_SOCK == "/home/alice/.local/share/passlet/ssh-agent.sock";
assert linux.systemd.user.services."passlet-ssh-agent@".Service.ExecStart == [ "${fakePackage}/bin/passlet ssh-agent --stdio" ];
assert linux.systemd.user.services."passlet-ssh-agent@".Service.StandardInput == "socket";
assert linux.systemd.user.sockets.passlet-ssh-agent.Socket.Accept;
assert linux.systemd.user.sockets.passlet-ssh-agent.Socket.SocketMode == "0600";
assert linux.systemd.user.sockets.passlet-ssh-agent.Socket.MaxConnections == 32;
assert linux.systemd.user.sockets.passlet-ssh-agent.Install.WantedBy == [ "sockets.target" ];
assert !(mac.systemd.user.sockets ? passlet-ssh-agent);
assert !(mac.systemd.user.services ? "passlet-ssh-agent@");
assert lib.hasInfix "<string>dev.passlet.agent</string>" plist;
assert lib.hasInfix "<string>/Users/alice/.local/share/passlet/Passlet.app/Contents/MacOS/passlet</string>" plist;
assert lib.hasInfix "<string>/Users/alice/.local/share/passlet/Passlet.app/Contents/MacOS/PassletHelper</string>" plist;
assert lib.hasInfix (builtins.unsafeDiscardStringContext "<string>${fakePackage}</string>") plist;
assert lib.hasInfix "<string>approver</string>" plist;
assert lib.hasInfix "<string>start</string>" plist;
assert lib.hasInfix "<string>dev.passlet.app</string>" plist;
assert lib.hasInfix "<string>Aqua</string>" plist;
assert lib.hasInfix "<integer>63</integer>" plist;
assert lib.hasInfix "<string>/usr/bin:/bin</string>" plist;
assert !(lib.hasInfix "/bin/sh" plist);
assert !(lib.hasInfix "--tailscale" plist);
assert toString mac.xdg.configFile."passlet/approver.toml".source == toString (tomlFormat.generate "passlet-approver.toml" { tailscale = false; });
assert toString tailscale.xdg.configFile."passlet/approver.toml".source == toString (tomlFormat.generate "passlet-approver.toml" { tailscale = true; ssh_agent_socket = "/Users/alice/.ssh/agent.sock"; });
assert !(linux.xdg.configFile ? "passlet/agent.toml");
assert !(linux.xdg.configFile ? "passlet/endpoint");
assert lib.hasInfix "<key>PassletConfiguration</key>" plist;
assert plist != tailscale.programs.passlet.service.plist.text;
assert builtins.elem "linkGeneration" mac.home.activation.passletAgent.after;
assert !(linux.xdg.configFile ? "passlet/approver.toml");
assert !(client.xdg.configFile ? "passlet/approver.toml");
assert lib.hasInfix "/bin:/usr/bin:/bin</string>" tailscale.programs.passlet.service.plist.text;
assert plist != updated.programs.passlet.service.plist.text;
assert lib.hasInfix "launchctl bootstrap" activation;
assert !(mac.launchd.agents ? passlet);
assert !(disabled.home.activation ? passletAgent);
assert !(linux.home.activation ? passletAgent);
assert builtins.elem fakePackage linux.home.packages;
assert !(client.home.activation ? passletAgent);
assert builtins.elem fakePackage client.home.packages;
assert !(builtins.tryEval invalidLinux.home.packages).success;
assert flake.homeManagerModules.default == flake.homeManagerModules.passlet;
assert builtins.attrNames flake.packages == [ "aarch64-darwin" "aarch64-linux" "x86_64-linux" ];
assert builtins.all (system:
  let package = flake.packages.${system}.default.override {
    release = {
      version = "0.0.0-test";
      artifacts = builtins.listToAttrs (map (name: {
        inherit name;
        value = { url = "https://example.invalid/${name}"; sha256 = lib.fakeSha256; };
      }) [ "macos" "linux_arm64" "linux_amd64" ]);
    };
  };
  in package.version == "0.0.0-test" && package.meta.mainProgram == "passlet"
) (builtins.attrNames flake.packages);
pkgs.runCommand "passlet-package-module-test.json" { } ''
  test -x ${plugins.programs.claude-code.finalPackage}/bin/claude
  for cache in ${plugins.home-files}/${cache} ${pluginsXdg.home-files}/.config/codex/plugins/cache/passlet/passlet/${pluginVersion}; do
    test -d "$cache"
    test ! -L "$cache"
    test -f "$cache/plugin.json"
    test -f "$cache/skills/passlet/SKILL.md"
  done
  cp ${pkgs.writeText "passlet-package-module-result.json" (builtins.toJSON result)} "$out"
''
