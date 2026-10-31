{ pkgs, home-manager, source }:
let
  lib = pkgs.lib;
  fakePackage = pkgs.runCommand "passlet-package-fixture" { } ''
    mkdir -p "$out/bin" "$out/Applications/Passlet.app/Contents/MacOS"
    printf '#!/bin/sh\nexit 0\n' > "$out/bin/passlet"
    chmod +x "$out/bin/passlet"
  '';
  evaluate = system: enabled: service: package:
    let platformPkgs = import pkgs.path { inherit system; };
    in (home-manager.lib.homeManagerConfiguration {
      pkgs = platformPkgs;
      modules = [ (source + "/packaging/nix/home-manager.nix") {
        home.username = "alice";
        home.homeDirectory = if platformPkgs.stdenv.hostPlatform.isDarwin then "/Users/alice" else "/home/alice";
        home.stateVersion = "25.05";
        programs.passlet = { enable = enabled; inherit package; }
          // lib.optionalAttrs (service != null) { service.enable = service; };
      } ];
    }).config;
  mac = evaluate "aarch64-darwin" true null fakePackage;
  updated = evaluate "aarch64-darwin" true null (fakePackage.overrideAttrs (_: { name = "passlet-package-fixture-new"; }));
  disabled = evaluate "aarch64-darwin" false false fakePackage;
  linux = evaluate "x86_64-linux" true null fakePackage;
  agent = mac.launchd.agents.passlet.config;
  result = {
    label = agent.Label;
    arguments = agent.ProgramArguments;
    runAtLoad = agent.RunAtLoad;
    privateMask = agent.Umask;
  };
in
assert builtins.all (a: a.assertion) mac.assertions;
assert builtins.all (a: a.assertion) linux.assertions;
assert agent.Label == "dev.passlet.agent";
assert agent.KeepAlive.SuccessfulExit == false;
assert agent.LimitLoadToSessionType == "Aqua";
assert agent.Umask == 63;
assert agent.RunAtLoad;
assert agent.EnvironmentVariables.PATH == "/usr/bin:/bin";
assert lib.last agent.ProgramArguments == "";
assert agent.ProgramArguments != updated.launchd.agents.passlet.config.ProgramArguments;
assert !(disabled.launchd.agents ? passlet);
assert !(linux.launchd.agents ? passlet);
assert builtins.elem fakePackage linux.home.packages;
pkgs.writeText "passlet-package-module-test.json" (builtins.toJSON result)
