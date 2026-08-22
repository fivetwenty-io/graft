# graft Makefile
SHELL := /bin/bash
.DEFAULT_GOAL := help

# Colors for terminal output
GREEN  := \033[1;32m
YELLOW := \033[1;33m
BLUE   := \033[1;34m
CYAN   := \033[1;36m
RESET  := \033[0m

# Variables
BINARY_NAME := graft
SOURCE_DIR := ./cmd/graft
BUILD_DIR := build
# Prefer an exact tag match for a clean semver release build. Fall back to
# a fixed baseline (kept in sync with cmd/graft/main.go's default Version)
# rather than a commit hash, since genesis's check_prereqs() requires the
# `-v`/`--version` output to contain a semver token >= 1.28.0.
VERSION := $(shell (git describe --tags --exact-match 2>/dev/null || echo "v1.33.0") | sed 's/^v//')
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
GO_FILES := $(shell find . -name '*.go' -not -path "./vendor/*")
COVERAGE_DIR := coverage
COVERAGE_FILE := $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML := $(COVERAGE_DIR)/coverage.html
INSTALL_PATH ?= /usr/local/bin
# CI's Lint job pins both of these (.github/workflows/ci.yml); keep them in
# sync so a local `make golangci` reproduces CI byte-for-byte. The
# GOTOOLCHAIN pin also matters locally on its own: golangci-lint is built
# with go1.26 and panics type-checking against a newer local toolchain's
# standard library.
GOLANGCI_LINT_VERSION := v2.12.2
LINT_GOTOOLCHAIN := go1.26.6

# Platform detection
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
PLATFORM := $(GOOS)-$(GOARCH)

# Build output paths
BUILD_OUTPUT := $(BUILD_DIR)/$(PLATFORM)/$(BINARY_NAME)
# spruce-named alias binary (drop-in replacement build target; see
# tests/spruce-compat/e2e-genesis-dropin.sh for the consumer of this path).
ALIAS_OUTPUT := $(BUILD_DIR)/$(PLATFORM)/spruce

# Release platforms
PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64
# Windows is built separately from PLATFORMS because the binary needs a .exe
# suffix and ships as a .zip rather than a .tar.gz.
WINDOWS_PLATFORMS := windows-amd64
CHECKSUM_FILE := $(BINARY_NAME)-$(VERSION)-checksums.sha256

# Phony targets
.PHONY: help build build-linux build-windows build-release build-spruce-alias package checksums clean install
.PHONY: test test-unit test-clean test-verbose test-race test-short test-all test-spruce-compat test-spruce-e2e
.PHONY: fmt vet lint security gosec vuln trivy coverage coverage-text
.PHONY: staticcheck gocyclo ineffassign errcheck goimports deadcode golangci
.PHONY: check check-all
.PHONY: bench bench-full
.PHONY: deps deps-update deps-tidy check-prereq ci install-tools
.PHONY: hooks hooks-install hooks-uninstall hooks-check pre-commit pre-push
.PHONY: validate-imports validate-imports-example

##@ General

help: ## Show this help message
	@echo -e "\033[33mgraft\033[0m - Available \`make T\` (T)argets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[32m\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""
	@echo -e "\033[32mVersion:\033[0m \033[33m$(VERSION)\033[0m"

##@ Build & Package

build: ## Build the graft binary for current platform
	@printf "$(GREEN)Building graft for $(PLATFORM)...$(RESET)\n"
	@mkdir -p $(BUILD_DIR)/$(PLATFORM)
	@go build $(LDFLAGS) -o $(BUILD_OUTPUT) $(SOURCE_DIR)
	@printf "$(GREEN)✓ Built: $(BUILD_OUTPUT)$(RESET)\n"

build-spruce-alias: build ## Build a spruce-named alias binary from graft for drop-in replacement testing
	@printf "$(GREEN)Building spruce-named alias binary for $(PLATFORM)...$(RESET)\n"
	@cp $(BUILD_OUTPUT) $(ALIAS_OUTPUT)
	@chmod +x $(ALIAS_OUTPUT)
	@printf "$(GREEN)✓ Built: $(ALIAS_OUTPUT)$(RESET)\n"
	@$(ALIAS_OUTPUT) -v

build-linux: ## Build for Linux (amd64 and arm64)
	@printf "$(GREEN)Building graft for Linux...$(RESET)\n"
	@mkdir -p $(BUILD_DIR)/linux-amd64
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/$(BINARY_NAME) $(SOURCE_DIR)
	@printf "$(GREEN)✓ Built: $(BUILD_DIR)/linux-amd64/$(BINARY_NAME)$(RESET)\n"
	@mkdir -p $(BUILD_DIR)/linux-arm64
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-arm64/$(BINARY_NAME) $(SOURCE_DIR)
	@printf "$(GREEN)✓ Built: $(BUILD_DIR)/linux-arm64/$(BINARY_NAME)$(RESET)\n"

build-windows: ## Build for Windows (amd64)
	@printf "$(GREEN)Building graft for Windows...$(RESET)\n"
	@mkdir -p $(BUILD_DIR)/windows-amd64
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/$(BINARY_NAME).exe $(SOURCE_DIR)
	@printf "$(GREEN)✓ Built: $(BUILD_DIR)/windows-amd64/$(BINARY_NAME).exe$(RESET)\n"

build-release: ## Build for all release platforms
	@printf "$(GREEN)Building graft for all platforms...$(RESET)\n"
	@for platform in $(PLATFORMS); do \
		os=$${platform%%-*}; \
		arch=$${platform##*-}; \
		printf "Building for $$os/$$arch...\n"; \
		mkdir -p $(BUILD_DIR)/$$platform; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $(BUILD_DIR)/$$platform/$(BINARY_NAME) $(SOURCE_DIR); \
	done
	@for platform in $(WINDOWS_PLATFORMS); do \
		os=$${platform%%-*}; \
		arch=$${platform##*-}; \
		printf "Building for $$os/$$arch...\n"; \
		mkdir -p $(BUILD_DIR)/$$platform; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $(BUILD_DIR)/$$platform/$(BINARY_NAME).exe $(SOURCE_DIR); \
	done
	@printf "$(GREEN)✓ Release builds complete$(RESET)\n"
	@ls -la $(BUILD_DIR)/*/$(BINARY_NAME) $(BUILD_DIR)/*/$(BINARY_NAME).exe

package: clean build-release ## Clean, build all platforms, archive, and checksum for release
	@printf "$(GREEN)Packaging graft...$(RESET)\n"
	@for platform in $(PLATFORMS); do \
		printf "Creating archive for $$platform...\n"; \
		tar -czf $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-$$platform.tar.gz -C $(BUILD_DIR)/$$platform $(BINARY_NAME); \
	done
	@for platform in $(WINDOWS_PLATFORMS); do \
		printf "Creating archive for $$platform...\n"; \
		(cd $(BUILD_DIR)/$$platform && zip -q ../$(BINARY_NAME)-$(VERSION)-$$platform.zip $(BINARY_NAME).exe); \
	done
	@$(MAKE) --no-print-directory checksums
	@printf "$(GREEN)✓ Packages ready in $(BUILD_DIR)/$(RESET)\n"
	@ls -la $(BUILD_DIR)/*.tar.gz $(BUILD_DIR)/*.zip $(BUILD_DIR)/$(CHECKSUM_FILE)

checksums: ## Generate SHA256 checksums for the packaged release artifacts
	@printf "$(GREEN)Generating SHA256 checksums...$(RESET)\n"
	@set -eo pipefail; cd $(BUILD_DIR) 2>/dev/null || { \
		printf "$(YELLOW)No $(BUILD_DIR)/ directory - run 'make package' first$(RESET)\n" >&2; \
		exit 1; \
	}; \
	shopt -s nullglob; \
	artifacts=($(BINARY_NAME)-$(VERSION)-*.tar.gz $(BINARY_NAME)-$(VERSION)-*.zip); \
	if [ $${#artifacts[@]} -eq 0 ]; then \
		printf "$(YELLOW)No $(BINARY_NAME)-$(VERSION)-* archives in $(BUILD_DIR)/ - run 'make package' first$(RESET)\n" >&2; \
		exit 1; \
	fi; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum "$${artifacts[@]}" > $(CHECKSUM_FILE); \
	else \
		shasum -a 256 "$${artifacts[@]}" > $(CHECKSUM_FILE); \
	fi
	@printf "$(GREEN)✓ Checksums: $(BUILD_DIR)/$(CHECKSUM_FILE)$(RESET)\n"

install: build ## Install graft binary to INSTALL_PATH (default: /usr/local/bin)
	@printf "$(GREEN)Installing graft to $(INSTALL_PATH)...$(RESET)\n"
	@mkdir -p $(INSTALL_PATH)
	@cp $(BUILD_OUTPUT) $(INSTALL_PATH)/$(BINARY_NAME)
	@printf "$(GREEN)✓ graft installed to $(INSTALL_PATH)/$(BINARY_NAME)$(RESET)\n"

clean: ## Remove build artifacts and binaries
	@printf "$(YELLOW)Cleaning...$(RESET)\n"
	@rm -f $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/
	@rm -rf $(COVERAGE_DIR)
	@rm -f *.test
	@printf "$(GREEN)✓ Clean complete$(RESET)\n"

##@ Testing

test: test-unit ## Run all unit tests (alias for test-unit)

test-unit: ## Run Go unit tests with coverage
	@printf "$(GREEN)Running unit tests...$(RESET)\n"
	@mkdir -p $(COVERAGE_DIR)
	@go test -v -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@printf "$(GREEN)✓ Unit tests complete$(RESET)\n"

test-clean: ## Run tests without linker warnings (CGO disabled)
	@printf "$(GREEN)Running tests with CGO disabled...$(RESET)\n"
	@CGO_ENABLED=0 go test ./...
	@printf "$(GREEN)✓ Clean tests complete$(RESET)\n"

test-verbose: ## Run tests with verbose output
	@printf "$(GREEN)Running tests (verbose)...$(RESET)\n"
	@go test -v ./...
	@printf "$(GREEN)✓ Verbose tests complete$(RESET)\n"

test-race: ## Run tests with Go race detector enabled
	@printf "$(GREEN)Running tests with race detector...$(RESET)\n"
	@go test -race ./...
	@printf "$(GREEN)✓ Race tests complete$(RESET)\n"

test-short: ## Run tests in short mode
	@printf "$(GREEN)Running short tests...$(RESET)\n"
	@go test -short ./...
	@printf "$(GREEN)✓ Short tests complete$(RESET)\n"

test-spruce-compat: ## Run the full spruce/graft parity suite: golden-output harness, operator matrix, vaultinfo pipefail (golden/operator suites skip gracefully if spruce is unavailable; vaultinfo pipefail always runs, needs only graft)
	@printf "$(GREEN)Running spruce/graft golden-output parity harness...$(RESET)\n"
	@bash tests/spruce-compat/run.sh
	@printf "$(GREEN)Running spruce/graft operator parity suite...$(RESET)\n"
	@bash tests/spruce-compat/operators/run-operators.sh
	@printf "$(GREEN)Running vaultinfo pipefail pipeline test...$(RESET)\n"
	@bash tests/spruce-compat/vaultinfo-pipefail.sh
	@printf "$(GREEN)✓ Parity suite complete$(RESET)\n"

test-spruce-e2e: build-spruce-alias ## Run end-to-end genesis drop-in validation against the spruce-named alias binary
	@printf "$(GREEN)Running genesis drop-in end-to-end validation...$(RESET)\n"
	@bash tests/spruce-compat/e2e-genesis-dropin.sh
	@printf "$(GREEN)✓ Genesis drop-in validation complete$(RESET)\n"

test-all: vet test-unit test-race test-spruce-compat ## Run all test targets

##@ Code Quality

fmt: ## Format all Go source files using gofmt
	@printf "$(GREEN)Formatting code...$(RESET)\n"
	@gofmt -w $(GO_FILES)
	@printf "$(GREEN)✓ Code formatted$(RESET)\n"

vet: ## Run go vet for static analysis
	@printf "$(GREEN)Running go vet...$(RESET)\n"
	@go vet ./...
	@printf "$(GREEN)✓ Vet complete$(RESET)\n"

lint: fmt vet ## Run fmt and vet

staticcheck: ## Run staticcheck static analysis
	@printf "$(GREEN)Running staticcheck...$(RESET)\n"
	@command -v staticcheck >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing staticcheck...$(RESET)\n"; \
		go install honnef.co/go/tools/cmd/staticcheck@latest; \
	}
	@staticcheck ./...
	@printf "$(GREEN)✓ Staticcheck complete$(RESET)\n"

gocyclo: ## Run gocyclo complexity analysis
	@printf "$(GREEN)Running gocyclo complexity analysis...$(RESET)\n"
	@command -v gocyclo >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing gocyclo...$(RESET)\n"; \
		go install github.com/fzipp/gocyclo/cmd/gocyclo@latest; \
	}
	@gocyclo -over 15 $(shell find . -name '*.go' -type f -not -path "./vendor/*")
	@printf "$(GREEN)✓ Gocyclo complete$(RESET)\n"

ineffassign: ## Run ineffassign to detect ineffectual assignments
	@printf "$(GREEN)Running ineffassign...$(RESET)\n"
	@command -v ineffassign >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing ineffassign...$(RESET)\n"; \
		go install github.com/gordonklaus/ineffassign@latest; \
	}
	@ineffassign ./...
	@printf "$(GREEN)✓ Ineffassign complete$(RESET)\n"

errcheck: ## Run errcheck to find unchecked errors
	@printf "$(GREEN)Running errcheck...$(RESET)\n"
	@command -v errcheck >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing errcheck...$(RESET)\n"; \
		go install github.com/kisielk/errcheck@latest; \
	}
	@errcheck -ignoretests ./...
	@printf "$(GREEN)✓ Errcheck complete$(RESET)\n"

goimports: ## Run goimports to check import formatting
	@printf "$(GREEN)Running goimports...$(RESET)\n"
	@command -v goimports >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing goimports...$(RESET)\n"; \
		go install golang.org/x/tools/cmd/goimports@latest; \
	}
	@goimports -l $(shell find . -name '*.go' -type f -not -path "./vendor/*") | (! grep . || (printf "$(YELLOW)Files need goimports formatting$(RESET)\n" && false))
	@printf "$(GREEN)✓ Goimports check complete$(RESET)\n"

deadcode: ## Run deadcode to find unused code
	@printf "$(GREEN)Running deadcode analysis...$(RESET)\n"
	@command -v deadcode >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing deadcode...$(RESET)\n"; \
		go install golang.org/x/tools/cmd/deadcode@latest; \
	}
	@deadcode -test ./... || true
	@printf "$(GREEN)✓ Deadcode analysis complete$(RESET)\n"

golangci: ## Run golangci-lint at CI's pinned version and toolchain
	@printf "$(GREEN)Running golangci-lint $(GOLANGCI_LINT_VERSION)...$(RESET)\n"
	@golangci-lint version 2>/dev/null | grep -q "version $(patsubst v%,%,$(GOLANGCI_LINT_VERSION)) " || { \
		printf "$(YELLOW)Installing golangci-lint $(GOLANGCI_LINT_VERSION)...$(RESET)\n"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	}
	@GOTOOLCHAIN=$(LINT_GOTOOLCHAIN) golangci-lint run ./...
	@printf "$(GREEN)✓ Golangci-lint complete$(RESET)\n"

##@ Security

security: gosec vuln trivy ## Run all security checks (gosec, govulncheck, trivy)

gosec: ## Run gosec security scanner
	@printf "$(GREEN)Running gosec...$(RESET)\n"
	@command -v gosec >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing gosec...$(RESET)\n"; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	}
	@gosec -quiet -fmt text ./...
	@printf "$(GREEN)✓ Gosec complete$(RESET)\n"

vuln: ## Run govulncheck for Go vulnerability scanning
	@printf "$(GREEN)Running govulncheck...$(RESET)\n"
	@command -v govulncheck >/dev/null 2>&1 || { \
		printf "$(YELLOW)Installing govulncheck...$(RESET)\n"; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	}
	@govulncheck ./...
	@printf "$(GREEN)✓ Govulncheck complete$(RESET)\n"

trivy: ## Run Trivy container and dependency scanner
	@printf "$(GREEN)Running Trivy scan...$(RESET)\n"
	@command -v trivy >/dev/null 2>&1 || { \
		printf "$(YELLOW)Trivy not found. Please install it:$(RESET)\n"; \
		printf "$(CYAN)  brew install trivy$(RESET) (macOS)\n"; \
		printf "$(CYAN)  apt-get install trivy$(RESET) (Debian/Ubuntu)\n"; \
		printf "$(CYAN)  Or visit: https://aquasecurity.github.io/trivy$(RESET)\n"; \
		exit 1; \
	}
	@trivy fs --scanners vuln,misconfig,secret --severity HIGH,CRITICAL --exit-code 1 --skip-dirs .git --skip-dirs vendor .
	@printf "$(GREEN)✓ Trivy scan complete$(RESET)\n"

##@ Combined Checks

check: lint staticcheck test test-race golangci test-spruce-compat ## Run basic quality checks
	@printf "$(GREEN)✓ Basic checks passed$(RESET)\n"

check-all: lint test-all security staticcheck gocyclo ineffassign errcheck golangci ## Run all quality checks
	@printf "$(GREEN)✓ All checks passed$(RESET)\n"

##@ Coverage

coverage: test-unit ## Generate HTML coverage report
	@printf "$(GREEN)Generating coverage report...$(RESET)\n"
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@printf "$(GREEN)✓ Coverage report: $(COVERAGE_HTML)$(RESET)\n"

coverage-text: test-unit ## Show coverage summary in terminal
	@printf "$(GREEN)Coverage summary:$(RESET)\n"
	@go tool cover -func=$(COVERAGE_FILE)

##@ Performance

bench: ## Run benchmarks
	@printf "$(GREEN)Running benchmarks...$(RESET)\n"
	@go test -bench=. -benchmem ./...
	@printf "$(GREEN)✓ Benchmarks complete$(RESET)\n"

bench-full: ## Run comprehensive benchmarks (10s)
	@printf "$(GREEN)Running comprehensive benchmarks...$(RESET)\n"
	@go test -bench=. -benchmem -benchtime=10s ./...
	@printf "$(GREEN)✓ Benchmarks complete$(RESET)\n"

##@ Dependencies

deps: ## Download and verify Go module dependencies
	@printf "$(GREEN)Downloading dependencies...$(RESET)\n"
	@go mod download
	@go mod verify
	@printf "$(GREEN)✓ Dependencies ready$(RESET)\n"

deps-update: ## Update all dependencies to latest versions
	@printf "$(GREEN)Updating dependencies...$(RESET)\n"
	@go get -u ./...
	@go mod tidy
	@printf "$(GREEN)✓ Dependencies updated$(RESET)\n"

deps-tidy: ## Tidy go.mod and go.sum
	@printf "$(GREEN)Tidying dependencies...$(RESET)\n"
	@go mod tidy
	@printf "$(GREEN)✓ Dependencies tidied$(RESET)\n"

##@ CI/CD

check-prereq: ## Verify all build prerequisites are installed
	@printf "$(GREEN)Checking prerequisites...$(RESET)\n"
	@command -v go >/dev/null 2>&1 || { printf "go is required but not installed.\n"; exit 1; }
	@command -v git >/dev/null 2>&1 || { printf "git is required but not installed.\n"; exit 1; }
	@printf "$(GREEN)✓ All prerequisites installed$(RESET)\n"

ci: vet test-unit build ## Run full CI pipeline (vet, test, build)
	@printf "$(GREEN)✓ CI pipeline completed successfully$(RESET)\n"

install-tools: ## Install all development tools
	@printf "$(GREEN)Installing development tools...$(RESET)\n"
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
	@go install github.com/gordonklaus/ineffassign@latest
	@go install github.com/kisielk/errcheck@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install golang.org/x/tools/cmd/deadcode@latest
	@printf "$(YELLOW)Installing golangci-lint...$(RESET)\n"
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin
	@printf "$(GREEN)✓ Development tools installed$(RESET)\n"
	@printf "$(CYAN)Note: For trivy, install via your package manager (brew install trivy)$(RESET)\n"

##@ Import Validation

validate-imports: ## Run import validation integration tests
	@printf "$(GREEN)Running import validation tests...$(RESET)\n"
	@go test -v -tags=integration -run TestExternalImport ./pkg/graft/...
	@printf "$(GREEN)✓ Import validation complete$(RESET)\n"

validate-imports-example: ## Run standalone import validation example
	@printf "$(GREEN)Running import validation example...$(RESET)\n"
	@go run ./examples/import-validation/main.go
	@printf "$(GREEN)✓ Import validation example complete$(RESET)\n"

##@ Git Hooks

pre-commit: fmt vet build ## Run all pre-commit checks (fmt, vet, build)
	@printf "$(GREEN)✓ All pre-commit checks passed!$(RESET)\n"

# Mirrors CI's push-gating jobs (Lint, Security Scan, Test): golangci at
# the pinned version, trivy with the same scanners and severities, and the
# full test suite. gosec and govulncheck stay in `security`/`check-all`
# for on-demand runs -- CI does not gate on them, and standalone gosec
# does not honor the //nolint annotations golangci-lint does, so gating
# here would block pushes CI would accept.
pre-push: lint golangci trivy test ## Run all pre-push checks (lint, golangci, trivy, test)
	@printf "$(GREEN)✓ All pre-push checks passed!$(RESET)\n"

hooks: hooks-install ## Install git hooks (alias for hooks-install)

hooks-install: ## Point git at the checked-in .githooks hooks
	@git config core.hooksPath .githooks
	@printf "$(GREEN)✓ Git hooks installed successfully$(RESET)\n"
	@printf "$(CYAN)Hooks location: .githooks/$(RESET)\n"
	@printf "  - pre-commit: fmt, vet, build\n"
	@printf "  - pre-push: lint, golangci, trivy, tests\n"

hooks-uninstall: ## Remove git hooks configuration
	@printf "$(YELLOW)Removing git hooks...$(RESET)\n"
	@git config --unset core.hooksPath || true
	@printf "$(GREEN)✓ Git hooks uninstalled$(RESET)\n"

hooks-check: ## Check current git hooks configuration
	@printf "$(CYAN)Current git hooks configuration:$(RESET)\n"
	@printf "Hooks path: "
	@git config core.hooksPath || printf "(default .git/hooks/)\n"
	@if [ -d ".githooks" ]; then \
		printf "Available hooks in .githooks/:\n"; \
		ls -la .githooks/ | grep -E "pre-commit|pre-push" || printf "  No hooks found\n"; \
	else \
		printf "No .githooks directory found\n"; \
	fi
