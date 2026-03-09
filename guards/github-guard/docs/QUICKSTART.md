# GitHub Guard Quick Start Guide

This quick start has one goal: run
`make test-copilot-repo-only`
using a **locally built MCP Gateway image**.

## What You Need

- Docker running locally
- Git
- Make
- Rust + `wasm32-wasip1` target
- GitHub CLI (`gh`) authenticated (`gh auth login`)
- Copilot CLI installed (`copilot` command available)
- A GitHub PAT with at least: `repo`, `read:org`, `read:user`

## 1) Clone and Build the Guard

```bash
git clone https://github.com/lpcox/github-guard.git
cd github-guard

# Install WASI target once (safe to re-run)
rustup target add wasm32-wasip1

# Build guard WASM
make build
```

## 2) Add Your GitHub Token

Create a project-local `.env` file (never commit it):

```bash
echo 'GITHUB_TOKEN=ghp_your_token_here' > .env
```

## 3) Build the Local MCP Gateway Image

Build from `github/gh-aw-mcpg` branch `lpcox/github-difc`:

```bash
git clone https://github.com/github/gh-aw-mcpg.git
cd gh-aw-mcpg
git fetch origin lpcox/github-difc
git checkout lpcox/github-difc
docker build -t local/gh-aw-mcpg:latest .
cd ../github-guard
```

## 4) Run Repo-Only Copilot Test with Local Gateway

Set scope and image explicitly, then run:

```bash
DIFC_SCOPE=lpcox/github-guard \
GATEWAY_IMAGE=local/gh-aw-mcpg:latest \
make test-copilot-repo-only
```

## Expected Result

- Gateway starts with DIFC enabled in repo-only mode.
- Guard WASM loads from this repo.
- Copilot test runs through the local gateway container.

## Logs Created by This Quick Start

Running `make test-copilot-repo-only` creates logs in these locations:

- Copilot CLI logs: `/tmp/copilot/logs/` (files like `process-*.log`)
- Gateway container output (during run): `/tmp/copilot/gateway.log`
- Gateway log copied to repo root on exit: `./gateway.log`

Note: the runner copies `/tmp/copilot/gateway.log` to `./gateway.log` during cleanup (including Ctrl+C exit).

## If It Fails Quickly

- `copilot: command not found` → install Copilot CLI.
- `gh auth` errors → run `gh auth login`.
- token errors → verify `.env` contains `GITHUB_TOKEN=...`.
- image errors → confirm `local/gh-aw-mcpg:latest` exists:

```bash
docker image inspect local/gh-aw-mcpg:latest
```

## More Testing

For the full test matrix and additional workflows, see [TESTING.md](./TESTING.md).
For architecture and mode details, see [OVERVIEW.md](./OVERVIEW.md).
