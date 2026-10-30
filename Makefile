.PHONY: check test build macos release
check:
	@mkdir -p build
	./scripts/check.sh
test:
	./scripts/go.sh test -race ./...
build:
	./scripts/build.sh
macos:
	./scripts/build-macos.sh
release:
	./scripts/release.sh
