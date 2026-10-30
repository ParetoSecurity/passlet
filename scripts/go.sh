#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "${PASSLET_DEVENV:-0}" = 1 ]; then
    exec go "$@"
fi
echo "Enter the development environment with nix-shell or direnv allow first." >&2
exit 1
