#!/bin/sh
set -eu
# CI uses setup-go; local builds retain the pinned Nix toolchain.
if [ "${PASSLET_GO_SYSTEM:-0}" = 1 ]; then
    case "$(go version)" in
        'go version go1.26.8 '*) exec go "$@" ;;
        *) echo 'Expected Go 1.26.8' >&2; exit 1 ;;
    esac
fi
# Same pinned nixpkgs revision as the validated Go 1.26.8 toolchain.
exec nix shell github:NixOS/nixpkgs/da6dc0e7b26060f2f71ce3ef7088fb56ee44fa36#go --command go "$@"
