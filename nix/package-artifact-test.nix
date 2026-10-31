{ system ? builtins.currentSystem, archive }:
let
  pkgs = import ./. { inherit system; };
  platform = if pkgs.stdenv.hostPlatform.isDarwin then "macos_universal"
    else if pkgs.stdenv.hostPlatform.isAarch64 then "linux_arm64" else "linux_amd64";
  package = pkgs.callPackage ../packaging/nix/package.nix {
    release = {
      version = "0.0.0-test";
      artifacts.${platform} = { url = "file://${archive}"; sha256 = pkgs.lib.fakeSha256; };
    };
  };
in package.overrideAttrs (_: { src = builtins.path { path = builtins.toPath archive; name = if pkgs.stdenv.hostPlatform.isDarwin then "fixture.zip" else "fixture.tar.gz"; }; })
