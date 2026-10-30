{
  description = "Passlet development dependencies";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv.url = "github:cachix/devenv";
  };
  outputs = { nixpkgs, devenv, ... }: {
    lib.sources = { inherit nixpkgs devenv; };
  };
}
