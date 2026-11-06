#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
python3 scripts/test-app-metadata.py
python3 scripts/test-packaging.py
python3 scripts/test-release.py
python3 scripts/test-agent-conventions.py
./scripts/go.sh mod verify
golangci-lint run ./assets/... ./cmd/... ./internal/...
# Count library code exercised by CLI tests; exclude local programs under build/.
./scripts/go.sh test -race -coverpkg=./assets/...,./cmd/...,./internal/... -coverprofile=build/coverage.txt ./assets/... ./cmd/... ./internal/...
go-test-coverage --config=.testcoverage.yml
