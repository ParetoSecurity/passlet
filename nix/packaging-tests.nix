{ system ? builtins.currentSystem }:
let
  deps = builtins.getFlake (toString ./.);
in deps.lib.packagingTest { inherit system; source = ../.; }
