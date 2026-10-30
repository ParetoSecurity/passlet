{
  description = "Passlet development dependencies";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv.url = "github:cachix/devenv";
    niteo-agents = {
      url = "github:teamniteo/agents";
      flake = false;
    };
  };
  outputs = { nixpkgs, devenv, niteo-agents, ... }: {
    lib.sources = { inherit nixpkgs devenv niteo-agents; };
    lib.integrationTest = { source, system }:
      import ./integration.nix { pkgs = import nixpkgs { inherit system; }; inherit source; };
  };
}
