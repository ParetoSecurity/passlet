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
[ "$#" -eq 0 ] || { echo "Usage: check-macos.sh" >&2; exit 1; }
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target arm64-apple-macosx26.0 -swift-version 5 macos/ApprovalState.swift macos/Credentials.swift macos/main.swift -o build/PassletHelper-ci-arm64 -framework AppKit -framework Security -framework LocalAuthentication
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target arm64-apple-macosx26.0 -swift-version 5 macos/ApprovalState.swift macos/Credentials.swift macos/tests/main.swift -o build/helper-state-tests
build/helper-state-tests build/PassletHelper-ci-arm64
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target arm64-apple-macosx26.0 -swift-version 5 macos/ui-smoke/main.swift -o build/ui-smoke -framework AppKit -framework ApplicationServices
