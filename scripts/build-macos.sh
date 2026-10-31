#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
: "${PASSLET_PROVISIONING_PROFILE:?Set provisioning profile path}"
: "${PASSLET_SIGNING_IDENTITY:?Set signing identity fingerprint}"
mode=${PASSLET_BUILD_MODE:-development}
version=${PASSLET_VERSION:-0.2.0}
case "$mode" in development|release) ;; *) echo 'Invalid build mode' >&2; exit 1;; esac
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
if [ "$mode" = release ]; then
    APP="$PWD/build/release/Passlet.app"
    architectures='arm64 x86_64'
else
    APP="$PWD/build/Passlet.app"
    architectures=arm64
fi
mkdir -p "$(dirname "$APP")" build/swift-cache
/usr/bin/security cms -D -i "$PASSLET_PROVISIONING_PROFILE" > "$(dirname "$APP")/profile.plist"
python3 scripts/app-metadata.py "$(dirname "$APP")/profile.plist" "$APP" "$version" "$mode"
cp "$PASSLET_PROVISIONING_PROFILE" "$APP/Contents/embedded.provisionprofile"
./scripts/icon.sh
cp build/Passlet.icns "$APP/Contents/Resources/Passlet.icns"
cp LICENSE README.md "$APP/Contents/Resources/"
mkdir -p "$APP/Contents/Resources/docs"
cp docs/*.md "$APP/Contents/Resources/docs/"
mkdir -p "$APP/Contents/Resources/skills/passlet"
cp skills/passlet/SKILL.md "$APP/Contents/Resources/skills/passlet/SKILL.md"
for arch in $architectures; do
    /usr/bin/xcrun swiftc -sdk "$SDKROOT" -target "$arch-apple-macosx14.0" -swift-version 5 -O -module-cache-path "$PWD/build/swift-cache" macos/ApprovalState.swift macos/main.swift -o "$(dirname "$APP")/helper-$arch" -framework AppKit -framework Security -framework LocalAuthentication
done
if [ "$mode" = release ]; then
    /usr/bin/lipo -create build/release/helper-arm64 build/release/helper-x86_64 -output "$APP/Contents/MacOS/PassletHelper"
    for arch in arm64 amd64; do
        GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 ./scripts/go.sh build -trimpath -ldflags "-s -w -X main.version=$version" -o "build/release/cli-$arch" ./cmd/passlet
    done
    /usr/bin/lipo -create build/release/cli-arm64 build/release/cli-amd64 -output "$APP/Contents/MacOS/passlet"
    /usr/bin/codesign --force --timestamp --options runtime --sign "$PASSLET_SIGNING_IDENTITY" "$APP/Contents/MacOS/passlet"
    /usr/bin/codesign --force --timestamp --options runtime --sign "$PASSLET_SIGNING_IDENTITY" --entitlements build/release/entitlements.plist "$APP"
    /usr/bin/codesign -dvv "$APP" 2>&1 | /usr/bin/grep -q 'Authority=Developer ID Application:' || { echo 'Release requires Developer ID Application signing' >&2; exit 1; }
else
    cp build/helper-arm64 "$APP/Contents/MacOS/PassletHelper"
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 ./scripts/go.sh build -trimpath -ldflags "-X main.version=$version" -o "$APP/Contents/MacOS/passlet" ./cmd/passlet
    /usr/bin/codesign --force --sign "$PASSLET_SIGNING_IDENTITY" "$APP/Contents/MacOS/passlet"
    /usr/bin/codesign --force --sign "$PASSLET_SIGNING_IDENTITY" --entitlements build/entitlements.plist "$APP"
fi
/usr/bin/codesign --verify --deep --strict "$APP"
echo "Built $APP"
