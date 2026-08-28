SHELL := sh

# Historical callers passed VERSION on the make command line. Never export or
# expand it: release versions are read only from RELEASE_VERSION_FILE by shell.
unexport VERSION

BUILD_DIR ?= build
RELEASE_DIR ?= release

BINARIES := ipgw ipgw-meta ipgw-legacy
PLATFORMS := \
	darwin-amd64 \
	darwin-arm64 \
	linux-amd64 \
	linux-arm64 \
	windows-amd64 \
	windows-arm64

.PHONY: all build clean test race vet doccheck ci package release release-gate $(PLATFORMS)

define load_build_version
version_file="$${RELEASE_VERSION_FILE:-}"; \
if [ -n "$$version_file" ]; then \
	if [ ! -f "$$version_file" ]; then \
		echo "RELEASE_VERSION_FILE must name a readable regular file" >&2; \
		exit 2; \
	fi; \
	version_bytes=$$(wc -c < "$$version_file" | tr -d '[:space:]'); \
	if [ "$$version_bytes" -gt 256 ]; then \
		echo "release version file exceeds 256 bytes" >&2; \
		exit 2; \
	fi; \
	version=$$(cat -- "$$version_file"); \
else \
	version=$$(git describe --tags --always --dirty 2>/dev/null || printf '%s' dev); \
fi; \
case "$$version" in ''|*[!0-9A-Za-z._+-]*) \
	echo "build version contains unsupported characters" >&2; \
	exit 2;; \
esac;
endef

all: $(PLATFORMS)

build:
	@set -eu; \
	$(load_build_version) \
	ldflags="-s -w -X main.version=$$version"; \
	out="$(BUILD_DIR)/host"; \
	mkdir -p "$$out"; \
	for binary in $(BINARIES); do \
		go build -trimpath -ldflags "$$ldflags" -o "$$out/$$binary" "./cmd/$$binary"; \
	done

$(PLATFORMS):
	@set -eu; \
	$(load_build_version) \
	ldflags="-s -w -X main.version=$$version"; \
	target="$@"; \
	goos="$${target%%-*}"; \
	goarch="$${target##*-}"; \
	out="$(BUILD_DIR)/$$target"; \
	suffix=""; \
	if [ "$$goos" = windows ]; then suffix=.exe; fi; \
	mkdir -p "$$out"; \
	for binary in $(BINARIES); do \
		CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build -trimpath -ldflags "$$ldflags" -o "$$out/$$binary$$suffix" "./cmd/$$binary"; \
	done

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

doccheck:
	go run ./cmd/doccheck --check

ci: doccheck test vet race all

package: all
	@set -eu; \
	$(load_build_version) \
	if [ -z "$$version_file" ]; then \
		echo "package requires RELEASE_VERSION_FILE" >&2; \
		exit 2; \
	fi; \
	bash scripts/release.sh "$(BUILD_DIR)" "$(RELEASE_DIR)" "$$version_file"

release-gate:
	@set -eu; \
	for milestone in M0 M1 M2 M3; do \
		if ! grep -E "^\\| $$milestone .*\\| complete \\|" docs/upgrade/status.md >/dev/null; then \
			echo "release blocked: $$milestone is not complete in docs/upgrade/status.md" >&2; \
			exit 1; \
		fi; \
	done

release: release-gate package

clean:
	rm -rf -- build release
