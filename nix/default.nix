{ system ? builtins.currentSystem }:
let
  lock = builtins.fromJSON (builtins.readFile ./flake.lock);
  node = lock.nodes.${lock.nodes.root.inputs.nixpkgs};
  source = builtins.fetchTarball {
    url = "https://github.com/${node.locked.owner}/${node.locked.repo}/archive/${node.locked.rev}.tar.gz";
    sha256 = node.locked.narHash;
  };
in import source { inherit system; }
