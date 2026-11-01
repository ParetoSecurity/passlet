{ pkgs, home-manager, source }:
let
  lib = pkgs.lib;
  flake = (import (source + "/flake.nix")).outputs {
    nixpkgs = { outPath = pkgs.path; inherit lib; };
  };
  fakePackage = pkgs.runCommand "passlet-package-fixture" { } ''
    mkdir -p "$out/bin" "$out/Applications/Passlet.app/Contents/MacOS"
    printf '#!/bin/sh\nexit 0\n' > "$out/bin/passlet"
    chmod +x "$out/bin/passlet"
  '';
  evaluate = system: enabled: service: package:
    let platformPkgs = import pkgs.path { inherit system; };
    in (home-manager.lib.homeManagerConfiguration {
      pkgs = platformPkgs;
      modules = [ flake.homeManagerModules.default {
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
  client = evaluate "aarch64-darwin" true false fakePackage;
  invalidLinux = evaluate "x86_64-linux" true true fakePackage;
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
assert !(client.launchd.agents ? passlet);
assert builtins.elem fakePackage client.home.packages;
assert !(builtins.tryEval invalidLinux.home.packages).success;
assert flake.homeManagerModules.default == flake.homeManagerModules.passlet;
assert builtins.attrNames flake.packages == [ "aarch64-darwin" "aarch64-linux" "x86_64-darwin" "x86_64-linux" ];
assert builtins.all (system:
  let package = flake.packages.${system}.default.override {
    release = {
      version = "0.0.0-test";
      artifacts = builtins.listToAttrs (map (name: {
        inherit name;
        value = { url = "https://example.invalid/${name}"; sha256 = lib.fakeSha256; };
      }) [ "macos_universal" "linux_arm64" "linux_amd64" ]);
    };
  };
  in package.version == "0.0.0-test" && package.meta.mainProgram == "passlet"
) (builtins.attrNames flake.packages);
pkgs.writeText "passlet-package-module-test.json" (builtins.toJSON result)
