.PHONY: all help bootstrap bootstrap-force hooks-ensure tools sync dependencies verify-dependencies version version-set version-bump-major version-bump-minor version-bump-patch
.PHONY: check lint test build install build-all package package-sign release-verify-keys verify-release-key clean fmt fmt-check check-all precommit prepush pr-final license-audit license-check license-check-test content-check
.PHONY: release-check release-prepare release-build doctor validate-app-identity
.PHONY: sync-embedded-identity verify-embedded-identity test-standalone-binary cdrl-verify test-cdrl-verify
.PHONY: verify-toolchain
.PHONY: release-guard-tag-name release-guard-tag-version release-guard-signing-tag release-guard-release-surfaces sync-changelog-links
.PHONY: release-prepare-tag-message release-tag release-push-tag release-verify-tag release-verify-published-tag release-verify-remote-tag
.PHONY: release-export-pin release-validate-pin release-insert-anchors release-github-preflight release-preflight
.PHONY: release-tooling-test release-tag-tests release-anchors-check surface-scan surface-scan-release

# Binary and version information
BINARY_NAME := docpresser
BINARY_EXT :=
ifeq ($(OS),Windows_NT)
	BINARY_EXT := .exe
endif
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Go related variables
GOCMD := go
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GO_MOD_VERSION := $(shell sed -n 's/^go //p' go.mod 2>/dev/null)
LINT_GOTOOLCHAIN := $(if $(filter local,$(GOTOOLCHAIN)),local,auto)
export GOTOOLCHAIN ?= $(if $(GO_MOD_VERSION),go$(GO_MOD_VERSION),auto)

# Build static binaries with cgo disabled. The coordinator binary imports no C
# parser libraries (libyaml, libxml2, expat); forcing CGO_ENABLED := 0 overrides
# any container environment preset.
export CGO_ENABLED := 0

# Tool installation (user-space bin dir; overridable with BINDIR=...)
UNAME_S_RAW := $(shell uname -s 2>/dev/null || echo unknown)
IS_WINDOWS_SHELL := $(filter MINGW% MSYS% CYGWIN%,$(UNAME_S_RAW))

ifeq ($(strip $(IS_WINDOWS_SHELL)),)
DEFAULT_BINDIR := $(if $(HOME),$(HOME)/.local/bin,./bin)
else
CYGPATH_BIN := $(shell command -v cygpath 2>/dev/null)
ifeq ($(strip $(USERPROFILE)),)
DEFAULT_BINDIR := $(if $(HOME),$(HOME)/bin,./bin)
else
DEFAULT_BINDIR := $(if $(CYGPATH_BIN),$(shell cygpath -u "$(USERPROFILE)")/bin,$(USERPROFILE)/bin)
endif
endif

BINDIR ?= $(DEFAULT_BINDIR)

# Embedded identity mirror (build artifact contract)
EMBEDDED_IDENTITY_SRC := .fulmen/app.yaml
EMBEDDED_IDENTITY_DST := internal/assets/appidentity/app.yaml

# Tool versions
GONEAT_VERSION ?= v0.6.8
SFETCH_BIN := $(shell command -v sfetch 2>/dev/null)
GONEAT_BIN = $(firstword $(wildcard $(BINDIR)/goneat$(BINARY_EXT)) $(shell command -v goneat 2>/dev/null))

# Default target
all: fmt test

help:  ## Show this help message
	@printf "%b" '$(BINARY_NAME) - Available Make Targets\n\nRequired targets (Fulmen Makefile Standard):\n  help                     - Show this help message\n  bootstrap                - Install external tools (goneat) and dependencies\n  bootstrap-force          - Force reinstall external tools\n  tools                    - Verify external tools are available\n  sync                     - Compatibility target (embedded assets are explicitly pinned)\n  sync-embedded-identity   - Sync embedded identity mirror (.fulmen → internal/assets)\n  verify-embedded-identity - Verify embedded identity mirror is in sync\n  dependencies             - Generate SBOM for supply-chain security\n  license-audit            - Audit dependency licenses\n  license-check            - Verify verbatim LICENSE-MIT, LICENSE-APACHE, and NOTICE\n  content-check            - Verify committed files contain no local paths, keys, or tokens\n  lint                     - Run lint/format/style checks\n  test                     - Run all tests\n  build                    - Build distributable artifacts\n  install                  - Install binary to BINDIR (default: ~/.local/bin)\n  build-all                - Build multi-platform binaries\n  test-standalone-binary   - Verify built binary works outside repo\n  clean                    - Remove build artifacts and caches\n  fmt                      - Format code\n  fmt-check                - Check formatting without mutating (CI gate)\n  version                  - Print current version\n  version-set              - Set version to specific value (VERSION=x.y.z)\n  version-bump-major       - Bump major version\n  version-bump-minor       - Bump minor version\n  version-bump-patch       - Bump patch version\n  release-check            - Run release checklist validation\n  release-prepare          - Prepare for release\n  release-build            - Build release artifacts\n  release-preflight        - Pre-tag release gate (see RELEASE_CHECKLIST.md)\n  release-tooling-test     - Run release tooling test suites (throwaway keys)\n  release-anchors-check    - Check committed public pin against fingerprint anchors\n  release-guard-release-surfaces - Verify CHANGELOG.md matches VERSION\n  sync-changelog-links     - Sync CHANGELOG compare-link footers to VERSION\n  check                    - Complete non-mutating validation gate\n  check-all                - Run all quality checks (fmt, lint, test)\n  precommit                - Run pre-commit hooks\n  prepush                  - Run pre-push hooks\n  pr-final                 - Non-mutating PR gate\n\nIdentity and source checks:\n  doctor                   - Run foundation checks\n  validate-app-identity    - Validate app identity configuration\n  cdrl-verify              - Fast residue detector (fails on template strings)\n'

bootstrap:  ## Install external tools via sfetch + goneat
	@echo "Installing external tools..."
	@if [ -z "$(SFETCH_BIN)" ]; then echo "❌ sfetch not found (required trust anchor)"; exit 1; else echo "✅ sfetch found: $$($(SFETCH_BIN) --version 2>&1 | head -n1)"; $(SFETCH_BIN) --self-verify; fi
	@mkdir -p "$(BINDIR)"; if [ "$(FORCE)" = "1" ] || [ "$(FORCE)" = "true" ]; then rm -f "$(BINDIR)/goneat$(BINARY_EXT)"; fi; if [ -x "$(BINDIR)/goneat$(BINARY_EXT)" ] && "$(BINDIR)/goneat$(BINARY_EXT)" --version 2>/dev/null | grep -q "$(GONEAT_VERSION)"; then echo "→ goneat already available at $(BINDIR)/goneat$(BINARY_EXT)"; else echo "→ Installing goneat $(GONEAT_VERSION) to $(BINDIR) via sfetch..."; $(SFETCH_BIN) --repo fulmenhq/goneat --tag $(GONEAT_VERSION) --dest-dir "$(BINDIR)"; fi
	@for scope in bootstrap foundation lint security format; do \
		$(GONEAT_BIN) doctor tools --scope $$scope --install --yes \
			|| echo "⚠️  scope '$$scope' had tools unavailable on this platform (continuing)"; \
	done
	@echo "→ Download Go module dependencies..." && go mod download && go mod tidy && $(MAKE) hooks-ensure && echo "✅ Bootstrap completed. Ensure '$(BINDIR)' is on your PATH"

bootstrap-force:  ## Force reinstall external tools
	@$(MAKE) bootstrap FORCE=1

hooks-ensure:  ## Ensure git hooks are installed (idempotent)
	@if [ -d ".git" ] && [ -n "$(GONEAT_BIN)" ] && [ ! -x ".git/hooks/pre-commit" ]; then \
		echo "🔗 Installing git hooks with goneat..."; \
		$(GONEAT_BIN) hooks install 2>/dev/null || true; \
	fi

tools:  ## Verify external tools are available
	@echo "Verifying external tools..."
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "✅ goneat: $$($(GONEAT_BIN) --version 2>&1 | head -n1)"
	@echo "✅ Tool verification completed"

sync:  ## Compatibility target (embedded assets are explicitly pinned)
	@echo "⚠️  $(BINARY_NAME) microtool does not consume SSOT assets directly"
	@echo "✅ Sync target satisfied (no-op)"

sync-embedded-identity:  ## Copy .fulmen/app.yaml → internal/assets/appidentity/app.yaml
	@mkdir -p "$(dir $(EMBEDDED_IDENTITY_DST))"
	@cp "$(EMBEDDED_IDENTITY_SRC)" "$(EMBEDDED_IDENTITY_DST)"
	@echo "✅ Synced embedded identity mirror: $(EMBEDDED_IDENTITY_DST)"

verify-embedded-identity:  ## Fail if embedded identity mirror is out of sync
	@if [ ! -f "$(EMBEDDED_IDENTITY_SRC)" ]; then echo "❌ Missing $(EMBEDDED_IDENTITY_SRC)"; exit 1; fi
	@if [ ! -f "$(EMBEDDED_IDENTITY_DST)" ]; then echo "❌ Missing $(EMBEDDED_IDENTITY_DST) (run: make sync-embedded-identity)"; exit 1; fi
	@if ! cmp -s "$(EMBEDDED_IDENTITY_SRC)" "$(EMBEDDED_IDENTITY_DST)"; then \
		echo "❌ Embedded identity is out of sync."; \
		echo "   Run: make sync-embedded-identity (then commit the updated mirror)"; \
		diff -u "$(EMBEDDED_IDENTITY_SRC)" "$(EMBEDDED_IDENTITY_DST)" || true; \
		exit 1; \
	fi
	@echo "✅ Embedded identity mirror is in sync"

dependencies:  ## Generate SBOM for supply-chain security
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "Generating Software Bill of Materials (SBOM)..."; mkdir -p sbom; $(GONEAT_BIN) dependencies --sbom --sbom-output sbom/$(BINARY_NAME).cdx.json; echo "✅ SBOM generated at sbom/$(BINARY_NAME).cdx.json"

verify-dependencies:  ## Alias for dependencies (compatibility)
	@$(MAKE) dependencies

license-audit:  ## Audit dependency licenses (goneat — enforces .goneat/dependencies.yaml)
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "🧪 Auditing dependency licenses (goneat dependencies)..."
	@$(GONEAT_BIN) dependencies --licenses --fail-on high
	@echo "✅ License audit passed"

license-check:  ## Verify verbatim LICENSE-MIT, LICENSE-APACHE, and NOTICE
	@python3 -I -B ./scripts/license-check.py

license-check-test:  ## Run negative controls for license-check
	@./scripts/license-check.test.sh

content-check:  ## Verify committed files contain no local paths, keys, tokens, or commit markers
	@if git rev-parse --verify HEAD >/dev/null 2>&1; then \
		python3 -I -B ./scripts/content-check.py; \
	else \
		echo "ℹ️  Skipping committed-blob content-check before initial commit"; \
	fi

version:  ## Print current version
	@echo "$(VERSION)"

version-set:  ## Set version to specific value (usage: make version-set VERSION=x.y.z)
	@if [ -z "$(VERSION)" ]; then \
		echo "❌ VERSION not specified. Usage: make version-set VERSION=x.y.z"; \
		exit 1; \
	fi
	@echo "$(VERSION)" > VERSION
	@echo "✅ Version set to $(VERSION)"

version-bump-major:  ## Bump major version
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@$(GONEAT_BIN) version bump major
	@echo "✅ Version bumped to $$(cat VERSION)"

version-bump-minor:  ## Bump minor version
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@$(GONEAT_BIN) version bump minor
	@echo "✅ Version bumped to $$(cat VERSION)"

version-bump-patch:  ## Bump patch version
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@$(GONEAT_BIN) version bump patch
	@echo "✅ Version bumped to $$(cat VERSION)"

release-check:  ## Run release checklist validation
	@echo "Running release checklist..."
	@$(MAKE) check
	@echo "✅ Release check passed"

release-prepare:  ## Prepare for release (tests, checks)
	@echo "Preparing release..."
	@$(MAKE) check
	@echo "✅ Release preparation complete"

release-build: build-all  ## Build release artifacts (binaries + checksums)
	@echo "✅ Release build complete"

# Signed-tag release ceremony. Releases are source-only signed tags; see
# RELEASE_CHECKLIST.md. Flow: release-preflight -> release-prepare-tag-message
# -> release-tag (local, signed, verified) -> release-push-tag ->
# release-verify-published-tag.

release-github-preflight:  ## Verify remote GitHub Tag Publish Protection ruleset and immutable releases
	@./scripts/release-github-preflight.sh

release-preflight:  ## Pre-tag gate: clean synced main, changelog, pins, dates, and release surface scan
	@./scripts/release-verify-committed-anchors.sh
	@$(MAKE) surface-scan-release
	@./scripts/release-preflight.sh

release-guard-tag-name:  ## Guard: DOCPRESSER_RELEASE_TAG set and canonical vX.Y.Z (no VERSION check)
	@if [ -z "$(DOCPRESSER_RELEASE_TAG)" ]; then \
		echo "❌ DOCPRESSER_RELEASE_TAG not set (example: DOCPRESSER_RELEASE_TAG=v$(VERSION))." >&2; \
		exit 1; \
	fi
	@printf '%s\n' "$(DOCPRESSER_RELEASE_TAG)" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$' || { \
		echo "❌ DOCPRESSER_RELEASE_TAG must be canonical vX.Y.Z" >&2; exit 1; }
	@echo "[ok] release tag name $(DOCPRESSER_RELEASE_TAG) is canonical"

release-guard-tag-version:  ## Guard: DOCPRESSER_RELEASE_TAG canonical, matches VERSION and app identity
	@if [ -z "$(DOCPRESSER_RELEASE_TAG)" ]; then \
		echo "❌ DOCPRESSER_RELEASE_TAG not set (example: DOCPRESSER_RELEASE_TAG=v$(VERSION))." >&2; \
		exit 1; \
	fi
	@DOCPRESSER_RELEASE_TAG="$(DOCPRESSER_RELEASE_TAG)" ./scripts/release-guard-tag-version.sh
	@APP_VERSION="$$(awk -F': ' '/^[[:space:]]*version:/ {gsub(/"/, "", $$2); print $$2; exit}' .fulmen/app.yaml)"; \
	if [ "$$APP_VERSION" != "$(VERSION)" ]; then \
		echo "❌ App version mismatch: $$APP_VERSION != $(VERSION) (VERSION file)" >&2; \
		exit 1; \
	fi

release-guard-signing-tag: release-guard-tag-version  ## Guard: DOCPRESSER_RELEASE_TAG matches VERSION and app identity
	@:

release-guard-release-surfaces:  ## Verify CHANGELOG.md section and compare links match VERSION
	@./scripts/release-guard-release-surfaces.sh

sync-changelog-links:  ## Sync CHANGELOG compare-link footers to VERSION
	@./scripts/sync-changelog-links.sh

# Signed version tag (maintainer machine only; see RELEASE_CHECKLIST.md). Uses
# DOCPRESSER_RELEASE_TAG, DOCPRESSER_TAG_MESSAGE_DIR, DOCPRESSER_TAGGER_NAME,
# DOCPRESSER_TAGGER_EMAIL, DOCPRESSER_GPG_SIGNING_FINGERPRINT,
# DOCPRESSER_PGP_KEY_ID (<subkey>!), DOCPRESSER_GPG_HOMEDIR and
# DOCPRESSER_DECERNOR_BIN.

release-prepare-tag-message:  ## Maintainer-only: prepare and review the external per-cut tag message
	@./scripts/release-tag-operator.sh prepare-message

release-tag:  ## Maintainer-only: create and verify a local signed version tag (no push)
	@./scripts/release-tag-operator.sh local-tag

release-push-tag:  ## Maintainer-only: publish and verify the signed version tag
	@./scripts/release-tag-operator.sh remote-push

release-verify-tag:  ## Verify the local tag using only the committed public pin
	@./scripts/release-verify-tag.sh

# The published-tag check installs the remote tag as the local ref only after
# every check passes.
release-verify-published-tag: release-guard-tag-name  ## Verify the published tag object and signature against its committed pin
	@DOCPRESSER_RELEASE_TAG="$(DOCPRESSER_RELEASE_TAG)" ./scripts/release-verify-published-tag.sh

release-verify-remote-tag:  ## Compare local and remote tag objects and GitHub verification
	@./scripts/release-verify-remote-tag.sh

release-export-pin:  ## Maintainer-only: export the approved public key when no pin exists
	@./scripts/release-export-pin.sh

release-validate-pin:  ## Maintainer-only: read-only validation of the existing public pin
	@./scripts/release-validate-pin.sh

release-insert-anchors: release-validate-pin  ## Maintainer-only: validate pin, then generate absent public anchors
	@./scripts/release-insert-anchors.sh

release-anchors-check:  ## Check the committed public key against both fingerprint anchors (needs gpg, DOCPRESSER_DECERNOR_BIN)
	@./scripts/release-verify-committed-anchors.sh
	@./scripts/validate-release-anchors.sh

release-tooling-test:  ## Run all release and surface-hygiene tooling test suites
	@./scripts/release-tooling-test.sh

release-tag-tests: release-tooling-test  ## Signed-tag tooling tests (throwaway keys only; needs gpg, git, python3, jq, make, decernor)
	@echo "✅ Signed-tag tooling tests passed"

surface-scan: content-check  ## Run two-tier public-surface hygiene checks
	@./scripts/surface-scan.sh

surface-scan-release: content-check  ## Run release-mode public-surface hygiene checks (requires external limensafe catalog)
	@DOCPRESSER_SURFACE_SCAN_REQUIRED=1 DOCPRESSER_SURFACE_SCOPE=release ./scripts/surface-scan.sh

build: verify-embedded-identity  ## Build binary for current platform
	@echo "→ Building $(BINARY_NAME) v$(VERSION)..."
	@mkdir -p bin
	@go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)$(BINARY_EXT) ./cmd/$(BINARY_NAME)
	@echo "✓ Binary built: bin/$(BINARY_NAME)$(BINARY_EXT)"

install: build  ## Install binary to BINDIR (default: ~/.local/bin)
	@echo "→ Installing $(BINARY_NAME) to $(BINDIR)..."
	@mkdir -p "$(BINDIR)"
	@cp bin/$(BINARY_NAME)$(BINARY_EXT) "$(BINDIR)/$(BINARY_NAME)$(BINARY_EXT)"
	@echo "✓ Installed: $(BINDIR)/$(BINARY_NAME)$(BINARY_EXT)"

test-standalone-binary: build  ## Verify built binary runs outside repo
	@echo "→ Standalone binary check (outside repo)..."
	@set -eu; standalone_dir=$$(mktemp -d "/tmp/$(BINARY_NAME).XXXXXX"); \
	trap 'rm -rf "$$standalone_dir"' EXIT; \
	mkdir -p "$$standalone_dir/home" "$$standalone_dir/tmp"; \
	cp "bin/$(BINARY_NAME)$(BINARY_EXT)" "$$standalone_dir/$(BINARY_NAME)$(BINARY_EXT)"; \
	cd "$$standalone_dir"; \
	cleanenv() { env -i PATH="$$PATH" HOME="$$standalone_dir/home" TMPDIR="$$standalone_dir/tmp" "$$@"; }; \
	cleanenv "./$(BINARY_NAME)$(BINARY_EXT)" version --extended > version.stdout 2> version.stderr; \
	test ! -s version.stderr; \
	grep -qx 'Application:     $(BINARY_NAME)' version.stdout; \
	grep -qx 'Go Version:      go$(GO_MOD_VERSION)' version.stdout; \
	grep -qx 'Gofulmen:        v0.3.6' version.stdout; \
	cleanenv "./$(BINARY_NAME)$(BINARY_EXT)" envinfo > envinfo.stdout 2> envinfo.stderr; \
	test ! -s envinfo.stdout; \
	grep -q '=== $(BINARY_NAME) Environment Information ===' envinfo.stderr; \
	cleanenv "./$(BINARY_NAME)$(BINARY_EXT)" doctor > doctor.stdout 2> doctor.stderr; \
	test ! -s doctor.stdout; \
	grep -q '=== $(BINARY_NAME) Doctor ===' doctor.stderr; \
	cleanenv "./$(BINARY_NAME)$(BINARY_EXT)" --help > help.stdout 2> help.stderr; \
	test ! -s help.stderr
	@echo "✅ Standalone binary check passed"

build-all: verify-embedded-identity  ## Build multi-platform binaries and generate checksums
	@echo "→ Building for multiple platforms..."
	@mkdir -p bin
	@echo "Building Linux amd64..."
	@GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/$(BINARY_NAME)
	@echo "Building Darwin arm64..."
	@GOOS=darwin GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/$(BINARY_NAME)
	@echo "Building Windows amd64..."
	@GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/$(BINARY_NAME)
	@echo "Building Windows arm64..."
	@GOOS=windows GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows-arm64.exe ./cmd/$(BINARY_NAME)
	@echo "Building Linux arm64..."
	@GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/$(BINARY_NAME)
	@cd bin && (sha256sum * > SHA256SUMS.txt 2>/dev/null || shasum -a 256 * > SHA256SUMS.txt)
	@echo "✓ Multi-platform binaries built in bin/"

package: build-all  ## Package release archives and checksums to dist/release (no signing)
	@BINARY_NAME=$(BINARY_NAME) ./scripts/package-artifacts.sh

package-sign: build-all  ## Package release archives and sign SHA256SUMS (requires gpg, SIGN=1 optional)
	@SIGN=1 BINARY_NAME=$(BINARY_NAME) ./scripts/package-artifacts.sh

release-verify-keys:  ## Verify exported public key contains no private material (run package-sign with SIGNING_KEY_ID)
	@./scripts/verify-public-key.sh dist/release/$(BINARY_NAME)-release-signing-key.asc

# Deprecated alias (kept for one cycle).
verify-release-key: release-verify-keys  ## Deprecated: use release-verify-keys
	@echo "⚠️  verify-release-key is deprecated; use make release-verify-keys" >&2

test: verify-embedded-identity  ## Run all tests
	@echo "Running test suite..."
	$(GOTEST) ./... -v -cover

lint:  ## Run lint checks with goneat
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "Running Go vet..." && $(GOCMD) vet ./... && echo "Running goneat assess (lint)..." && GOTOOLCHAIN=$(LINT_GOTOOLCHAIN) $(GONEAT_BIN) assess --categories lint --fail-on medium --package-mode && echo "✅ Lint checks passed"

fmt:  ## Format code with goneat
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "Formatting with goneat..." && $(GONEAT_BIN) format && echo "✅ Formatting completed"

fmt-check:  ## Check formatting without mutating (CI gate)
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "Checking formatting with goneat..." && $(GONEAT_BIN) format --check && echo "✅ Formatting is clean"

verify-toolchain:  ## Require the pinned Go and goneat versions
	@expected_go="go$$(sed -n 's/^go //p' go.mod)"; actual_go="$$($(GOCMD) env GOVERSION)" || exit 1; \
		test "$$actual_go" = "$$expected_go" || { echo "Expected $$expected_go; got $$actual_go" >&2; exit 1; }
	@if [ -z "$(GONEAT_BIN)" ]; then echo "goneat not found" >&2; exit 1; fi
	@actual_goneat="$$("$(GONEAT_BIN)" --version)" || exit 1; \
		test "$$actual_goneat" = "goneat $(GONEAT_VERSION)" || { echo "Expected goneat $(GONEAT_VERSION); got $$actual_goneat" >&2; exit 1; }

check: pr-final  ## Complete non-mutating validation gate

check-all: fmt lint test  ## Run all quality checks (mutates formatting)
	@echo "✅ All quality checks passed"

precommit:  ## Run pre-commit hooks (format/lint/security + tests)
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@$(MAKE) verify-embedded-identity
	@$(GOMOD) verify
	@$(MAKE) fmt-check
	@echo "Running pre-commit validation..." && GOTOOLCHAIN=auto $(GONEAT_BIN) assess --categories lint,security --fail-on critical --package-mode
	@$(MAKE) test
	@echo "✅ Pre-commit checks passed"

prepush: license-audit ## Run pre-push hooks (reserved for the tag/pushtag path)
	@if [ -z "$(GONEAT_BIN)" ]; then echo "❌ goneat not found. Run 'make bootstrap' first."; exit 1; fi
	@echo "Running pre-push validation..." && GOTOOLCHAIN=auto $(GONEAT_BIN) assess --categories format,lint,security,dependencies,dates,tools,maturity,repo-status --fail-on high --package-mode && echo "✅ Pre-push checks passed"

pr-final:  ## Non-mutating PR gate (identity + fmt-check + lint + test + license + build-all)
	@echo "🔒 Running pr-final gate (non-mutating)..."
	@$(MAKE) verify-toolchain
	@$(MAKE) validate-app-identity
	@$(MAKE) cdrl-verify
	@$(MAKE) test-cdrl-verify
	@$(MAKE) license-check
	@$(MAKE) license-check-test
	@$(MAKE) content-check
	@$(MAKE) verify-embedded-identity
	@$(GOMOD) verify
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test
	@$(MAKE) license-audit
	@$(MAKE) build-all
	@$(MAKE) test-standalone-binary
	@echo "✅ pr-final gate passed"

clean:  ## Clean build artifacts and reports
	@echo "Cleaning artifacts..."
	@rm -rf bin/ dist/ coverage.out coverage.html sbom/ vendor/
	@echo "✅ Clean completed"

doctor:  ## Run Docpresser foundation checks
	@echo "🏥 Running Docpresser foundation check..." && test -f .fulmen/app.yaml || (echo "❌ Missing .fulmen/app.yaml" && exit 1)
	@which go >/dev/null 2>&1 || (echo "❌ Go not installed" && exit 1)
	@test -f go.mod || (echo "❌ Missing go.mod" && exit 1)
	@$(MAKE) test > /dev/null 2>&1 || (echo "❌ Tests failed" && exit 1)
	@echo "🎉 Docpresser foundation is ready for development."

cdrl-verify:  ## Fast residue detector for CDRL
	@./scripts/cdrl-verify.sh .

test-cdrl-verify:  ## Test hidden-file coverage without touching the worktree
	@./scripts/test-cdrl-verify.sh

validate-app-identity: cdrl-verify  ## Validate Docpresser application identity

.DEFAULT_GOAL := help
