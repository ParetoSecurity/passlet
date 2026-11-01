{
  description = "Short-lived credentials for your agents";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
    in {
      homeManagerModules = {
        default = ./packaging/nix/home-manager.nix;
        passlet = ./packaging/nix/home-manager.nix;
      };
      packages = nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          passlet = pkgs.callPackage ./packaging/nix/package.nix { };
        in {
          inherit passlet;
          default = passlet;
        });
    };
}
