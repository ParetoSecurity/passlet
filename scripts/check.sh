#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
python3 scripts/test-app-metadata.py
./scripts/go.sh mod verify
unformatted=$(./scripts/go.sh fmt ./...)
if [ -n "$unformatted" ]; then
    printf 'Go files needed formatting:\n%s\n' "$unformatted" >&2
    exit 1
fi
./scripts/go.sh vet ./...
./scripts/go.sh test -race -coverprofile=build/coverage.txt ./...
