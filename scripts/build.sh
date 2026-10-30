#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
./scripts/go.sh build -trimpath -o build/passlet ./cmd/passlet
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 ./scripts/go.sh build -trimpath -o build/passlet-linux-amd64 ./cmd/passlet
