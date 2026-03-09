# GitHub Guard - Rust Implementation

A Rust-based WASM guard for the MCP Gateway that implements DIFC
(Decentralized Information Flow Control) for GitHub resources.

## Overview

This guard labels GitHub API responses with security tags:

- **Secrecy labels**: Based on repository visibility (public/private)
- **Integrity labels**: Based on content trust level (merged/approved/unapproved/none)

## Prerequisites

1. Install Rust: https://rustup.rs/
2. Add the WASI target:
   ```bash
   rustup target add wasm32-wasip1
   ```

## Building

```bash
# Build release version
./build.sh

# Build debug version  
./build.sh debug

# Or use cargo directly
cargo build --target wasm32-wasip1 --release
```

The output will be:
- `target/wasm32-wasip1/release/github_guard.wasm` - Full WASM file
- `../github-guard-rust.wasm` - Copied to project root (~170KB)

## Architecture

```
src/
├── lib.rs         # Main entry point, WASM exports, memory management
├── labels/        # DIFC label generation and response labeling
│   ├── mod.rs
│   ├── tool_rules.rs
│   ├── response_items.rs
│   └── backend.rs
├── tools.rs       # Tool classification (write/read operations)
└── permissions.rs # Permission level helpers
```

## Key Features

### label_resource

Labels a resource before access based on the tool name and arguments.
Returns secrecy and integrity labels following the DIFC spec.

### label_response

Fine-grained labeling of response items. Parses JSON responses and labels
individual items based on their properties.

For example, in `search_repositories`:
- Public repos get empty secrecy labels
- Private repos get `private:owner/repo` labels
- All repos get `approved + unapproved` integrity (endorsed metadata)

For `list_issues`:
- Bot authors get `approved` integrity (with unapproved floor)
- Authors with merged PRs get at least `unapproved` integrity
- Unknown authors get empty integrity (untrusted)

## DIFC Label Format

**Secrecy Tags** (scoped):
- `[]` - Public (no restrictions)
- `["private:owner/repo"]` - Private to repository
- `["private:user"]` - Private to user
- `["secret"]` - Contains secrets

**Integrity Tags** (hierarchical: merged > approved > unapproved > none):
- `[]` - Untrusted (user-submitted content)
- `["unapproved:owner/repo"]` - Reader-level contribution trust
- `["unapproved:X", "approved:X"]` - Writer-level endorsement
- `["unapproved:X", "approved:X", "merged:X"]` - Merged/default-branch endorsement

## Testing

```bash
# Run unit tests
cargo test

# Run with verbose output
cargo test -- --nocapture

# From project root
make test
```

## Backend Calls

The guard can call the GitHub MCP server to verify contributor status:

```rust
// Check if user has merged PRs in a repo
let count = count_merged_prs("username", "owner", "repo");
if count.unwrap_or(0) > 0 {
    // User is a verified contributor
}
```

This uses `search_pull_requests` with query `author:X repo:Y is:merged`.
