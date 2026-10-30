{ system ? (builtins.replaceStrings [ "darwin" ] [ "linux" ] builtins.currentSystem) }:
let
  deps = builtins.getFlake (toString ./.);
in deps.lib.integrationTest { inherit system; source = ../.; }
