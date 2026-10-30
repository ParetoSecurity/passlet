#!/bin/sh
# Prepare signed/notarized artifacts only. Publishing is a separate operation.
set -eu
cd "$(dirname "$0")/.."
: "${PASSLET_VERSION:?Set release version, e.g. 0.1.0}"
: "${NOTARIZATION_USERNAME:?Set Apple ID}"
: "${NOTARIZATION_PASSWORD:?Set app-specific password}"
: "${APPLE_TEAM_ID:?Set Apple team ID}"
python3 -c 'import os,re; assert re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", os.environ["PASSLET_VERSION"]), "Expected X.Y.Z version"'
export PASSLET_BUILD_MODE=release
make check
./scripts/build-macos.sh
mkdir -p dist
app=build/release/Passlet.app
archive="dist/passlet_${PASSLET_VERSION}_macos_universal.zip"
ditto -c -k --keepParent "$app" build/release/notarization.zip
xcrun notarytool submit build/release/notarization.zip --apple-id "$NOTARIZATION_USERNAME" --password "$NOTARIZATION_PASSWORD" --team-id "$APPLE_TEAM_ID" --wait
xcrun stapler staple "$app"
xcrun stapler validate "$app"
spctl --assess --type execute --verbose "$app"
ditto -c -k --keepParent "$app" "$archive"
for arch in amd64 arm64; do
    stage="build/release/linux-$arch"
    mkdir -p "$stage/skills/passlet"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 ./scripts/go.sh build -trimpath -ldflags "-s -w -X main.version=$PASSLET_VERSION" -o "$stage/passlet" ./cmd/passlet
    cp skills/passlet/SKILL.md "$stage/skills/passlet/SKILL.md"
    cp README.md "$stage/README.md"
    tar -czf "dist/passlet_${PASSLET_VERSION}_linux_${arch}.tar.gz" -C "$stage" passlet skills README.md
done
(cd dist && shasum -a 256 "passlet_${PASSLET_VERSION}_macos_universal.zip" "passlet_${PASSLET_VERSION}_linux_amd64.tar.gz" "passlet_${PASSLET_VERSION}_linux_arm64.tar.gz" > "passlet_${PASSLET_VERSION}_SHA256SUMS")
echo 'Artifacts prepared in dist/. Nothing has been published.'
