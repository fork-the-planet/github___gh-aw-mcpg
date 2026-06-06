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

    # Use Python for reliable multi-line insertion with correct indentation.
    python3 "$REPO_ROOT/scripts/_inject_local_build.py" "$lockfile"

    if [ $? -eq 0 ]; then
        echo "PATCHED $lockfile"
        PATCHED=$((PATCHED + 1))
    else
        echo "SKIP $lockfile (no insertion point found)"
        SKIPPED=$((SKIPPED + 1))
    fi
done

echo ""
echo "Done: $PATCHED patched, $SKIPPED skipped"
if [ "$PATCHED" -gt 0 ]; then
    echo ""
    echo "The patched lock files will now build the gateway from source."
    echo "To revert: gh aw compile"
fi
