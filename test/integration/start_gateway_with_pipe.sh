#!/bin/bash
# start_gateway_with_pipe.sh - Launch MCP Gateway with configuration via pipes
#
# This script demonstrates launching the MCP Gateway using different pipe mechanisms:
# 1. Standard pipe (echo | command) - Simple pipe-based configuration
# 2. Named pipe/FIFO (mkfifo) - More robust asynchronous communication
#
# This script is similar in concept to start_mcp_gateway_server.sh from the gh-aw repository,
# providing a reusable way to launch the gateway with dynamic configuration in environments
# where file-based configuration is not suitable (e.g., containerized deployments).
#
# Usage:
#   BINARY=./awmg PORT=8000 MODE=--routed PIPE_TYPE=standard ./start_gateway_with_pipe.sh
#   BINARY=./awmg PORT=8001 MODE=--unified PIPE_TYPE=named ./start_gateway_with_pipe.sh
#
# Environment Variables:
#   BINARY       - Path to the awmg binary (default: ./awmg)
#   HOST         - Host to bind to (default: 127.0.0.1)
#   PORT         - Port to listen on (default: 13100)
#   MODE         - Gateway mode: --routed or --unified (default: --routed)
#   PIPE_TYPE    - Pipe mechanism: standard or named (default: standard)
#   TIMEOUT      - Startup timeout in seconds (default: 30)
#   NO_CLEANUP   - If set to 1, don't cleanup gateway process on exit (for tests)
#   KEEP_RUNNING - If set to 1, keep script running after gateway starts

set -e

# Default values
BINARY="${BINARY:-./awmg}"
HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-13100}"
MODE="${MODE:---routed}"
TIMEOUT="${TIMEOUT:-30}"
PIPE_TYPE="${PIPE_TYPE:-standard}"  # standard or named

# Color output for better visibility
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

# Cleanup function
cleanup() {
    local exit_code=$?
    
    # Only cleanup if NO_CLEANUP is not set (used by tests)
    if [ "${NO_CLEANUP:-0}" != "1" ]; then
        if [ -n "$GATEWAY_PID" ] && kill -0 "$GATEWAY_PID" 2>/dev/null; then
            log_info "Stopping gateway process (PID: $GATEWAY_PID)..."
            kill "$GATEWAY_PID" 2>/dev/null || true
            wait "$GATEWAY_PID" 2>/dev/null || true
        fi
    fi
    
    if [ -n "$NAMED_PIPE" ] && [ -p "$NAMED_PIPE" ]; then
        log_info "Cleaning up named pipe: $NAMED_PIPE"
        rm -f "$NAMED_PIPE"
    fi
    
    exit "$exit_code"
}

trap cleanup EXIT INT TERM

# Validate binary exists
if [ ! -x "$BINARY" ]; then
    log_error "Binary not found or not executable: $BINARY"
    log_error "Please build the binary first: make build"
    exit 1
fi

log_info "Using binary: $BINARY"

# Prepare configuration JSON
# If MOCK_BACKEND_URL is set, use an HTTP backend instead of a Docker stdio container.
# This avoids Docker pull delays in automated tests.
if [ -n "${MOCK_BACKEND_URL:-}" ]; then
    CONFIG_JSON=$(cat <<EOF
{
  "mcpServers": {
    "testserver": {
      "type": "http",
      "url": "${MOCK_BACKEND_URL}"
    }
  },
  "gateway": {
    "port": ${PORT},
    "domain": "localhost",
    "apiKey": "test-key"
  }
}
EOF
)
else
    CONFIG_JSON=$(cat <<EOF
{
  "mcpServers": {
    "testserver": {
      "type": "stdio",
      "container": "echo"
    }
  },
  "gateway": {
    "port": ${PORT},
    "domain": "localhost",
    "apiKey": "test-key"
  }
}
EOF
)
fi

# Function to start gateway with standard pipe
start_with_standard_pipe() {
    log_info "Starting gateway with standard pipe (echo | command)..."
    
    # Launch gateway with config piped via stdin
    echo "$CONFIG_JSON" | "$BINARY" \
        --config-stdin \
        --listen "${HOST}:${PORT}" \
        "$MODE" \
        > /tmp/gateway-stdout.log 2> /tmp/gateway-stderr.log &
    
    GATEWAY_PID=$!
    log_info "Gateway started with PID: $GATEWAY_PID"
}

# Function to start gateway with named pipe (FIFO)
start_with_named_pipe() {
    log_info "Starting gateway with named pipe (FIFO)..."
    
    # Create a named pipe
    NAMED_PIPE="/tmp/mcp-gateway-config-$$.fifo"
    mkfifo "$NAMED_PIPE"
    log_info "Created named pipe: $NAMED_PIPE"
    
    # Start the gateway in background, reading from the named pipe
    "$BINARY" \
        --config-stdin \
        --listen "${HOST}:${PORT}" \
        "$MODE" \
        < "$NAMED_PIPE" \
        > /tmp/gateway-stdout.log 2> /tmp/gateway-stderr.log &
    
    GATEWAY_PID=$!
    log_info "Gateway started with PID: $GATEWAY_PID"
    
    # Write configuration to the named pipe
    # This will block until the gateway opens the pipe for reading
    log_info "Writing configuration to named pipe..."
    echo "$CONFIG_JSON" > "$NAMED_PIPE"
    log_info "Configuration written to named pipe"
}

# Function to wait for gateway to be ready
wait_for_gateway() {
    local max_wait="$TIMEOUT"
    local url="http://${HOST}:${PORT}/health"
    local start_time
    start_time=$(date +%s)
    
    log_info "Waiting for gateway to be ready at $url (timeout: ${max_wait}s)..."
    
    while true; do
        if curl -s -f --max-time 2 "$url" > /dev/null 2>&1; then
            log_info "Gateway is ready!"
            return 0
        fi
        
        # Check elapsed time after the curl attempt
        local elapsed=$(( $(date +%s) - start_time ))
        if [ "$elapsed" -ge "$max_wait" ]; then
            break
        fi
        
        # Check if process is still running
        if ! kill -0 "$GATEWAY_PID" 2>/dev/null; then
            log_error "Gateway process died unexpectedly"
            if [ -f /tmp/gateway-stderr.log ]; then
                log_error "Stderr output:"
                cat /tmp/gateway-stderr.log >&2
            fi
            return 1
        fi
        
        sleep 0.5
    done
    
    log_error "Gateway did not become ready within ${max_wait}s"
    return 1
}

# Main execution
main() {
    log_info "Pipe type: $PIPE_TYPE"
    log_info "Listen address: ${HOST}:${PORT}"
    log_info "Mode: $MODE"
    
    # Start gateway based on pipe type
    case "$PIPE_TYPE" in
        standard)
            start_with_standard_pipe
            ;;
        named)
            start_with_named_pipe
            ;;
        *)
            log_error "Unknown pipe type: $PIPE_TYPE (must be 'standard' or 'named')"
            exit 1
            ;;
    esac
    
    # Wait for gateway to be ready
    if wait_for_gateway; then
        log_info "Gateway successfully started and is ready to accept requests"
        
        # Output the PID so tests can interact with it
        echo "$GATEWAY_PID"
        
        # Keep the script running if requested (for manual testing)
        if [ "${KEEP_RUNNING:-0}" = "1" ]; then
            log_info "Keeping gateway running (press Ctrl+C to stop)..."
            wait "$GATEWAY_PID"
        fi
        
        exit 0
    else
        log_error "Failed to start gateway"
        exit 1
    fi
}

main "$@"
