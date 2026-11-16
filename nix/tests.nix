{ system ? (builtins.replaceStrings [ "darwin" ] [ "linux" ] builtins.currentSystem), agentArchive ? null }:
let
  deps = builtins.getFlake (toString ./.);
in deps.lib.integrationTest { inherit system agentArchive; source = ../.; }
