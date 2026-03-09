#!/bin/bash
# Copilot test runner for GitHub Guard
# This script starts the gateway container with the guard and runs Copilot CLI
#
# ============================================================================
# USAGE
# ============================================================================
#
# ./run_copilot_test.sh [MODE]
#
# Modes:
#   yolo           - No guard, no DIFC (plain gateway passthrough)
#   all            - Allow all repos with approved integrity floor
#   public-only    - Filtering private data (public repos only)
#   owner-only     - Filtering private data outside owner scope
#   repo-only      - Filtering data outside repo scope (lpcox/github-guard)
#   prefix-only    - Filtering data outside repo prefix scope (lpcox/git-*)
#   multi-only     - Filtering using multiple repo scopes (lpcox/git-* + lpcox/github-guard)
#
# Default mode is 'yolo' if not specified.
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
#    echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
#
# 3. Copilot CLI Installation
#    Install: brew install --cask copilot-cli
#
#    Copilot CLI uses GitHub CLI authentication.
#    Make sure you're logged in: gh auth login
#
# ============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse mode argument (default: yolo)
MODE="${1:-yolo}"

# Validate mode
case "$MODE" in
  yolo|all|public-only|owner-only|repo-only|prefix-only|multi-only|lockdown)
        ;;
    *)
        echo -e "${RED}ERROR: Invalid mode '$MODE'${NC}"
        echo ""
        echo "Valid modes:"
        echo "  yolo           - No guard, no DIFC extensions"
        echo "  all            - Allow all repos with approved integrity floor"
        echo "  public-only    - Filtering private data (public repos only)"
        echo "  owner-only     - Filtering private data outside owner scope"
        echo "  repo-only      - Filtering data outside repo scope (lpcox/github-guard)"
        echo "  prefix-only    - Filtering data outside repo prefix scope (lpcox/git-*)"
        echo "  multi-only     - Filtering using multiple repo scopes (lpcox/git-* + lpcox/github-guard)"
        exit 1
        ;;
esac

SERVER_GUARD_POLICIES_JSON="{}"

# Configuration
GATEWAY_IMAGE="${GATEWAY_IMAGE:-ghcr.io/lpcox/github-guard:latest}"
GITHUB_MCP_IMAGE="${GITHUB_MCP_IMAGE:-ghcr.io/github/github-mcp-server:latest}"
GATEWAY_PORT="${GATEWAY_PORT:-18080}"
GATEWAY_API_KEY="${GATEWAY_API_KEY:-test-api-key-12345}"
CONTAINER_NAME="github-guard-copilot-test"
COPILOT_WORK_DIR="/tmp/copilot"

# DIFC scope - the repository to protect
DIFC_SCOPE="${DIFC_SCOPE:-lpcox/github-guard}"
ALLOW_OWNER="${ALLOW_OWNER:-lpcox}"
DIFC_OWNER="${DIFC_SCOPE%%/*}"
DIFC_REPO="${DIFC_SCOPE##*/}"
ALLOW_OWNER_POLICY="$(printf '%s' "$ALLOW_OWNER" | tr '[:upper:]' '[:lower:]')"
DIFC_OWNER_POLICY="$(printf '%s' "$DIFC_OWNER" | tr '[:upper:]' '[:lower:]')"
DIFC_REPO_POLICY="$(printf '%s' "$DIFC_REPO" | tr '[:upper:]' '[:lower:]')"

# AllowOnly policy JSON (override via env if policy shape/values change)
if [ -z "${ALLOW_ONLY_PUBLIC_POLICY:-}" ]; then
  ALLOW_ONLY_PUBLIC_POLICY='{"allow-only":{"repos":"public","min-integrity": "none"}}'
fi
if [ -z "${ALLOW_ONLY_ALL_POLICY:-}" ]; then
  ALLOW_ONLY_ALL_POLICY='{"allow-only":{"repos":"all","min-integrity": "none"}}'
fi
if [ -z "${ALLOW_ONLY_OWNER_POLICY:-}" ]; then
  ALLOW_ONLY_OWNER_POLICY="{\"allow-only\":{\"repos\":[\"${ALLOW_OWNER_POLICY}/*\"],\"min-integrity\":\"none\"}}"
fi
if [ -z "${ALLOW_ONLY_REPO_POLICY:-}" ]; then
  ALLOW_ONLY_REPO_POLICY="{\"allow-only\":{\"repos\":[\"${DIFC_OWNER_POLICY}/${DIFC_REPO_POLICY}\"],\"min-integrity\":\"none\"}}"
fi
if [ -z "${ALLOW_ONLY_PREFIX_POLICY:-}" ]; then
  ALLOW_ONLY_PREFIX_POLICY='{"allow-only":{"repos":["lpcox/git-*"],"min-integrity": "none"}}'
fi
if [ -z "${ALLOW_ONLY_MULTI_POLICY:-}" ]; then
  ALLOW_ONLY_MULTI_POLICY='{"allow-only":{"repos":["lpcox/git-*","lpcox/github-guard"],"min-integrity": "merged"}}'
fi

validate_json_policy() {
    local policy_name="$1"
    local policy_value="$2"

    if ! python -c "import json,sys; json.loads(sys.argv[1])" "$policy_value" >/dev/null 2>&1; then
        echo -e "${RED}ERROR: Invalid JSON in ${policy_name}${NC}"
        echo ""
        echo "Value: $policy_value"
        echo ""
        echo "Provide valid JSON for ${policy_name}."
        exit 1
    fi
}

validate_owner_scope_policy() {
  local policy_name="$1"
  local policy_value="$2"

  if ! python -c 'import json,re,sys
policy=json.loads(sys.argv[1])
repos=((policy.get("allow-only") or {}).get("repos"))
ok=isinstance(repos,list) and len(repos)==1 and isinstance(repos[0],str) and re.fullmatch(r"[a-z0-9_-]{1,39}/\*", repos[0])
sys.exit(0 if ok else 1)
' "$policy_value" >/dev/null 2>&1; then
    echo -e "${RED}ERROR: ${policy_name} must use allow-only.repos as [\"<owner>/*\"] in lowercase${NC}"
    echo ""
    echo "Value: $policy_value"
    echo ""
    echo "Example: {\"allow-only\":{\"repos\":[\"octocat/*\"],\"min-integrity\":\"approved\"}}"
    exit 1
  fi
}

validate_json_policy "ALLOW_ONLY_PUBLIC_POLICY" "$ALLOW_ONLY_PUBLIC_POLICY"
validate_json_policy "ALLOW_ONLY_ALL_POLICY" "$ALLOW_ONLY_ALL_POLICY"
validate_json_policy "ALLOW_ONLY_OWNER_POLICY" "$ALLOW_ONLY_OWNER_POLICY"
validate_json_policy "ALLOW_ONLY_REPO_POLICY" "$ALLOW_ONLY_REPO_POLICY"
validate_json_policy "ALLOW_ONLY_PREFIX_POLICY" "$ALLOW_ONLY_PREFIX_POLICY"
validate_json_policy "ALLOW_ONLY_MULTI_POLICY" "$ALLOW_ONLY_MULTI_POLICY"
validate_owner_scope_policy "ALLOW_ONLY_OWNER_POLICY" "$ALLOW_ONLY_OWNER_POLICY"

echo "=========================================="
echo "GitHub Guard Copilot Test Runner"
echo "=========================================="
echo -e "Mode: ${BLUE}${MODE}${NC}"
echo ""

# Check for Copilot CLI
if ! command -v copilot &> /dev/null; then
    echo -e "${RED}ERROR: Copilot CLI not found!${NC}"
    echo ""
    echo "Install Copilot CLI:"
    echo "  brew install --cask copilot-cli"
    exit 1
fi

echo -e "${GREEN}✓${NC} Copilot CLI found"

# Check GitHub CLI authentication (Copilot uses gh auth)
if ! gh auth status &> /dev/null 2>&1; then
    echo -e "${RED}ERROR: GitHub CLI not authenticated!${NC}"
    echo ""
    echo "Copilot CLI uses GitHub CLI authentication."
    echo "Run the following to authenticate:"
    echo "  gh auth login"
    exit 1
fi

echo -e "${GREEN}✓${NC} GitHub CLI authenticated"
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
    exit 1
fi

echo -e "${GREEN}✓${NC} GitHub token loaded from .env"
echo ""

# Check for WASM file
GUARD_WASM="${GUARD_WASM:-github-guard-rust.wasm}"
WASM_FILE="$PROJECT_ROOT/$GUARD_WASM"
if [ ! -f "$WASM_FILE" ]; then
    echo -e "${YELLOW}WASM file not found: $GUARD_WASM. Building...${NC}"
    cd "$PROJECT_ROOT"
    make build
    if [ ! -f "$WASM_FILE" ]; then
        echo -e "${RED}ERROR: Failed to build WASM file${NC}"
        exit 1
    fi
fi

echo -e "${GREEN}✓${NC} WASM file found: $WASM_FILE"
echo ""

# Check Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}ERROR: Docker is not running${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Docker is running"
echo ""

# Pull required container images
PLAYWRIGHT_MCP_IMAGE="${PLAYWRIGHT_MCP_IMAGE:-mcp/playwright:latest}"

echo "Checking required container images..."

ensure_image() {
  local image="$1"
  local description="$2"

  if docker image inspect "$image" > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Using local $description: $image"
    return 0
  fi

  echo -e "${YELLOW}Local $description not found. Pulling: $image${NC}"
  if docker pull "$image" > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Pulled $description: $image"
  else
    echo -e "${YELLOW}⚠${NC} Failed to pull $description, continuing if runtime can resolve it: $image"
  fi
}

ensure_image "$GATEWAY_IMAGE" "gateway image"
ensure_image "$GITHUB_MCP_IMAGE" "GitHub MCP image"
ensure_image "$PLAYWRIGHT_MCP_IMAGE" "Playwright MCP image"
echo ""

# Create Copilot working directory
mkdir -p "$COPILOT_WORK_DIR/logs"
echo -e "${GREEN}✓${NC} Created Copilot work directory: $COPILOT_WORK_DIR"
echo ""

# Stop any existing test container
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

echo "Starting gateway container..."
echo "  Image: $GATEWAY_IMAGE"
echo "  Port: $GATEWAY_PORT"
if [ "$MODE" != "yolo" ] && [ "$MODE" != "lockdown" ]; then
  echo "  Guard discovery dir: /guards (server: github, file: 00-github-guard.wasm)"
else
  echo "  Guard: disabled"
fi
echo "  Mode: $MODE"
echo ""

# Build environment variables based on mode
DOCKER_ENV_ARGS=(
    -e MCP_GATEWAY_PORT="$GATEWAY_PORT"
    -e MCP_GATEWAY_DOMAIN=localhost
    -e MCP_GATEWAY_API_KEY="$GATEWAY_API_KEY"
    -e DOCKER_API_VERSION=1.44
    -e DEBUG="*"
)

  DOCKER_VOLUME_ARGS=(
    -v /var/run/docker.sock:/var/run/docker.sock
    -v "/tmp:/tmp:rw"
  )

  WASM_GUARDS_ROOT_HOST="$COPILOT_WORK_DIR/guards"
  WASM_GUARDS_ROOT_CONTAINER="/guards"
  if [ "$MODE" != "yolo" ] && [ "$MODE" != "lockdown" ]; then
    WASM_GUARD_SERVER_DIR_HOST="$WASM_GUARDS_ROOT_HOST/github"
    WASM_GUARD_DISCOVERED_FILE_HOST="$WASM_GUARD_SERVER_DIR_HOST/00-github-guard.wasm"
    mkdir -p "$WASM_GUARD_SERVER_DIR_HOST"
    cp "$WASM_FILE" "$WASM_GUARD_DISCOVERED_FILE_HOST"

    DOCKER_ENV_ARGS+=(
      -e MCP_GATEWAY_WASM_GUARDS_DIR="$WASM_GUARDS_ROOT_CONTAINER"
    )
    DOCKER_VOLUME_ARGS+=(
      -v "$WASM_GUARDS_ROOT_HOST:$WASM_GUARDS_ROOT_CONTAINER:ro"
    )
  fi

# Forward optional DIFC sink-server-IDs when the caller sets it
if [[ -n "${MCP_GATEWAY_DIFC_SINK_SERVER_IDS:-}" ]]; then
    DOCKER_ENV_ARGS+=(-e MCP_GATEWAY_DIFC_SINK_SERVER_IDS="$MCP_GATEWAY_DIFC_SINK_SERVER_IDS")
    echo -e "${BLUE}Forwarding MCP_GATEWAY_DIFC_SINK_SERVER_IDS=${MCP_GATEWAY_DIFC_SINK_SERVER_IDS}${NC}"
fi

case "$MODE" in
    yolo)
        # No guard, no DIFC - plain gateway mode
        echo -e "${YELLOW}Mode: yolo - No guard, no DIFC (development)${NC}"
        ;;
  all)
    # DIFC with global scope and approved integrity floor
    echo -e "${BLUE}Mode: all - Allow all repos with approved integrity floor${NC}"
    SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_ALL_POLICY"
    DOCKER_ENV_ARGS+=(
      -e MCP_GATEWAY_ENABLE_DIFC=1
      -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
      -e DEBUG='server:unified,guard:wasm'
    )
    ;;
    public-only)
        # DIFC with integrity filtering only (public repos only)
        echo -e "${BLUE}Mode: public-only - Filtering private data (public repos only)${NC}"
      SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_PUBLIC_POLICY"
        DOCKER_ENV_ARGS+=(
            -e MCP_GATEWAY_ENABLE_DIFC=1
            -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
            -e DEBUG='server:unified,guard:wasm'
        )
        ;;
    owner-only)
      # DIFC with integrity filtering scoped to a single owner
      echo -e "${BLUE}Mode: owner-only - Filtering data outside owner scope (${ALLOW_OWNER})${NC}"
      SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_OWNER_POLICY"
      DOCKER_ENV_ARGS+=(
        -e MCP_GATEWAY_ENABLE_DIFC=1
        -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
        -e DEBUG='server:unified,guard:wasm'
      )
      ;;
    repo-only)
        # DIFC with integrity filtering scoped to a single repository
        echo -e "${BLUE}Mode: repo-only - Filtering data outside repo scope (${DIFC_SCOPE})${NC}"
      SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_REPO_POLICY"
        DOCKER_ENV_ARGS+=(
        -e MCP_GATEWAY_ENABLE_DIFC=1
        -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
        -e DEBUG='server:unified,guard:wasm'
        )
        ;;
      prefix-only)
        # DIFC with integrity filtering scoped to owner/repo prefix
        echo -e "${BLUE}Mode: prefix-only - Filtering data outside repo prefix scope (lpcox/git-*)${NC}"
        SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_PREFIX_POLICY"
        DOCKER_ENV_ARGS+=(
        -e MCP_GATEWAY_ENABLE_DIFC=1
        -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
        -e DEBUG='server:unified,guard:wasm'
        )
        ;;
      multi-only)
        # DIFC with integrity filtering scoped to multiple repo entries
        echo -e "${BLUE}Mode: multi-only - Filtering using multiple repo scopes (lpcox/git-* + lpcox/github-guard)${NC}"
        SERVER_GUARD_POLICIES_JSON="$ALLOW_ONLY_MULTI_POLICY"
        DOCKER_ENV_ARGS+=(
        -e MCP_GATEWAY_ENABLE_DIFC=1
        -e MCP_GATEWAY_CONFIG_EXTENSIONS=1
        -e DEBUG='server:unified,guard:wasm'
        )
        ;;
    lockdown)
        # Yolo mode + GitHub MCP lockdown flag
        echo -e "${GREEN}Mode: lockdown - Yolo mode with GitHub MCP --lockdown-mode${NC}"
        ;;
esac
echo ""

# Build JSON config based on mode
if [ "$MODE" = "yolo" ] || [ "$MODE" = "lockdown" ]; then
    # Yolo mode: no guard, no extensions - includes Playwright for unrestricted web access
        GITHUB_MCP_ENTRYPOINT_ARGS_JSON=""
        if [ "$MODE" = "lockdown" ]; then
            GITHUB_MCP_ENTRYPOINT_ARGS_JSON=",
          \"entrypointArgs\": [\"stdio\", \"--lockdown-mode\"]"
        fi

    GATEWAY_CONFIG=$(cat <<EOF
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "container": "$GITHUB_MCP_IMAGE",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_TOKEN"
            }${GITHUB_MCP_ENTRYPOINT_ARGS_JSON}
    },
    "playwright": {
      "type": "stdio",
      "container": "$PLAYWRIGHT_MCP_IMAGE",
      "env": {
        "PLAYWRIGHT_MCP_HEADLESS": "true"
      }
    }
  },
  "gateway": {
    "port": $GATEWAY_PORT,
    "domain": "localhost",
    "apiKey": "$GATEWAY_API_KEY"
  }
}
EOF
)
else
    # All other modes: guard is discovered from MCP_GATEWAY_WASM_GUARDS_DIR
    GATEWAY_CONFIG=$(cat <<EOF
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "container": "$GITHUB_MCP_IMAGE",
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_TOKEN"
      },
      "guard-policies": $SERVER_GUARD_POLICIES_JSON
    },
    "playwright": {
      "type": "stdio",
      "container": "$PLAYWRIGHT_MCP_IMAGE",
      "env": {
        "PLAYWRIGHT_MCP_HEADLESS": "true"
      }
    }
  },
  "gateway": {
    "port": $GATEWAY_PORT,
    "domain": "localhost",
    "apiKey": "$GATEWAY_API_KEY"
  }
}
EOF
)
fi

# Build CLI args for the gateway.
# The entrypoint script doesn't translate env vars to CLI flags, so we override it.
USE_ROUTED_MODE="${USE_ROUTED_MODE:-auto}"
if [ "$USE_ROUTED_MODE" = "auto" ]; then
  if [[ "$GATEWAY_IMAGE" == local/gh-aw-mcpg* ]]; then
    USE_ROUTED_MODE="0"
  else
    USE_ROUTED_MODE="1"
  fi
fi

GATEWAY_CLI_ARGS=(
    --listen "0.0.0.0:$GATEWAY_PORT"
    --config-stdin
    --log-dir /tmp/gh-aw/mcp-logs
)

if [ "$USE_ROUTED_MODE" = "1" ]; then
  GATEWAY_CLI_ARGS=(--routed "${GATEWAY_CLI_ARGS[@]}")
fi

GATEWAY_DIFC_MODE="disabled"
ACTIVE_ALLOW_ONLY_POLICY=""

# Add DIFC flags for non-yolo modes
if [ "$MODE" != "yolo" ] && [ "$MODE" != "lockdown" ]; then
    GATEWAY_CLI_ARGS+=(
        --enable-config-extensions
        --enable-difc
    )
    
    # DIFC mode is guard-managed at runtime (do not set via CLI)
    case "$MODE" in
      all)
        GATEWAY_DIFC_MODE="guard-managed"
      ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_ALL_POLICY"
        ;;
        public-only)
            GATEWAY_DIFC_MODE="guard-managed"
        ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_PUBLIC_POLICY"
            ;;
        owner-only)
            GATEWAY_DIFC_MODE="guard-managed"
        ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_OWNER_POLICY"
            ;;
        repo-only)
            GATEWAY_DIFC_MODE="guard-managed"
        ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_REPO_POLICY"
            ;;
        prefix-only)
          GATEWAY_DIFC_MODE="guard-managed"
        ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_PREFIX_POLICY"
          ;;
        multi-only)
          GATEWAY_DIFC_MODE="guard-managed"
        ACTIVE_ALLOW_ONLY_POLICY="$ALLOW_ONLY_MULTI_POLICY"
          ;;
    esac
    
    # Add session labels based on mode
    # public-only: no session labels (empty)
fi

echo "Gateway runtime settings:"
echo "  Test mode: $MODE"
echo "  DIFC mode: $GATEWAY_DIFC_MODE"
if [ -n "$ACTIVE_ALLOW_ONLY_POLICY" ]; then
  echo "  AllowOnly policy: $ACTIVE_ALLOW_ONLY_POLICY"
fi
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
(docker run -i --rm --name "$CONTAINER_NAME" \
    "${DOCKER_ENV_ARGS[@]}" \
  "${DOCKER_VOLUME_ARGS[@]}" \
  -p "$GATEWAY_PORT:$GATEWAY_PORT" \
    "$GATEWAY_IMAGE" <<< "$GATEWAY_CONFIG"
) > "$COPILOT_WORK_DIR/gateway.log" 2>&1 &
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
        cat "$COPILOT_WORK_DIR/gateway.log"
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
    cat "$COPILOT_WORK_DIR/gateway.log"
    kill $GATEWAY_PID 2>/dev/null || true
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    exit 1
fi

# Create MCP config for Copilot
# Copilot CLI supports HTTP MCP servers with type/url/headers format
MCP_CONFIG_FILE="$COPILOT_WORK_DIR/mcp-config.json"

# In restricted DIFC modes, hard-restrict GitHub tools to read-only calls.
GITHUB_MCP_TOOLS_JSON='["*"]'
if [ "$MODE" = "public-only" ] || [ "$MODE" = "owner-only" ] || [ "$MODE" = "repo-only" ] || [ "$MODE" = "prefix-only" ] || [ "$MODE" = "multi-only" ]; then
    GITHUB_MCP_TOOLS_JSON='[
        "get_commit",
        "get_file_contents",
        "get_label",
        "get_latest_release",
        "get_me",
        "get_release_by_tag",
        "get_tag",
        "issue_read",
        "list_branches",
        "list_commits",
        "list_issues",
        "list_pull_requests",
        "list_releases",
        "list_tags",
        "pull_request_read",
        "search_code",
        "search_issues",
        "search_pull_requests",
        "search_repositories",
        "search_users"
      ]'
fi

cat > "$MCP_CONFIG_FILE" <<EOF
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "http://localhost:$GATEWAY_PORT/mcp/github",
      "headers": {
        "Authorization": "$GATEWAY_API_KEY"
      },
      "tools": $GITHUB_MCP_TOOLS_JSON
    },
    "playwright": {
      "type": "http",
      "url": "http://localhost:$GATEWAY_PORT/mcp/playwright",
      "headers": {
        "Authorization": "$GATEWAY_API_KEY"
      },
      "tools": ["*"]
    }
  }
}
EOF
echo -e "${GREEN}✓${NC} Created MCP config: $MCP_CONFIG_FILE"
echo ""

# Build mode-aware default prompt (can be overridden with COPILOT_PROMPT_FILE)
GENERATED_PROMPT_FILE="$COPILOT_WORK_DIR/copilot-prompt-${MODE}.txt"
case "$MODE" in
  yolo)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub MCP behavior through the MCP Gateway. Do not use the 'gh' cli tool. Only use the tools provided by the MCP servers (github and playwright).

## Test Configuration
- **Mode**: yolo
- **Guard**: disabled
- **DIFC**: disabled

## Test Plan

Execute EVERY read-only GitHub MCP call currently available in this environment.

### Part 1: Global/User Read-Only Calls (run once)
Run each of these once:
1. get_me
2. search_users (query: "octocat", perPage: 5)
3. search_repositories (query: "github-guard", perPage: 10)
4. search_issues (query: "bug", perPage: 5)
5. search_pull_requests (query: "is:open", perPage: 5)

### Part 2: Repo-Scoped Read-Only Calls (run for BOTH repos)
For each tool below, execute it twice:
- private target: owner=lpcox repo=github-guard
- public target: owner=octocat repo=Hello-World

Repo-scoped read-only tools to test:
1. list_issues (perPage: 5)
2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
3. list_pull_requests (perPage: 5)
4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
5. list_commits (perPage: 5)
6. get_commit (discover sha from list_commits first; skip only if no commits)
7. get_file_contents (path: "README.md")
8. list_branches (perPage: 10)
9. list_tags (perPage: 10)
10. get_tag (use strategy below)
11. list_releases (perPage: 5)
12. get_latest_release (skip only if repo has no releases)
13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
14. get_label (name: "bug")
15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

### get_tag Practical Strategy
- Use list_tags to pick up to 3 candidate tags per repo.
- Call get_tag on each candidate until one succeeds.
- If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
- Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

### Part 3: Expected Behavior Checks
- All read-only calls should succeed when data exists.
- No DIFC denial/filtering behavior should occur in yolo mode.
- Private and public repositories should both be accessible.

### Part 4: Final Report (required)
Provide:
1. A checklist of every read-only tool above with status for private and public calls.
2. Any skips and exact reason (e.g., no releases/tags in that repo).
3. Any unexpected errors observed.
4. A final PASS/FAIL for "yolo mode allows unrestricted read-only GitHub MCP access".
EOF
    ;;
  all)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub Guard in all mode through the MCP Gateway.

In all mode, policy allows all repos and enforces a approved integrity floor.

## Test Configuration
- **Mode**: all
- **DIFC Mode**: strict/filter (guard-managed)
- **AllowOnly Policy**: ${ALLOW_ONLY_ALL_POLICY}

## Test Plan

Execute EVERY read-only GitHub MCP call currently available in this environment.

### Part 1: Global/User Read-Only Calls (run once)
Run each of these once:
1. get_me
2. search_users (query: "octocat", perPage: 5)
3. search_repositories (query: "github-guard", perPage: 10)
4. search_issues (query: "bug", perPage: 5)
5. search_pull_requests (query: "is:open", perPage: 5)

### Part 2: Repo-Scoped Read-Only Calls (run for BOTH repos)
For each tool below, execute it twice:
- private target: owner=lpcox repo=github-guard
- public target: owner=octocat repo=Hello-World

Repo-scoped read-only tools to test:
1. list_issues (perPage: 5)
2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
3. list_pull_requests (perPage: 5)
4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
5. list_commits (perPage: 5)
6. get_commit (discover sha from list_commits first; skip only if no commits)
7. get_file_contents (path: "README.md")
8. list_branches (perPage: 10)
9. list_tags (perPage: 10)
10. get_tag (use strategy below)
11. list_releases (perPage: 5)
12. get_latest_release (skip only if repo has no releases)
13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
14. get_label (name: "bug")
15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

### get_tag Practical Strategy
- Use list_tags to pick up to 3 candidate tags per repo.
- Call get_tag on each candidate until one succeeds.
- If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
- Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

### Part 3: Expected Behavior Checks
- Both private and public repo calls may succeed when data exists.
- Results below approved integrity may be filtered/denied by guard policy.
- Empty arrays / zero-count search results should be recorded as **NO_DATA** (not failure) when no matching data exists.
- get_latest_release returning 404 Not Found should be recorded as **NO_RELEASE** (expected for repos without releases), not failure.
- get_label returning empty/filtered output should be recorded as **NO_LABEL_OR_FILTERED** unless there is a transport/auth/tool error.
- Treat only transport/auth/tool errors as hard failures (for example non-404 API errors, gateway/tool invocation errors, or malformed responses).

### Part 4: Final Report (required)
Provide:
1. A checklist of every read-only tool above with status for private and public calls.
2. Any skips/NO_DATA outcomes with exact reason (e.g., no releases/tags in that repo).
3. A final PASS/FAIL for "all mode applies approved integrity while allowing all repo scope".
4. Do not mark overall FAIL solely because of empty results or expected 404 no-release outcomes.
EOF
    ;;
  lockdown)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub MCP behavior through the MCP Gateway. Do not use the 'gh' cli tool. Only use the tools provided by the MCP servers (github and playwright).

## Test Configuration
- **Mode**: lockdown
- **Guard**: disabled
- **DIFC**: disabled

## Test Plan
1. List issues and pull requests from ${DIFC_SCOPE}
2. List issues and pull requests from octocat/Hello-World
3. Run one search tool (e.g., search_issues)

Expected: All operations should succeed because security filtering is not active in this mode.
EOF
    ;;
  public-only)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub Guard in public-only mode through the MCP Gateway.

${DIFC_SCOPE} is a private repository and in public-only mode, the guard should block access to private data. You should still be able to access public repositories and perform operations that do not require private data.

## Test Configuration
- **Mode**: public-only
- **DIFC Mode**: filter
- **AllowOnly Policy**: ${ALLOW_ONLY_PUBLIC_POLICY}

## Test Plan

Execute EVERY read-only GitHub MCP call currently available in this environment.

### Part 1: Global/User Read-Only Calls (run once)
Run each of these once:
1. get_me
2. search_users (query: "octocat", perPage: 5)
3. search_repositories (query: "github-guard", perPage: 10)
4. search_issues (query: "bug", perPage: 5)
5. search_pull_requests (query: "is:open", perPage: 5)

### Part 2: Repo-Scoped Read-Only Calls (run for BOTH repos)
For each tool below, execute it twice:
- private target: owner=lpcox repo=github-guard
- public target: owner=octocat repo=Hello-World

Repo-scoped read-only tools to test:
1. list_issues (perPage: 5)
2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
3. list_pull_requests (perPage: 5)
4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
5. list_commits (perPage: 5)
6. get_commit (discover sha from list_commits first; skip only if no commits)
7. get_file_contents (path: "README.md")
8. list_branches (perPage: 10)
9. list_tags (perPage: 10)
10. get_tag (use strategy below)
11. list_releases (perPage: 5)
12. get_latest_release (skip only if repo has no releases)
13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
14. get_label (name: "bug")
15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

### get_tag Practical Strategy
- Use list_tags to pick up to 3 candidate tags per repo.
- Call get_tag on each candidate until one succeeds.
- If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
- Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

### Part 3: Expected Behavior Checks
For every repo-scoped call:
- private repo (${DIFC_SCOPE}): response should be filtered/empty/denied for private data
- public repo (octocat/Hello-World): response should be accessible when data exists

For global search calls:
- search_repositories results must not expose ${DIFC_SCOPE}
- public data should still be returned

### Part 4: Final Report (required)
Provide:
1. A checklist of every read-only tool above with status for private and public calls.
2. Any skips and exact reason (e.g., no releases/tags in that repo).
3. A final PASS/FAIL for "public-only blocks private repo data across all read-only calls".
EOF
    ;;
  owner-only)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub Guard in owner-only mode through the MCP Gateway.

Owner scope is set to ${ALLOW_OWNER}. Repositories under this owner should remain accessible when labels allow, while any data outside this owner scope must be blocked.

Owner-only also applies a higher integrity floor for access checks:
- Integrity = writer

## Test Configuration
- **Mode**: owner-only
- **DIFC Mode**: filter
- **AllowOnly Policy**: ${ALLOW_ONLY_OWNER_POLICY}

## Test Plan

Execute EVERY read-only GitHub MCP call currently available in this environment.

### Part 1: Global/User Read-Only Calls (run once)
Run each of these once:
1. get_me
2. search_users (query: "octocat", perPage: 5)
3. search_repositories (query: "github-guard", perPage: 10)
4. search_issues (query: "bug", perPage: 5)
5. search_pull_requests (query: "is:open", perPage: 5)

### Part 2: Repo-Scoped Read-Only Calls (run for BOTH repos)
For each tool below, execute it twice:
- owner-scoped private target: owner=lpcox repo=github-guard
- outside-owner public target: owner=octocat repo=Hello-World

Repo-scoped read-only tools to test:
1. list_issues (perPage: 5)
2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
3. list_pull_requests (perPage: 5)
4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
5. list_commits (perPage: 5)
6. get_commit (discover sha from list_commits first; skip only if no commits)
7. get_file_contents (path: "README.md")
8. list_branches (perPage: 10)
9. list_tags (perPage: 10)
10. get_tag (use strategy below)
11. list_releases (perPage: 5)
12. get_latest_release (skip only if repo has no releases)
13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
14. get_label (name: "bug")
15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

### get_tag Practical Strategy
- Use list_tags to pick up to 3 candidate tags per repo.
- Call get_tag on each candidate until one succeeds.
- If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
- Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

### Part 3: Expected Behavior Checks
For repo-scoped calls:
- owner-scoped private repo (${DIFC_SCOPE}): should be accessible when data exists
- outside-owner public repo (octocat/Hello-World): public data should remain accessible

For cross-repo/private boundaries:
- private data from owners other than ${ALLOW_OWNER} must not be exposed
- search_repositories/search_code/search_issues/search_pull_requests must not leak out-of-scope private content

For integrity behavior:
- results below approved integrity should be blocked/filtered in owner-only mode

### Part 4: Final Report (required)
Provide:
1. A checklist of every read-only tool above with status for owner-scoped and outside-owner calls.
2. Any skips and exact reason (e.g., no releases/tags in that repo).
3. A final PASS/FAIL for "owner-only enforces owner-scoped private access while blocking out-of-scope private data".
EOF
    ;;
  repo-only)
  cat > "$GENERATED_PROMPT_FILE" <<EOF
You are testing GitHub Guard in repo-only mode through the MCP Gateway.

In repo-only mode, policy scope is pinned to repo ${DIFC_SCOPE}.

## Test Configuration
- **Mode**: repo-only
- **DIFC Mode**: filter
- **AllowOnly Policy**: ${ALLOW_ONLY_REPO_POLICY}

## Test Plan

Execute EVERY read-only GitHub MCP call currently available in this environment.

### Part 1: Global/User Read-Only Calls (run once)
Run each of these once:
1. get_me
2. search_users (query: "octocat", perPage: 5)
3. search_repositories (query: "github-guard", perPage: 10)
4. search_issues (query: "bug", perPage: 5)
5. search_pull_requests (query: "is:open", perPage: 5)

### Part 2: Repo-Scoped Read-Only Calls (run for BOTH repos)
For each tool below, execute it twice:
- scoped private target: owner=lpcox repo=github-guard
- public target: owner=octocat repo=Hello-World

Repo-scoped read-only tools to test:
1. list_issues (perPage: 5)
2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
3. list_pull_requests (perPage: 5)
4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
5. list_commits (perPage: 5)
6. get_commit (discover sha from list_commits first; skip only if no commits)
7. get_file_contents (path: "README.md")
8. list_branches (perPage: 10)
9. list_tags (perPage: 10)
10. get_tag (use strategy below)
11. list_releases (perPage: 5)
12. get_latest_release (skip only if repo has no releases)
13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
14. get_label (name: "bug")
15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

### get_tag Practical Strategy
- Use list_tags to pick up to 3 candidate tags per repo.
- Call get_tag on each candidate until one succeeds.
- If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
- Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

### Part 3: Expected Behavior Checks
For repo-scoped calls:
- scoped repo (${DIFC_SCOPE}): should be accessible when data exists
- outside-scope public repo (octocat/Hello-World): public data should remain accessible

For search and cross-repo behavior:
- private data from repos other than ${DIFC_SCOPE} must not be exposed
- search_repositories/search_code/search_issues/search_pull_requests must not leak out-of-scope private content

### Part 4: Final Report (required)
Provide:
1. A checklist of every read-only tool above with status for scoped-private and public calls.
2. Any skips and exact reason (e.g., no releases/tags in that repo).
3. A final PASS/FAIL for "repo-only enforces repo-scoped private access while blocking out-of-scope private data".
EOF
  ;;
    prefix-only)
    cat > "$GENERATED_PROMPT_FILE" <<EOF
  You are testing GitHub Guard in prefix-only mode through the MCP Gateway.

  In prefix-only mode, policy scope is pinned to repositories under owner lpcox whose names start with "git-".

  ## Test Configuration
  - **Mode**: prefix-only
  - **DIFC Mode**: filter
  - **AllowOnly Policy**: ${ALLOW_ONLY_PREFIX_POLICY}

  ## Test Plan

  Execute EVERY read-only GitHub MCP call currently available in this environment.

  ### Part 1: Global/User Read-Only Calls (run once)
  Run each of these once:
  1. get_me
  2. search_users (query: "octocat", perPage: 5)
  3. search_repositories (query: "git", perPage: 10)
  4. search_issues (query: "bug", perPage: 5)
  5. search_pull_requests (query: "is:open", perPage: 5)

  ### Part 2: Repo-Scoped Read-Only Calls
  Run the repo-scoped tools for:
  - one lpcox repo where repo name starts with "git-" (if available)
  - out-of-prefix private target: owner=lpcox repo=github-guard
  - out-of-owner public target: owner=octocat repo=Hello-World

  Repo-scoped read-only tools to test:
  1. list_issues (perPage: 5)
  2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
  3. list_pull_requests (perPage: 5)
  4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
  5. list_commits (perPage: 5)
  6. get_commit (discover sha from list_commits first; skip only if no commits)
  7. get_file_contents (path: "README.md")
  8. list_branches (perPage: 10)
  9. list_tags (perPage: 10)
  10. get_tag (use strategy below)
  11. list_releases (perPage: 5)
  12. get_latest_release (skip only if repo has no releases)
  13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
  14. get_label (name: "bug")
  15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

  ### get_tag Practical Strategy
  - Use list_tags to pick up to 3 candidate tags per repo.
  - Call get_tag on each candidate until one succeeds.
  - If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
  - Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

  ### Part 3: Expected Behavior Checks
  For repo-scoped calls:
  - in-prefix repo ("lpcox/git-*"): should be accessible when data exists
  - out-of-prefix repos: must not expose private data

  For search and cross-repo behavior:
  - private data from repos outside "lpcox/git-*" must not be exposed
  - search_repositories/search_code/search_issues/search_pull_requests must not leak out-of-scope private content

  ### Part 4: Final Report (required)
  Provide:
  1. A checklist of every read-only tool above with status for in-prefix and out-of-prefix calls.
  2. Any skips and exact reason (e.g., no matching in-prefix repo exists).
  3. A final PASS/FAIL for "prefix-only enforces lpcox/git-* scoped private access while blocking out-of-scope private data".
EOF
    ;;
  multi-only)
  cat > "$GENERATED_PROMPT_FILE" <<EOF
  You are testing GitHub Guard in multi-only mode through the MCP Gateway.

  In multi-only mode, policy scope is an array with multiple entries.

  ## Test Configuration
  - **Mode**: multi-only
  - **DIFC Mode**: filter
  - **AllowOnly Policy**: ${ALLOW_ONLY_MULTI_POLICY}

  ## Test Plan

  Execute EVERY read-only GitHub MCP call currently available in this environment.

  ### Part 1: Global/User Read-Only Calls (run once)
  Run each of these once:
  1. get_me
  2. search_users (query: "octocat", perPage: 5)
  3. search_repositories (query: "git", perPage: 10)
  4. search_issues (query: "bug", perPage: 5)
  5. search_pull_requests (query: "is:open", perPage: 5)

  ### Part 2: Repo-Scoped Read-Only Calls
  Run repo-scoped tools against:
  - explicit in-scope target: owner=lpcox repo=github-guard
  - out-of-scope owner target: owner=octocat repo=Hello-World

  Repo-scoped read-only tools to test:
  1. list_issues (perPage: 5)
  2. issue_read (discover issue_number from list_issues first; skip only if repo has no issues)
  3. list_pull_requests (perPage: 5)
  4. pull_request_read (discover pullNumber from list_pull_requests first; skip only if repo has no PRs)
  5. list_commits (perPage: 5)
  6. get_commit (discover sha from list_commits first; skip only if no commits)
  7. get_file_contents (path: "README.md")
  8. list_branches (perPage: 10)
  9. list_tags (perPage: 10)
  10. get_tag (use strategy below)
  11. list_releases (perPage: 5)
  12. get_latest_release (skip only if repo has no releases)
  13. get_release_by_tag (discover tag from list_releases first; skip only if no releases)
  14. get_label (name: "bug")
  15. search_code (query: "repo:<owner>/<repo> README", perPage: 5)

  ### get_tag Practical Strategy
  - Use list_tags to pick up to 3 candidate tags per repo.
  - Call get_tag on each candidate until one succeeds.
  - If all attempts return 404 Not Found, mark get_tag as expected skip (lightweight tags), not failure.
  - Only mark get_tag failed for non-404 errors (auth, permission, transport, etc.).

  ### Part 3: Expected Behavior Checks
  - Private data matching either repos entry in the policy should be accessible when integrity allows.
  - Private data outside all policy entries must not be exposed.
  - Search and cross-repo tools must not leak out-of-scope private content.

  ### Part 4: Final Report (required)
  Provide:
  1. A checklist of every read-only tool above with status for in-scope and out-of-scope calls.
  2. Any skips and exact reason.
  3. A final PASS/FAIL for "multi-only enforces combined repos array scope semantics".
EOF
  ;;
esac

PROMPT_FILE="${COPILOT_PROMPT_FILE:-$GENERATED_PROMPT_FILE}"
echo -e "${GREEN}✓${NC} Using prompt file: $PROMPT_FILE"

# Create conversation file if not exists
CONVERSATION_FILE="$PROJECT_ROOT/conversation.md"
if [ ! -f "$CONVERSATION_FILE" ]; then
    cat > "$CONVERSATION_FILE" <<EOF
# GitHub Guard DIFC Test

Testing DIFC enforcement with strict mode.

## Agent Configuration
- **Managed Repo**: ${DIFC_SCOPE}
- **Integrity Tags**: approved:${DIFC_SCOPE}, unapproved:${DIFC_SCOPE}
- **Secrecy Tags**: private:${DIFC_SCOPE}

## Test Cases

### ✅ Expected to SUCCEED (managed repo)
- list_issues owner:${DIFC_SCOPE%%/*} repo:${DIFC_SCOPE##*/}
- list_pull_requests owner:${DIFC_SCOPE%%/*} repo:${DIFC_SCOPE##*/}
- get_issue_comments (if issues exist)
- list_discussions (if enabled)

### ❌ Expected to be BLOCKED (unmanaged repo)
- list_issues owner:octocat repo:Hello-World
- list_pull_requests owner:octocat repo:Hello-World
- search_repositories (no repo context = empty integrity)

## Run the Tests
Please execute the test plan from copilot-prompt.txt
EOF
    echo -e "${GREEN}✓${NC} Created conversation file: $CONVERSATION_FILE"
fi

echo ""
echo "=========================================="
echo "Starting Copilot with MCP Gateway"
echo "=========================================="
echo ""
echo "MCP Config: $MCP_CONFIG_FILE"
echo "Prompt: $PROMPT_FILE"
if [ -n "$ACTIVE_ALLOW_ONLY_POLICY" ]; then
  echo "AllowOnly policy: $ACTIVE_ALLOW_ONLY_POLICY"
fi
echo "Logs: $COPILOT_WORK_DIR/logs/"
echo ""
echo "Press Ctrl+C to exit when done."
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "=========================================="
    echo "Cleaning up..."
    kill $GATEWAY_PID 2>/dev/null || true
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    
    # Save gateway log to project directory
    if [ -f "$COPILOT_WORK_DIR/gateway.log" ]; then
        cp "$COPILOT_WORK_DIR/gateway.log" "$PROJECT_ROOT/gateway.log"
        echo "Gateway logs saved to: $PROJECT_ROOT/gateway.log"
    fi
    
    echo -e "${GREEN}✓${NC} Cleanup complete"
}
trap cleanup EXIT

# Run Copilot
cd "$PROJECT_ROOT"

# Default to Claude model if not specified
COPILOT_MODEL="${MODEL_AGENT_COPILOT:-claude-sonnet-4}"

copilot \
    --add-dir "$COPILOT_WORK_DIR" \
    --log-level all \
    --log-dir "$COPILOT_WORK_DIR/logs/" \
    --add-dir "${PWD}" \
    --disable-builtin-mcps \
    --additional-mcp-config "@$MCP_CONFIG_FILE" \
    --allow-all-tools \
    --allow-all-paths \
    --log-level debug \
    --share "$CONVERSATION_FILE" \
    --prompt "$(cat "$PROMPT_FILE")" \
    --model "$COPILOT_MODEL"
