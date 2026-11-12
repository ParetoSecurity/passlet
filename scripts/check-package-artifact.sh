#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "$(uname -s)" in
    Darwin)
        python3 scripts/test-nix-activation.py "$(nix-build nix/packaging-tests.nix --no-out-link)"
        archive="$PWD/build/package-fixture.zip"
        ditto -c -k --keepParent build/unsigned/Passlet.app "$archive"
        package=$(nix-build nix/package-artifact-test.nix --argstr archive "$archive" --no-out-link)
        /usr/bin/codesign --verify --deep --strict "$package/Applications/Passlet.app"
        diff -qr build/unsigned/Passlet.app "$package/Applications/Passlet.app"
        "$package/bin/passlet" --version
        local_app=$(mktemp -d)
        trap 'rm -rf "$local_app"' EXIT
        ditto "$package/Applications/Passlet.app" "$local_app/Passlet.app"
        chmod -R u+w "$local_app/Passlet.app"
        /usr/bin/codesign --verify --deep --strict "$local_app/Passlet.app"
        "$local_app/Passlet.app/Contents/MacOS/passlet" --version
        ;;
    Linux)
        archive="$PWD/build/package-fixture.tar.gz"
        tar -czf "$archive" -C build passlet
        package=$(nix-build nix/package-artifact-test.nix --argstr archive "$archive" --no-out-link)
        cmp build/passlet "$package/bin/passlet"
        "$package/bin/passlet" --version
        ;;
    *) echo 'Unsupported package test platform' >&2; exit 1 ;;
esac
