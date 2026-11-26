#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mode=${PASSLET_BUILD_MODE:-development}
version=${PASSLET_VERSION:-0.2.0}
case "$mode" in development|release|unsigned) ;; *) echo 'Invalid build mode' >&2; exit 1;; esac
if [ "$mode" != unsigned ]; then
    : "${PASSLET_PROVISIONING_PROFILE:?Set provisioning profile path}"
    : "${PASSLET_SIGNING_IDENTITY:?Set signing identity fingerprint}"
fi
# xcode-select also honors DEVELOPER_DIR; clear Nix's override first.
unset DEVELOPER_DIR SDKROOT
DEVELOPER_DIR="${PASSLET_DEVELOPER_DIR:-$(/usr/bin/xcode-select -p)}"
export DEVELOPER_DIR
if ! /usr/bin/xcrun --find swiftc >/dev/null 2>&1; then
    export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi
# Resolve the Apple SDK instead of inheriting the SDK injected by Nix.
unset SDKROOT
export SDKROOT="${PASSLET_SDKROOT:-$(/usr/bin/xcrun --sdk macosx --show-sdk-path)}"
if [ "$mode" != development ]; then
    APP="$PWD/build/$mode/Passlet.app"
else
    APP="$PWD/build/Passlet.app"
fi
mkdir -p "$(dirname "$APP")" build/swift-cache
rm -rf "$APP"
if [ "$mode" = unsigned ]; then
    python3 scripts/app-metadata.py - "$APP" "$version" "$mode"
else
    /usr/bin/security cms -D -i "$PASSLET_PROVISIONING_PROFILE" > "$(dirname "$APP")/profile.plist"
    python3 scripts/app-metadata.py "$(dirname "$APP")/profile.plist" "$APP" "$version" "$mode"
    cp "$PASSLET_PROVISIONING_PROFILE" "$APP/Contents/embedded.provisionprofile"
fi
./scripts/icon.sh
cp build/Passlet.icns "$APP/Contents/Resources/Passlet.icns"
cp LICENSE README.md "$APP/Contents/Resources/"
cp packaging/homebrew/service.sh "$APP/Contents/Resources/homebrew-service.sh"
mkdir -p "$APP/Contents/Resources/docs"
cp docs/*.md "$APP/Contents/Resources/docs/"
mkdir -p "$APP/Contents/Resources/skills/passlet"
cp plugins/passlet-dev/skills/passlet/SKILL.md "$APP/Contents/Resources/skills/passlet/SKILL.md"
/usr/bin/xcrun swiftc -sdk "$SDKROOT" -target arm64-apple-macosx26.0 -swift-version 5 -O -module-cache-path "$PWD/build/swift-cache" macos/ApprovalState.swift macos/Credentials.swift macos/main.swift -o "$APP/Contents/MacOS/PassletHelper" -framework AppKit -framework Security -framework LocalAuthentication
ldflags="-X main.version=$version"
if [ "$mode" != development ]; then ldflags="-s -w $ldflags"; fi
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 ./scripts/go.sh build -trimpath -ldflags "$ldflags" -o "$APP/Contents/MacOS/passlet" ./cmd/passlet
if [ "$mode" != development ]; then
    if [ "$mode" = unsigned ]; then
        # Ad-hoc signatures allow local execution without a signing identity.
        /usr/bin/codesign --force --sign - "$APP/Contents/MacOS/passlet"
        /usr/bin/codesign --force --sign - "$APP"
    else
        /usr/bin/codesign --force --timestamp --options runtime --sign "$PASSLET_SIGNING_IDENTITY" "$APP/Contents/MacOS/passlet"
        /usr/bin/codesign --force --timestamp --options runtime --sign "$PASSLET_SIGNING_IDENTITY" --entitlements build/release/entitlements.plist "$APP"
        /usr/bin/codesign -dvv "$APP" 2>&1 | /usr/bin/grep -q 'Authority=Developer ID Application:' || { echo 'Release requires Developer ID Application signing' >&2; exit 1; }
    fi
else
    /usr/bin/codesign --force --sign "$PASSLET_SIGNING_IDENTITY" "$APP/Contents/MacOS/passlet"
    /usr/bin/codesign --force --sign "$PASSLET_SIGNING_IDENTITY" --entitlements build/entitlements.plist "$APP"
fi
/usr/bin/codesign --verify --deep --strict "$APP"
echo "Built $APP"
