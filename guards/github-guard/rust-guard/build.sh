#!/bin/bash
# Build script for the Rust GitHub Guard WASM module
# 
# Prerequisites:
#   rustup target add wasm32-wasip1
#
# Usage:
#   ./build.sh           # Build in release mode
#   ./build.sh debug     # Build in debug mode

set -e

cd "$(dirname "$0")"

MODE="${1:-release}"

# Use rustup's toolchain if available (homebrew rust doesn't have wasm targets)
# We must set RUSTC explicitly because cargo may still find the wrong rustc in PATH
if [ -d "$HOME/.rustup/toolchains/stable-aarch64-apple-darwin/bin" ]; then
    TOOLCHAIN="$HOME/.rustup/toolchains/stable-aarch64-apple-darwin"
    export PATH="$TOOLCHAIN/bin:$PATH"
    export RUSTC="$TOOLCHAIN/bin/rustc"
elif [ -d "$HOME/.rustup/toolchains/stable-x86_64-apple-darwin/bin" ]; then
    TOOLCHAIN="$HOME/.rustup/toolchains/stable-x86_64-apple-darwin"
    export PATH="$TOOLCHAIN/bin:$PATH"
    export RUSTC="$TOOLCHAIN/bin/rustc"
fi

echo "Building GitHub Guard (Rust) in $MODE mode..."
echo "Using: $(which cargo)"

if [ "$MODE" = "debug" ]; then
    cargo build --target wasm32-wasip1
    WASM_PATH="target/wasm32-wasip1/debug/github_guard.wasm"
else
    cargo build --target wasm32-wasip1 --release
    WASM_PATH="target/wasm32-wasip1/release/github_guard.wasm"
fi

# Copy to project root for easy access
cp "$WASM_PATH" ../github-guard-rust.wasm

# Show file size
SIZE=$(wc -c < ../github-guard-rust.wasm)
SIZE_KB=$((SIZE / 1024))

echo ""
echo "✅ Build successful!"
echo "   Output: github-guard-rust.wasm ($SIZE_KB KB)"
