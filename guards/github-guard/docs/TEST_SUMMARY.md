# Test Coverage Summary

This document summarizes the test coverage for the GitHub Guard project.

## Overview

- **Implementation**: Rust (WASM)
- **Test Framework**: Cargo test
- **WASM Target**: wasm32-wasip1

## Test Types

### 1. Unit Tests (rust-guard/src/)
Tests the Rust source code directly with the native target.

### 2. WASM Build Verification
Verifies the WASM module compiles correctly.

### 3. Integration Tests (Docker)
Tests the full stack with real MCP Gateway and GitHub MCP Server.

### 4. Copilot Tests
End-to-end tests with GitHub Copilot CLI.

## Unit Tests

### Permission Tests (permissions.rs)

**test_permission_level_parsing** (8 cases)
- Parses GitHub permission strings: admin, maintain, write, read
- Handles legacy names: push → write, pull → read
- Case-insensitive matching

**test_permission_level_checks** (6 cases)
- `is_maintainer()`: Admin, Maintain → true; Write, Read → false
- `is_contributor()`: Admin, Maintain, Write → true; Read → false

**test_bot_detection** (5 cases)
- Detects `[bot]` suffix
- Detects `-bot` suffix
- Recognizes known bots: dependabot, renovate, github-actions

## Labeling Coverage

The guard implements labeling for all GitHub MCP tools. Coverage by category:

### Repository Operations
- `search_repositories`: Per-item labeling (private repos get secrecy tags)
- `get_repository`: Tool-level labels

### Pull Request Operations
- `list_pull_requests`: Per-item labeling based on merged state
- `search_pull_requests`: Per-item labeling
- `get_pull_request`: Tool-level labels

### Issue Operations
- `list_issues`: Per-item labeling based on author contributor status
- `search_issues`: Per-item labeling
- `get_issue`: Per-item labeling

### Commit Operations
- `list_commits`: Per-item labeling based on branch
- `get_commit`: Tool-level labels

### Other Operations
- Releases, Tags, Gists, Notifications, etc.

## Running Tests

```bash
# Run all tests
make test

# Run with verbose output
cd rust-guard && cargo test -- --nocapture

# Run specific test
cd rust-guard && cargo test test_bot_detection
```

## Integration Tests

Integration tests run with Docker containers:

```bash
# Prerequisites: Docker running, .env with GITHUB_TOKEN
make test-integration
```

Tests cover:
- Gateway startup with guard loaded
- MCP session initialization
- Tool listing
- Read operations

## Copilot Tests

End-to-end tests with Copilot CLI:

```bash
# Prerequisites: Copilot CLI installed, authenticated
make test-copilot
```

## Build Requirements

- **Rust**: Stable toolchain
- **Target**: `rustup target add wasm32-wasip1`
- **Docker**: For integration tests

## Continuous Integration

Example GitHub Actions workflow:

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install Rust
        uses: actions-rs/toolchain@v1
        with:
          toolchain: stable
          target: wasm32-wasip1
      - run: make test
      - run: make build
```
