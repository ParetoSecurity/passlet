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

.PHONY: check-all
check-all: check build
	lint

.PHONY: integration check-macos
integration:
	nix-build nix/tests.nix --no-out-link
check-macos:
	./scripts/check-macos.sh
