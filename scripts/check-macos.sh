#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
unset DEVELOPER_DIR SDKROOT
DEVELOPER_DIR="${PASSLET_DEVELOPER_DIR:-$(/usr/bin/xcode-select -p)}"
export DEVELOPER_DIR
if ! /usr/bin/xcrun --find swiftc >/dev/null 2>&1; then
    export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi
SDKROOT="$(/usr/bin/xcrun --sdk macosx --show-sdk-path)"
export SDKROOT
mkdir -p build
for arch in arm64 x86_64; do
    /usr/bin/xcrun swiftc -sdk "$SDKROOT" -target "$arch-apple-macosx14.0" -swift-version 5 macos/ApprovalState.swift macos/main.swift -o "build/PassletHelper-ci-$arch" -framework AppKit -framework Security -framework LocalAuthentication
done
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target "$(uname -m)-apple-macosx14.0" -swift-version 5 macos/ApprovalState.swift macos/tests/main.swift -o build/helper-state-tests
build/helper-state-tests
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target "$(uname -m)-apple-macosx14.0" -swift-version 5 macos/ui-smoke/main.swift -o build/ui-smoke -framework AppKit -framework ApplicationServices
