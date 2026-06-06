#!/usr/bin/env bash
#
# patch-smoke-local-build.sh — Post-compilation script for local-build smoke testing
#
# The smoke test .md files use sandbox.mcp.version: "latest" so that
# gh aw compile produces valid lock files that pull from the registry.
# This script patches the compiled .lock.yml files to:
#
#   1. Build a Docker image from the current checkout (with WASM guard)
#   2. Tag it as ghcr.io/github/gh-aw-mcpg:latest (overwriting the pulled image)
#
# The build step is injected AFTER the "Download container images" step
# so our locally-built image overwrites whatever was pulled from the registry.
#
# Usage:
#   gh aw compile smoke-copilot && scripts/patch-smoke-local-build.sh
#   gh aw compile && scripts/patch-smoke-local-build.sh   # patches all smoke workflows
#
# To revert, simply re-run: gh aw compile
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PATCHED=0
SKIPPED=0

# The step YAML to inject after "Download container images"
read -r -d '' BUILD_STEP << 'STEP' || true
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
          docker build -t ghcr.io/github/gh-aw-mcpg:latest \
            --build-arg VERSION="$BUILD_VERSION" .
          echo "✓ Built local gateway image from $(git rev-parse --short HEAD)"
STEP

for lockfile in .github/workflows/smoke-*.lock.yml; do
    [ -f "$lockfile" ] || continue

    # Skip if already patched
    if grep -q "Build MCP Gateway from source (local)" "$lockfile" 2>/dev/null; then
        echo "SKIP $lockfile (already patched)"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # Skip if no mcpg image reference
    if ! grep -q "gh-aw-mcpg" "$lockfile" 2>/dev/null; then
        echo "SKIP $lockfile (no mcpg reference)"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    # Find the "Download container images" step and inject our build step after it.
    # The download step is a single long line starting with:
    #   - name: Download container images
    #     run: bash "${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh" ...
    #
    # We inject our build step immediately after the download run line.
    
    tmpfile=$(mktemp)
    awk -v build_step="$BUILD_STEP" '
    /name: Download container images/ { found=1 }
    found && /^      - name:/ && !/Download container images/ {
        # We have reached the NEXT step after download — inject before it
        print build_step
        found=0
    }
    { print }
    END { if (found) print build_step }
    ' "$lockfile" > "$tmpfile"

    if diff -q "$lockfile" "$tmpfile" > /dev/null 2>&1; then
        echo "SKIP $lockfile (no insertion point found)"
        rm "$tmpfile"
        SKIPPED=$((SKIPPED + 1))
    else
        mv "$tmpfile" "$lockfile"
        echo "PATCHED $lockfile"
        PATCHED=$((PATCHED + 1))
    fi
done

echo ""
echo "Done: $PATCHED patched, $SKIPPED skipped"
if [ "$PATCHED" -gt 0 ]; then
    echo ""
    echo "The patched lock files will now build the gateway from source."
    echo "To revert: gh aw compile"
fi
