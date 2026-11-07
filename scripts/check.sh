#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "${1:-}" in
    '') lint=true ;;
    --tests-only) lint=false ;;
    *) echo 'Usage: check.sh [--tests-only]' >&2; exit 1 ;;
esac
mkdir -p build
python3 scripts/test-app-metadata.py
python3 scripts/test-packaging.py
python3 scripts/test-release.py
python3 scripts/test-agent-conventions.py
./scripts/go.sh mod verify
if [ "$lint" = true ]; then
    golangci-lint run ./assets/... ./cmd/... ./internal/...
fi
# Count library code exercised by CLI tests; exclude local programs under build/.
./scripts/go.sh test -race -coverpkg=./assets/...,./cmd/...,./internal/... -coverprofile=build/coverage.txt ./assets/... ./cmd/... ./internal/...
if [ "$lint" = true ]; then
    go-test-coverage --config=.testcoverage.yml
fi
