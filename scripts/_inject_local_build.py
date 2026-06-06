#!/usr/bin/env python3
"""Inject a local Docker build step into a smoke test lock file.

Called by scripts/patch-smoke-local-build.sh — not intended for direct use.
Exits 0 on success, 1 if no insertion point was found.
"""
import sys

BUILD_STEP = """\
      - name: Build MCP Gateway from source (local)
        env:
          BUILD_VERSION: ${{ github.sha }}
        run: |
          # Install Rust with WASM target for the guard
          curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable -t wasm32-wasip1
          source "$HOME/.cargo/env"
          # Build WASM guard
          make -C guards/github-guard build
          # Build gateway Docker image, overwriting the pulled :latest
          docker build -t ghcr.io/github/gh-aw-mcpg:latest \\
            --build-arg VERSION="$BUILD_VERSION" .
          echo "Built local gateway image from $(git rev-parse --short HEAD)"
"""

lockfile = sys.argv[1]

with open(lockfile, "r") as f:
    lines = f.readlines()

result = []
found_download = False
injected = False

for line in lines:
    if "name: Download container images" in line:
        found_download = True
    elif found_download and not injected and line.strip().startswith("- name:"):
        result.append(BUILD_STEP)
        injected = True
    result.append(line)

if injected:
    with open(lockfile, "w") as f:
        f.writelines(result)

sys.exit(0 if injected else 1)
