#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build/Passlet.iconset
for size in 16 32 128 256 512; do
    sips -z "$size" "$size" assets/passlet.png --out "build/Passlet.iconset/icon_${size}x${size}.png" >/dev/null
    double=$((size * 2))
    sips -z "$double" "$double" assets/passlet.png --out "build/Passlet.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns build/Passlet.iconset -o build/Passlet.icns
