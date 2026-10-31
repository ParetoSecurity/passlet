{ system ? builtins.currentSystem }:
let
  inputs = (builtins.getFlake (toString ./nix)).lib.sources;
  pkgs = import ./nix { inherit system; };
in {
  devShell = inputs.devenv.lib.mkShell {
    inherit inputs pkgs;
    modules = [
      ./devenv.nix
      { devenv.root = toString ./.; }
    ];
  };
}
