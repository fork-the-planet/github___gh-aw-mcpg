#!/bin/bash
# Integration test runner for GitHub Guard
# This script starts the gateway container with the guard and runs integration tests
#
# ============================================================================
# PREREQUISITES
# ============================================================================
#
# 1. GitHub Personal Access Token (PAT)
#    Create a PAT at: https://github.com/settings/tokens
#    Required scopes: repo, read:org, read:user
#
#    Save it in a .env file in the project root:
#      echo 'GITHUB_TOKEN=ghp_your_token_here' > .env
#
# 2. GitHub Container Registry (ghcr.io) Authentication
#    The gateway image is hosted on ghcr.io and may require authentication.
#
#    To authenticate with ghcr.io:
#      echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
#
#    Or use the GitHub CLI:
#      gh auth token | docker login ghcr.io -u $(gh api user -q .login) --password-stdin
#
#    Your PAT needs the 'read:packages' scope for pulling container images.
#
# ============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
GATEWAY_IMAGE="${GATEWAY_IMAGE:-ghcr.io/lpcox/github-guard:latest}"
GITHUB_MCP_IMAGE="${GITHUB_MCP_IMAGE:-ghcr.io/github/github-mcp-server:latest}"
GATEWAY_PORT="${GATEWAY_PORT:-18080}"
GATEWAY_API_KEY="${GATEWAY_API_KEY:-test-api-key-12345}"
CONTAINER_NAME="github-guard-integration-test"
ALLOW_ONLY_INTEGRATION_POLICY="${ALLOW_ONLY_INTEGRATION_POLICY:-{\"allow-only\":{\"repos\":\"all\",\"min-integrity\":\"none\"}}}"

echo "=========================================="
echo "GitHub Guard Integration Test Runner"
echo "=========================================="
echo ""

# Check for .env file
ENV_FILE="$PROJECT_ROOT/.env"
if [ ! -f "$ENV_FILE" ]; then
    echo -e "${RED}ERROR: .env file not found!${NC}"
    echo ""
    echo "Create a .env file in the project root with your GitHub token:"
    echo "  echo 'GITHUB_TOKEN=ghp_your_token_here' > $ENV_FILE"
    echo ""
    echo -e "${YELLOW}WARNING: Never commit .env to git!${NC}"
    exit 1
fi

# Load GitHub token from .env
GITHUB_TOKEN=""
while IFS='=' read -r key value; do
    # Skip comments and empty lines
    [[ "$key" =~ ^#.*$ ]] && continue
    [[ -z "$key" ]] && continue
    
    # Remove quotes from value
    value=$(echo "$value" | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")
    
    if [ "$key" = "GITHUB_TOKEN" ] || [ "$key" = "GITHUB_PERSONAL_ACCESS_TOKEN" ]; then
        GITHUB_TOKEN="$value"
    fi
done < "$ENV_FILE"

if [ -z "$GITHUB_TOKEN" ]; then
    echo -e "${RED}ERROR: GitHub token not found in .env file!${NC}"
    echo ""
    echo "Add one of these to your .env file:"
    echo "  GITHUB_TOKEN=ghp_your_token_here"
    echo "  or"
    echo "  GITHUB_PERSONAL_ACCESS_TOKEN=ghp_your_token_here"
    exit 1
fi

echo -e "${GREEN}✓${NC} GitHub token loaded from .env"
echo "  Token: ${GITHUB_TOKEN:0:4}...${GITHUB_TOKEN: -4}"
echo ""

# Check for WASM file
WASM_FILE="$PROJECT_ROOT/github-guard-rust.wasm"
if [ ! -f "$WASM_FILE" ]; then
    echo -e "${YELLOW}WASM file not found. Building...${NC}"
    cd "$PROJECT_ROOT"
    make build
    if [ ! -f "$WASM_FILE" ]; then
        echo -e "${RED}ERROR: Failed to build WASM file${NC}"
        exit 1
    fi
fi

echo -e "${GREEN}✓${NC} WASM file found: $WASM_FILE"
WASM_SIZE=$(ls -lh "$WASM_FILE" | awk '{print $5}')
echo "  Size: $WASM_SIZE"
echo ""

# Check Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}ERROR: Docker is not running${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Docker is running"
echo ""

# Ensure required images are available
echo "Checking required Docker images..."

# Function to ensure image is present locally, pulling only when missing
ensure_image() {
    local image="$1"
    local description="$2"

    if docker image inspect "$image" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} Using local $description: $image"
        return 0
    fi

    echo -e "${YELLOW}Local $description not found. Pulling: $image${NC}"
    
    if docker pull "$image" 2>&1; then
        echo -e "${GREEN}✓${NC} Pulled $description: $image"
        return 0
    else
        echo ""
        echo -e "${RED}ERROR: Failed to pull $description${NC}"
        echo -e "${YELLOW}Image: $image${NC}"
        echo ""
        echo "This may be due to missing ghcr.io authentication."
        echo ""
        echo "To authenticate with GitHub Container Registry:"
        echo "  1. Ensure your GitHub token has 'read:packages' scope"
        echo "  2. Run: echo \$GITHUB_TOKEN | docker login ghcr.io -u YOUR_USERNAME --password-stdin"
        echo ""
        echo "Or using GitHub CLI:"
        echo "  gh auth token | docker login ghcr.io -u \$(gh api user -q .login) --password-stdin"
        echo ""
        
        echo -e "${RED}No local image available. Cannot continue.${NC}"
        return 1
    fi
}

ensure_image "$GATEWAY_IMAGE" "gateway image" || exit 1
ensure_image "$GITHUB_MCP_IMAGE" "GitHub MCP server image" || exit 1
echo ""

# Stop any existing test container
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

echo "Starting gateway container..."
echo "  Image: $GATEWAY_IMAGE"
echo "  Port: $GATEWAY_PORT"
echo "  Guard: /guard/github-guard-rust.wasm"
echo ""

GATEWAY_TEST_MODE="integration"
GATEWAY_DIFC_MODE="gateway-default"
GATEWAY_CLI_ARGS=(
    --enable-config-extensions
    --enable-difc
)

echo "Gateway runtime settings:"
echo "  Test mode: $GATEWAY_TEST_MODE"
echo "  DIFC mode: $GATEWAY_DIFC_MODE"
echo "  Guard policy: $ALLOW_ONLY_INTEGRATION_POLICY"
echo "  Enabled gateway flags:"
for ((i=0; i<${#GATEWAY_CLI_ARGS[@]}; i++)); do
    arg="${GATEWAY_CLI_ARGS[$i]}"
    if [[ "$arg" == --* ]]; then
        next="${GATEWAY_CLI_ARGS[$((i + 1))]:-}"
        if [[ -n "$next" && "$next" != --* ]]; then
            echo "    $arg=$next"
        else
            echo "    $arg"
        fi
    fi
done
echo ""

# Start the gateway container in the background with stdin
# We use a here-doc to pass the config directly to docker
(docker run -i --rm --name "$CONTAINER_NAME" \
    -e MCP_GATEWAY_PORT="$GATEWAY_PORT" \
    -e MCP_GATEWAY_DOMAIN=localhost \
    -e MCP_GATEWAY_API_KEY="$GATEWAY_API_KEY" \
    -e DOCKER_API_VERSION=1.44 \
    -e MCP_GATEWAY_ENABLE_DIFC=1 \
    -e MCP_GATEWAY_CONFIG_EXTENSIONS=1 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$WASM_FILE:/guard/github-guard-rust.wasm:ro" \
    -v "/tmp:/tmp:rw" \
    -p "$GATEWAY_PORT:$GATEWAY_PORT" \
        "$GATEWAY_IMAGE" "${GATEWAY_CLI_ARGS[@]}" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "container": "$GITHUB_MCP_IMAGE",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_TOKEN"
      },
      "guard": "github-guard",
      "guard-policies": $ALLOW_ONLY_INTEGRATION_POLICY
    }
  },
  "guards": {
    "github-guard": {
      "type": "wasm",
      "path": "/guard/github-guard-rust.wasm"
    }
  },
  "gateway": {
    "port": $GATEWAY_PORT,
    "domain": "localhost",
    "apiKey": "$GATEWAY_API_KEY"
  }
}
EOF
) > /tmp/gateway.log 2>&1 &
GATEWAY_PID=$!

# Wait for gateway to be ready
echo "Waiting for gateway to start..."
MAX_WAIT=30
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    if curl -s "http://localhost:$GATEWAY_PORT/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} Gateway is ready"
        break
    fi
    # Check if the process died
    if ! kill -0 $GATEWAY_PID 2>/dev/null; then
        echo -e "${RED}ERROR: Gateway process died${NC}"
        echo "Container logs:"
        cat /tmp/gateway.log
        exit 1
    fi
    sleep 1
    WAITED=$((WAITED + 1))
    echo -n "."
done
echo ""

if [ $WAITED -ge $MAX_WAIT ]; then
    echo -e "${RED}ERROR: Gateway failed to start within ${MAX_WAIT}s${NC}"
    echo ""
    echo "Container logs:"
    cat /tmp/gateway.log
    # Cleanup
    kill $GATEWAY_PID 2>/dev/null || true
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    exit 1
fi

# Run integration tests
echo ""
echo "Running integration tests..."
echo "=========================================="
cd "$PROJECT_ROOT/src"
go test -v -tags=integration -run "TestIntegration|TestGateway" . || TEST_RESULT=$?

# Cleanup
echo ""
echo "=========================================="
echo "Cleaning up..."
kill $GATEWAY_PID 2>/dev/null || true
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Copy gateway log to project directory before cleanup
if [ -f /tmp/gateway.log ]; then
    cp /tmp/gateway.log "$PROJECT_ROOT/gateway.log"
    echo "Gateway logs saved to: $PROJECT_ROOT/gateway.log"
fi
rm -f /tmp/gateway.log

if [ "${TEST_RESULT:-0}" -ne 0 ]; then
    echo -e "${RED}Integration tests failed${NC}"
    exit 1
else
    echo -e "${GREEN}✓ All integration tests passed${NC}"
fi
