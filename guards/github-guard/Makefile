.PHONY: all build test test-all test-unit test-wasm test-integration test-integrity-harness capture-integrity-corpus test-copilot test-copilot-yolo test-copilot-all test-copilot-public-only test-copilot-owner-only test-copilot-repo-only test-copilot-prefix-only test-copilot-multi-only test-copilot-lockdown build-sdk test-sdk build-noop test-noop-unit test-noop-wasm test-noop build-agentdojo-guards test-agentdojo-guards clean release help

# Default target
all: build test

# Build the WASM module
build:
	@echo "Building guard..."
	@cd rust-guard && ./build.sh

# Run default test pipeline
test: build test-unit test-wasm test-integration test-integrity-harness
	@echo "✓ Default test pipeline complete"

# Run unit tests
test-unit: build
	@echo "Running unit tests..."
	@cd rust-guard && cargo test --lib

# Run WASM build verification
test-wasm: build
	@echo "✓ WASM build verified"

# Run integration tests (requires Docker and .env file with GitHub token)
test-integration: build
	@echo "Running integration tests..."
	@./scripts/run_integration_tests.sh

# Run corpus-driven WASM integrity tests with replayed backend responses
test-integrity-harness: build
	@echo "Running corpus-driven WASM integrity harness tests..."
	@go test ./src -run IntegrityGroundTruth -v

# Refresh the integrity corpus from live open-source repositories
capture-integrity-corpus:
	@echo "Refreshing integrity corpus fixtures from GitHub API..."
	@./scripts/capture_integrity_corpus.sh

# Run Copilot with MCP Gateway (default: yolo mode)
# Prerequisites:
#   1. Install GitHub Copilot CLI: brew install --cask copilot-cli
#   2. Authenticate GitHub CLI: gh auth login
#   3. Create .env file with GITHUB_TOKEN
test-copilot: build
	@echo "Running Copilot with MCP Gateway Guard (yolo mode)..."
	@DEBUG='*' ./scripts/run_copilot_test.sh

# Copilot test modes
test-copilot-yolo: build
	@echo "Running Copilot with MCP Gateway Guard (yolo mode - no DIFC)..."
	@./scripts/run_copilot_test.sh yolo

test-copilot-all: build
	@echo "Running Copilot with MCP Gateway Guard (all mode, repos=all, min-integrity=approved)..."
	@./scripts/run_copilot_test.sh all

test-copilot-public-only: build
	@echo "Running Copilot with MCP Gateway Guard (public-only mode)..."
	@./scripts/run_copilot_test.sh public-only

test-copilot-owner-only: build
	@echo "Running Copilot with MCP Gateway Guard (owner-only mode, owner=lpcox)..."
	@ALLOW_OWNER=lpcox ./scripts/run_copilot_test.sh owner-only

test-copilot-repo-only: build
	@echo "Running Copilot with MCP Gateway Guard (repo-only mode, repo=lpcox/github-guard)..."
	@./scripts/run_copilot_test.sh repo-only

test-copilot-prefix-only: build
	@echo "Running Copilot with MCP Gateway Guard (prefix-only mode, repos=lpcox/git-*, min-integrity=merged)..."
	@./scripts/run_copilot_test.sh prefix-only

test-copilot-multi-only: build
	@echo "Running Copilot with MCP Gateway Guard (multi-only mode, repos=[lpcox/git-*,lpcox/github-guard], min-integrity=merged)..."
	@./scripts/run_copilot_test.sh multi-only

test-copilot-lockdown: build
	@echo "Running Copilot with MCP Gateway Guard (lockdown mode - yolo + --lockdown-mode)..."
	@./scripts/run_copilot_test.sh lockdown

# ---- Guard SDK -------------------------------------------------------------

# Build/check the guard SDK
build-sdk:
	@echo "Building guard-sdk..."
	@cd guard-sdk && cargo check

# Run guard SDK unit tests
test-sdk:
	@echo "Running guard-sdk unit tests..."
	@cd guard-sdk && cargo test --lib

# ---- No-op Guard -----------------------------------------------------------

# Build the no-op guard WASM module
build-noop: build-sdk
	@echo "Building noop guard..."
	@cd noop-guard && ./build.sh

# Run no-op guard Rust unit tests
test-noop-unit: build-noop
	@echo "Running noop guard unit tests..."
	@cd noop-guard && cargo test --lib

# Run no-op guard Go WASM integration tests
test-noop-wasm: build-noop
	@echo "Running noop guard WASM integration tests..."
	@go test ./src -run TestNoop -v

# Run the full noop test pipeline
test-noop: build-noop test-noop-unit test-noop-wasm
	@echo "✓ Noop guard test pipeline complete"

# ---- Aggregate targets -----------------------------------------------------

# Run all tests including integration, integrity, and copilot (requires Docker + .env + Copilot CLI)
test-all: test test-copilot

# Build AgentDojo WASM guards
build-agentdojo-guards: build-sdk
	@echo "Building AgentDojo guards..."
	@cd agentdojo-guards && ./build.sh

# Run AgentDojo guard unit tests
test-agentdojo-guards:
	@echo "Running AgentDojo guard unit tests..."
	@cd agentdojo-guards && cargo test --lib

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f github-guard-rust.wasm
	@rm -f noop-guard.wasm
	@rm -f banking-guard.wasm travel-guard.wasm workspace-guard.wasm
	@rm -rf rust-guard/target
	@rm -rf noop-guard/target
	@rm -rf guard-sdk/target
	@rm -rf agentdojo-guards/target
	@echo "✓ Clean complete"

# Show help
help:
	@echo "GitHub Guard Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all              - Build and run tests (default)"
	@echo "  build            - Build the WASM module"
	@echo "  test             - Run default pipeline (build, unit, wasm, integration, integrity)"
	@echo "  test-all         - Run all tests (unit, wasm, integration, copilot)"
	@echo "  test-unit        - Run unit tests only"
	@echo "  test-wasm        - Verify WASM build"
	@echo "  test-integration - Run integration tests (requires Docker + .env)"
	@echo "  test-integrity-harness - Run corpus-driven WASM integrity labeling tests"
	@echo "  capture-integrity-corpus - Refresh integrity corpus from live OSS repos"
	@echo "  test-copilot     - Run Copilot in yolo mode (default)"
	@echo "  test-copilot-yolo           - Copilot with no guard, no DIFC"
	@echo "  test-copilot-all            - Copilot with allow-only repos=all and min-integrity=approved"
	@echo "  test-copilot-public-only    - Copilot filtering private data (public repos only)"
	@echo "  test-copilot-owner-only     - Copilot filtering private data outside owner scope (lpcox)"
	@echo "  test-copilot-repo-only      - Copilot filtering data outside repo scope (lpcox/github-guard)"
	@echo "  test-copilot-prefix-only    - Copilot filtering data outside repo prefix scope (lpcox/git-*)"
	@echo "  test-copilot-multi-only     - Copilot filtering using multi-entry repo scope array"
	@echo "  build-sdk        - Build/check the guard SDK"
	@echo "  test-sdk         - Run guard SDK unit tests"
	@echo "  build-noop       - Build the no-op guard WASM module"
	@echo "  test-noop        - Run full noop guard test pipeline"
	@echo "  test-noop-unit   - Run noop guard Rust unit tests"
	@echo "  test-noop-wasm   - Run noop guard Go WASM integration tests"
	@echo "  clean            - Remove build artifacts"
	@echo "  release          - Create and push a release tag (requires bump type: patch|minor|major)"
	@echo "  help             - Show this help message"
	@echo ""
	@echo "Copilot Prerequisites:"
	@echo "  1. Install Copilot CLI: brew install --cask copilot-cli"
	@echo "  2. Authenticate GitHub CLI: gh auth login"
	@echo "  3. Create .env with GITHUB_TOKEN"

# Create and push a release tag
release:
	@echo "Creating release tag..."
	@# Check if first argument is provided
	@if [ -z "$(filter-out $@,$(MAKECMDGOALS))" ]; then \
		echo "Error: Bump type is required. Usage: make release patch|minor|major"; \
		exit 1; \
	fi
	@BUMP_TYPE="$(filter-out $@,$(MAKECMDGOALS))"; \
	if ! echo "$$BUMP_TYPE" | grep -qE '^(patch|minor|major)$$'; then \
		echo "Error: Bump type must be one of: patch, minor, major"; \
		exit 1; \
	fi; \
	echo "Bump type: $$BUMP_TYPE"; \
	echo ""; \
	echo "Fetching latest changes from remote..."; \
	git pull || { echo "Error: Failed to pull latest changes"; exit 1; }; \
	git fetch --tags --force || { echo "Error: Failed to fetch tags"; exit 1; }; \
	echo "✓ Latest changes and tags fetched"; \
	echo ""; \
	echo "Checking for uncommitted changes..."; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: You have uncommitted changes. Please commit or stash them before creating a release."; \
		git status --short; \
		exit 1; \
	fi; \
	echo "✓ Working directory is clean"; \
	echo ""; \
	LATEST_TAG=$$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -1); \
	if [ -z "$$LATEST_TAG" ]; then \
		echo "No existing tags found, starting from v0.0.0"; \
		LATEST_TAG="v0.0.0"; \
	else \
		echo "Latest tag: $$LATEST_TAG"; \
	fi; \
	VERSION_NUM=$$(echo $$LATEST_TAG | sed 's/^v//'); \
	MAJOR=$$(echo $$VERSION_NUM | cut -d. -f1); \
	MINOR=$$(echo $$VERSION_NUM | cut -d. -f2); \
	PATCH=$$(echo $$VERSION_NUM | cut -d. -f3); \
	if [ "$$BUMP_TYPE" = "major" ]; then \
		MAJOR=$$((MAJOR + 1)); \
		MINOR=0; \
		PATCH=0; \
	elif [ "$$BUMP_TYPE" = "minor" ]; then \
		MINOR=$$((MINOR + 1)); \
		PATCH=0; \
	elif [ "$$BUMP_TYPE" = "patch" ]; then \
		PATCH=$$((PATCH + 1)); \
	fi; \
	NEW_VERSION="v$$MAJOR.$$MINOR.$$PATCH"; \
	echo ""; \
	echo "New version will be: $$NEW_VERSION"; \
	echo ""; \
	printf "Do you want to create and push this tag? [Y/n] "; \
	read -r CONFIRM; \
	CONFIRM=$${CONFIRM:-Y}; \
	if [ "$$CONFIRM" != "Y" ] && [ "$$CONFIRM" != "y" ]; then \
		echo "Release cancelled."; \
		exit 1; \
	fi; \
	echo "Creating and pushing tag: $$NEW_VERSION"; \
	git tag -a "$$NEW_VERSION" -m "Release $$NEW_VERSION"; \
	git push origin "$$NEW_VERSION"; \
	echo "✓ Tag $$NEW_VERSION created and pushed"; 
