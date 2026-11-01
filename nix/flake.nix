{
  description = "Passlet development dependencies";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    nixpkgs-unstable.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    home-manager = {
      url = "github:nix-community/home-manager/release-26.05";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    devenv.url = "github:cachix/devenv";
    niteo-agents = {
      url = "github:teamniteo/agents";
      flake = false;
    };
  };
  outputs = { nixpkgs, nixpkgs-unstable, devenv, niteo-agents, home-manager, ... }: {
    lib.packagingTest = { source, system }: import ./packaging-test.nix {
      pkgs = import ./packages.nix { inherit nixpkgs nixpkgs-unstable system; };
      inherit home-manager source;
    };
    lib.sources = { inherit nixpkgs devenv niteo-agents; };
    lib.integrationTest = { source, system }:
      import ./integration.nix { pkgs = import ./packages.nix { inherit nixpkgs nixpkgs-unstable system; }; inherit source; };
  };
}
