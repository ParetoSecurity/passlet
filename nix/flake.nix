{
  description = "Passlet development dependencies";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    devenv.url = "github:cachix/devenv";
    niteo-agents = {
      url = "github:teamniteo/agents";
      flake = false;
    };
  };
  outputs = { nixpkgs, devenv, niteo-agents, home-manager, ... }: {
    lib.packagingTest = { source, system }: import ./packaging-test.nix {
      pkgs = import nixpkgs { inherit system; };
      inherit home-manager source;
    };
    lib.sources = { inherit nixpkgs devenv niteo-agents; };
    lib.integrationTest = { source, system }:
      import ./integration.nix { pkgs = import nixpkgs { inherit system; }; inherit source; };
  };
}
