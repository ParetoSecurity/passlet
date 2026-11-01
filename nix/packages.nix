{ nixpkgs, nixpkgs-unstable, system }:
let
  unstable = import nixpkgs-unstable { inherit system; };
in import nixpkgs {
  inherit system;
  overlays = [
    (_final: _prev: {
      # The stable linter is built with Go 1.26 and cannot lint Go 1.27 code.
      inherit (unstable) golangci-lint;
    })
  ];
}
