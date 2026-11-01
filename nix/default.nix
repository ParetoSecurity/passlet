{ system ? builtins.currentSystem }:
let
  lock = builtins.fromJSON (builtins.readFile ./flake.lock);
  source = name:
    let node = lock.nodes.${lock.nodes.root.inputs.${name}};
    in builtins.fetchTarball {
      url = "https://github.com/${node.locked.owner}/${node.locked.repo}/archive/${node.locked.rev}.tar.gz";
      sha256 = node.locked.narHash;
    };
in import ./packages.nix {
  inherit system;
  nixpkgs = source "nixpkgs";
  nixpkgs-unstable = source "nixpkgs-unstable";
}
