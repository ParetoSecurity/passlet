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
  '';
  evaluate = system: enabled: service: tailscale: package:
    let platformPkgs = import pkgs.path { inherit system; };
    in (home-manager.lib.homeManagerConfiguration {
      pkgs = platformPkgs;
      modules = [ flake.homeManagerModules.default {
        home.username = "alice";
        home.homeDirectory = if platformPkgs.stdenv.hostPlatform.isDarwin then "/Users/alice" else "/home/alice";
        home.stateVersion = "25.05";
        programs.passlet = { enable = enabled; inherit package; service.tailscale = tailscale; }
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
  plist = mac.programs.passlet.service.plist.text;
  activation = mac.home.activation.passletAgent.data;
  result = { inherit plist; };
in
assert builtins.all (a: a.assertion) mac.assertions;
assert builtins.all (a: a.assertion) linux.assertions;
assert lib.hasInfix "<string>dev.passlet.agent</string>" plist;
assert lib.hasInfix (builtins.unsafeDiscardStringContext "<string>${fakePackage}/bin/passlet</string>") plist;
assert lib.hasInfix "<string>approver</string>" plist;
assert lib.hasInfix "<string>start</string>" plist;
assert lib.hasInfix "<string>dev.passlet.app</string>" plist;
assert lib.hasInfix "<string>Aqua</string>" plist;
assert lib.hasInfix "<integer>63</integer>" plist;
assert lib.hasInfix "<string>/usr/bin:/bin</string>" plist;
assert !(lib.hasInfix "/bin/sh" plist);
assert !(lib.hasInfix "--tailscale" plist);
assert toString mac.xdg.configFile."passlet/approver.toml".source == toString (tomlFormat.generate "passlet-approver.toml" { tailscale = false; });
assert toString tailscale.xdg.configFile."passlet/approver.toml".source == toString (tomlFormat.generate "passlet-approver.toml" { tailscale = true; });
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
