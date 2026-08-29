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

.PHONY: all build clean test race vet doccheck ci package candidate-build candidate-gate release release-gate $(PLATFORMS)

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

candidate-gate:
	@set -eu; \
	status_file=docs/upgrade/status.md; \
	if [ ! -f "$$status_file" ] || [ -L "$$status_file" ]; then \
		echo "candidate blocked: docs/upgrade/status.md must be a regular file" >&2; \
		exit 1; \
	fi; \
	for milestone in M0 M1 M2; do \
		rows=$$(grep -Ec "^\\| $$milestone [^|]*\\| [^|]+ \\|" "$$status_file" || :); \
		complete=$$(grep -Ec "^\\| $$milestone [^|]*\\| complete \\|" "$$status_file" || :); \
		if [ "$$rows" -ne 1 ] || [ "$$complete" -ne 1 ]; then \
			echo "candidate blocked: $$milestone must appear exactly once and be complete in docs/upgrade/status.md" >&2; \
			exit 1; \
		fi; \
	done

candidate-build:
	@set -eu; \
	version_file="$${RELEASE_VERSION_FILE:-}"; \
	if [ -z "$$version_file" ] || [ ! -f "$$version_file" ] || [ -L "$$version_file" ]; then \
		echo "candidate build requires RELEASE_VERSION_FILE naming a regular file" >&2; \
		exit 2; \
	fi; \
	if ! printf '%s\n' v1.0.0 | cmp -s - "$$version_file"; then \
		echo "candidate release version must be exactly v1.0.0" >&2; \
		exit 2; \
	fi; \
	version=v1.0.0; \
	actual_go_version=$$(GOTOOLCHAIN=local go env GOVERSION); \
	actual_host_os=$$(GOTOOLCHAIN=local go env GOHOSTOS); \
	actual_host_arch=$$(GOTOOLCHAIN=local go env GOHOSTARCH); \
	if [ "$$actual_go_version" != go1.25.0 ]; then \
		echo "candidate build requires local Go go1.25.0, got $$actual_go_version" >&2; \
		exit 2; \
	fi; \
	if [ "$$actual_host_os-$$actual_host_arch" != linux-amd64 ]; then \
		echo "candidate build requires linux-amd64 host, got $$actual_host_os-$$actual_host_arch" >&2; \
		exit 2; \
	fi; \
	unset actual_go_version actual_host_os actual_host_arch; \
	requested_build_dir="$(BUILD_DIR)"; \
	case "$$requested_build_dir" in ''|/|.|..) \
		echo "candidate BUILD_DIR must name a new non-root directory" >&2; \
		exit 2;; \
	esac; \
	build_parent_input=$$(dirname -- "$$requested_build_dir"); \
	build_base=$$(basename -- "$$requested_build_dir"); \
	case "$$build_base" in ''|.|..) \
		echo "candidate BUILD_DIR must have a concrete final path component" >&2; \
		exit 2;; \
	esac; \
	mkdir -p -- "$$build_parent_input"; \
	build_parent=$$(cd -- "$$build_parent_input" && pwd -P); \
	build_dir="$$build_parent/$$build_base"; \
	if [ -e "$$build_dir" ] || [ -L "$$build_dir" ]; then \
		echo "candidate BUILD_DIR already exists; refusing to overwrite it" >&2; \
		exit 1; \
	fi; \
	stage=$$(mktemp -d "$$build_parent/.$$build_base.candidate.XXXXXX"); \
	cleanup_candidate_build() { \
		if [ -n "$${stage:-}" ] && [ -d "$$stage" ]; then rm -rf -- "$$stage"; fi; \
	}; \
	trap cleanup_candidate_build EXIT; \
	trap 'exit 130' HUP INT TERM; \
	ldflags="-s -w -X main.version=$$version"; \
	for target in $(PLATFORMS); do \
		goos=$${target%%-*}; \
		goarch=$${target##*-}; \
		suffix=; \
		if [ "$$goos" = windows ]; then suffix=.exe; fi; \
		mkdir -p -- "$$stage/$$target"; \
		for binary in $(BINARIES); do \
			GOTOOLCHAIN=local CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 \
				GOOS="$$goos" GOARCH="$$goarch" \
				go build -trimpath -buildvcs=false -ldflags "$$ldflags" \
				-o "$$stage/$$target/$$binary$$suffix" "./cmd/$$binary"; \
		done; \
	done; \
	mkdir -p -- "$$stage/test-tools"; \
	GOTOOLCHAIN=local CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -buildvcs=false -ldflags "-s -w" \
		-o "$$stage/test-tools/ipgw-live-gate-linux-amd64" ./internal/cmd/ipgw-live-gate; \
	GOTOOLCHAIN=local CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOOS=windows GOARCH=amd64 \
		go build -trimpath -buildvcs=false -ldflags "-s -w" \
		-o "$$stage/test-tools/ipgw-live-gate-windows-amd64.exe" ./internal/cmd/ipgw-live-gate; \
	output_count=0; \
	for target in $(PLATFORMS); do \
		suffix=; \
		case "$$target" in windows-*) suffix=.exe;; esac; \
		for binary in $(BINARIES); do \
			output="$$stage/$$target/$$binary$$suffix"; \
			if [ ! -f "$$output" ] || [ -L "$$output" ] || [ ! -s "$$output" ]; then \
				echo "candidate build did not produce a non-empty regular file: $$target/$$binary$$suffix" >&2; \
				exit 1; \
			fi; \
			output_count=$$((output_count + 1)); \
		done; \
	done; \
	for helper in ipgw-live-gate-linux-amd64 ipgw-live-gate-windows-amd64.exe; do \
		output="$$stage/test-tools/$$helper"; \
		if [ ! -f "$$output" ] || [ -L "$$output" ] || [ ! -s "$$output" ]; then \
			echo "candidate build did not produce a non-empty regular file: test-tools/$$helper" >&2; \
			exit 1; \
		fi; \
		output_count=$$((output_count + 1)); \
	done; \
	actual_count=$$(find "$$stage" -type f | wc -l | tr -d '[:space:]'); \
	if [ "$$output_count" -ne 20 ] || [ "$$actual_count" -ne 20 ]; then \
		echo "candidate build must contain exactly 20 outputs" >&2; \
		exit 1; \
	fi; \
	if [ -e "$$build_dir" ] || [ -L "$$build_dir" ]; then \
		echo "candidate BUILD_DIR appeared during build; refusing to overwrite it" >&2; \
		exit 1; \
	fi; \
	mv -nT -- "$$stage" "$$build_dir"; \
	if [ -d "$$stage" ]; then \
		echo "candidate BUILD_DIR appeared during publish; refusing to overwrite it" >&2; \
		exit 1; \
	fi; \
	stage=; \
	trap - EXIT HUP INT TERM

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
