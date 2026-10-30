#!/bin/sh
# Prepare signed/notarized artifacts only. Publishing is a separate operation.
set -eu
cd "$(dirname "$0")/.."
: "${PASSLET_VERSION:?Set release version, e.g. 0.1.0}"
: "${NOTARIZATION_USERNAME:?Set Apple ID}"
: "${NOTARIZATION_PASSWORD:?Set app-specific password}"
: "${APPLE_TEAM_ID:?Set Apple team ID}"
python3 -c 'import os,re; assert re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", os.environ["PASSLET_VERSION"]), "Expected X.Y.Z version"'
# xcode-select also honors DEVELOPER_DIR; clear Nix's override first.
unset DEVELOPER_DIR SDKROOT
DEVELOPER_DIR="${PASSLET_DEVELOPER_DIR:-$(/usr/bin/xcode-select -p)}"
export DEVELOPER_DIR
if ! /usr/bin/xcrun --find notarytool >/dev/null 2>&1; then
    export DEVELOPER_DIR=/Library/Developer/CommandLineTools
fi
/usr/bin/xcrun --find notarytool >/dev/null
/usr/bin/xcrun --find stapler >/dev/null
export PASSLET_BUILD_MODE=release
make check
./scripts/build-macos.sh
mkdir -p dist
app=build/release/Passlet.app
archive="dist/passlet_${PASSLET_VERSION}_macos_universal.zip"
ditto -c -k --keepParent "$app" build/release/notarization.zip
/usr/bin/xcrun notarytool submit build/release/notarization.zip --apple-id "$NOTARIZATION_USERNAME" --password "$NOTARIZATION_PASSWORD" --team-id "$APPLE_TEAM_ID" --wait
/usr/bin/xcrun stapler staple "$app"
/usr/bin/xcrun stapler validate "$app"
spctl --assess --type execute --verbose "$app"
ditto -c -k --keepParent "$app" "$archive"
for arch in amd64 arm64; do
    stage="build/release/linux-$arch"
    mkdir -p "$stage/skills/passlet" "$stage/docs"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 ./scripts/go.sh build -trimpath -ldflags "-s -w -X main.version=$PASSLET_VERSION" -o "$stage/passlet" ./cmd/passlet
    cp skills/passlet/SKILL.md "$stage/skills/passlet/SKILL.md"
    cp README.md LICENSE "$stage/"
    cp docs/*.md "$stage/docs/"
    tar -czf "dist/passlet_${PASSLET_VERSION}_linux_${arch}.tar.gz" -C "$stage" passlet skills docs README.md LICENSE
done
(cd dist && shasum -a 256 "passlet_${PASSLET_VERSION}_macos_universal.zip" "passlet_${PASSLET_VERSION}_linux_amd64.tar.gz" "passlet_${PASSLET_VERSION}_linux_arm64.tar.gz" > "passlet_${PASSLET_VERSION}_SHA256SUMS")
echo 'Artifacts prepared in dist/. Nothing has been published.'
