---
description: Smoke test validating that the MCP Gateway proxy applies DIFC integrity filtering to actions/github-script (octokit) API calls
on:
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "eyes"
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read

name: "Smoke: Proxy + github-script"
engine:
  id: copilot
strict: false
imports:
  - shared/reporting.md
network:
  allowed:
    - defaults
    - github
    - github.com
    - rust
tools:
  agentic-workflows:
  cache-memory: true
  github:
    toolsets: [repos, issues]
    allowed-repos: ["github/gh-aw-mcpg"]
    min-integrity: approved
  edit:
  bash:
    - "cat"
    - "echo"
    - "date"
    - "jq"
    - "wc"
    - "head"
    - "tail"
    - "grep"
sandbox:
  mcp:
    container: "ghcr.io/github/gh-aw-mcpg"
steps:
  # ── Build the gateway container image from source ──────────────────
  - name: Build MCP Gateway image
    run: |
      # Install Rust and WASM target if not present
      if ! command -v rustup &>/dev/null; then
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable
        source "$HOME/.cargo/env"
      fi
      rustup target add wasm32-wasip1

      # Build the Rust WASM guard (required by Dockerfile)
      cd guards/github-guard/rust-guard
      bash build.sh
      cd ../../..

      # Build Docker image
      docker build -t awmg-local:latest .

  # ── Start the DIFC proxy ──────────────────────────────────────────
  - name: Start DIFC proxy
    env:
      GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
    run: |
      PROXY_LOG_DIR=/tmp/gh-aw/proxy-logs
      MCP_LOG_DIR=/tmp/gh-aw/mcp-logs
      RESULTS_DIR=/tmp/gh-aw/github-script-results
      mkdir -p "$PROXY_LOG_DIR" "$MCP_LOG_DIR" "$RESULTS_DIR"

      POLICY='{"allow-only":{"repos":["github/gh-aw-mcpg"],"min-integrity":"approved"}}'

      # Plain HTTP — avoids TLS cert trust issues with undici/Node.js
      docker run -d --name awmg-proxy --network host \
        -e GH_TOKEN \
        -e DEBUG='*' \
        -v "$PROXY_LOG_DIR:$PROXY_LOG_DIR" \
        -v "$MCP_LOG_DIR:$MCP_LOG_DIR" \
        awmg-local:latest proxy \
          --policy "$POLICY" \
          --listen 0.0.0.0:18443 \
          --log-dir "$MCP_LOG_DIR" \
          --guards-mode filter \
          --trusted-bots github-actions[bot],dependabot[bot],copilot

      # Wait for proxy health check
      PROXY_READY=false
      for i in $(seq 1 30); do
        if curl -sf "http://localhost:18443/health" -o /dev/null 2>/dev/null; then
          echo "DIFC proxy ready on port 18443"
          PROXY_READY=true
          break
        fi
        sleep 1
      done

      if [ "$PROXY_READY" = "false" ]; then
        echo "::error::DIFC proxy failed to start"
        docker logs awmg-proxy 2>&1 | tail -30 || true
        exit 1
      fi

      echo "RESULTS_DIR=$RESULTS_DIR" >> "$GITHUB_ENV"

  # ── Test 1: In-scope repo — list issues (REST) ────────────────────
  - name: "Test 1: In-scope list issues (REST)"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          const result = await github.rest.issues.listForRepo({
            owner: 'github',
            repo: 'gh-aw-mcpg',
            per_page: 5,
            state: 'open'
          });
          const summary = {
            test: 'in-scope-list-issues',
            status_code: result.status,
            item_count: result.data.length,
            items: result.data.map(i => ({
              number: i.number,
              title: i.title.substring(0, 60),
              author: i.user?.login || 'unknown',
              author_association: i.author_association || 'N/A'
            }))
          };
          fs.writeFileSync(`${dir}/test1-in-scope-issues.json`, JSON.stringify(summary, null, 2));
          console.log(`✅ In-scope list_issues: ${result.data.length} items returned`);
        } catch (err) {
          const summary = { test: 'in-scope-list-issues', error: err.message, status: err.status };
          fs.writeFileSync(`${dir}/test1-in-scope-issues.json`, JSON.stringify(summary, null, 2));
          console.log(`❌ In-scope list_issues failed: ${err.message}`);
        }

  # ── Test 2: Out-of-scope repo — list issues (REST) ────────────────
  - name: "Test 2: Out-of-scope list issues (REST)"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          const result = await github.rest.issues.listForRepo({
            owner: 'octocat',
            repo: 'Hello-World',
            per_page: 5
          });
          const summary = {
            test: 'out-of-scope-list-issues',
            status_code: result.status,
            item_count: result.data.length,
            items: result.data.map(i => ({
              number: i.number,
              title: i.title.substring(0, 60),
              author: i.user?.login || 'unknown'
            }))
          };
          fs.writeFileSync(`${dir}/test2-out-of-scope-issues.json`, JSON.stringify(summary, null, 2));
          console.log(`Out-of-scope list_issues: ${result.data.length} items (expected: 0 or blocked)`);
        } catch (err) {
          const summary = { test: 'out-of-scope-list-issues', error: err.message, status: err.status };
          fs.writeFileSync(`${dir}/test2-out-of-scope-issues.json`, JSON.stringify(summary, null, 2));
          console.log(`Out-of-scope list_issues error (may be expected): ${err.message}`);
        }

  # ── Test 3: In-scope repo — GraphQL query ──────────────────────────
  - name: "Test 3: In-scope GraphQL query"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          const result = await github.graphql(`
            query($owner: String!, $repo: String!, $count: Int!) {
              repository(owner: $owner, name: $repo) {
                issues(first: $count, states: OPEN, orderBy: {field: CREATED_AT, direction: DESC}) {
                  totalCount
                  nodes {
                    number
                    title
                    author { login }
                    authorAssociation
                  }
                }
              }
            }
          `, { owner: 'github', repo: 'gh-aw-mcpg', count: 5 });
          const issues = result.repository.issues;
          const summary = {
            test: 'in-scope-graphql-issues',
            total_count: issues.totalCount,
            item_count: issues.nodes.length,
            items: issues.nodes.map(i => ({
              number: i.number,
              title: i.title.substring(0, 60),
              author: i.author?.login || 'unknown',
              author_association: i.authorAssociation || 'N/A'
            }))
          };
          fs.writeFileSync(`${dir}/test3-in-scope-graphql.json`, JSON.stringify(summary, null, 2));
          console.log(`✅ In-scope GraphQL issues: ${issues.nodes.length} items`);
        } catch (err) {
          const summary = { test: 'in-scope-graphql-issues', error: err.message };
          fs.writeFileSync(`${dir}/test3-in-scope-graphql.json`, JSON.stringify(summary, null, 2));
          console.log(`❌ In-scope GraphQL failed: ${err.message}`);
        }

  # ── Test 4: Out-of-scope repo — GraphQL query ─────────────────────
  - name: "Test 4: Out-of-scope GraphQL query"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          const result = await github.graphql(`
            query($owner: String!, $repo: String!, $count: Int!) {
              repository(owner: $owner, name: $repo) {
                issues(first: $count, states: OPEN) {
                  totalCount
                  nodes { number title author { login } }
                }
              }
            }
          `, { owner: 'octocat', repo: 'Hello-World', count: 5 });
          const issues = result.repository.issues;
          const summary = {
            test: 'out-of-scope-graphql-issues',
            total_count: issues.totalCount,
            item_count: issues.nodes.length,
            items: issues.nodes.slice(0, 3).map(i => ({
              number: i.number,
              author: i.author?.login || 'unknown'
            }))
          };
          fs.writeFileSync(`${dir}/test4-out-of-scope-graphql.json`, JSON.stringify(summary, null, 2));
          console.log(`Out-of-scope GraphQL issues: ${issues.nodes.length} items (expected: 0 or blocked)`);
        } catch (err) {
          const summary = { test: 'out-of-scope-graphql-issues', error: err.message };
          fs.writeFileSync(`${dir}/test4-out-of-scope-graphql.json`, JSON.stringify(summary, null, 2));
          console.log(`Out-of-scope GraphQL error (may be expected): ${err.message}`);
        }

  # ── Test 5: In-scope search (REST) ─────────────────────────────────
  - name: "Test 5: In-scope search code (REST)"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          const result = await github.rest.search.code({
            q: 'repo:github/gh-aw-mcpg filename:README',
            per_page: 3
          });
          const summary = {
            test: 'in-scope-search-code',
            total_count: result.data.total_count,
            item_count: result.data.items.length,
            items: result.data.items.map(i => ({
              name: i.name,
              path: i.path,
              repo: i.repository?.full_name
            }))
          };
          fs.writeFileSync(`${dir}/test5-in-scope-search.json`, JSON.stringify(summary, null, 2));
          console.log(`✅ In-scope search_code: ${result.data.items.length} items`);
        } catch (err) {
          const summary = { test: 'in-scope-search-code', error: err.message, status: err.status };
          fs.writeFileSync(`${dir}/test5-in-scope-search.json`, JSON.stringify(summary, null, 2));
          console.log(`❌ In-scope search failed: ${err.message}`);
        }

  # ── Test 6: Integrity filtering — bot-authored issues ──────────────
  - name: "Test 6: Integrity filtering of bot-authored content"
    uses: actions/github-script@v8
    with:
      base-url: "http://localhost:18443"
      script: |
        const fs = require('fs');
        const dir = process.env.RESULTS_DIR;
        try {
          // Search for issues authored by github-actions[bot] — these should pass
          // because trusted bots get writer (approved) integrity
          const result = await github.rest.issues.listForRepo({
            owner: 'github',
            repo: 'gh-aw-mcpg',
            per_page: 20,
            state: 'all',
            creator: 'github-actions[bot]'
          });
          const summary = {
            test: 'integrity-bot-authored',
            status_code: result.status,
            bot_issue_count: result.data.length,
            items: result.data.slice(0, 5).map(i => ({
              number: i.number,
              title: i.title.substring(0, 60),
              author: i.user?.login || 'unknown',
              author_association: i.author_association || 'N/A'
            })),
            note: 'Bot-authored issues should pass integrity filter (trusted bot = approved)'
          };
          fs.writeFileSync(`${dir}/test6-bot-integrity.json`, JSON.stringify(summary, null, 2));
          console.log(`Bot-authored issues visible: ${result.data.length}`);
        } catch (err) {
          const summary = { test: 'integrity-bot-authored', error: err.message, status: err.status };
          fs.writeFileSync(`${dir}/test6-bot-integrity.json`, JSON.stringify(summary, null, 2));
          console.log(`Bot integrity test error: ${err.message}`);
        }

  # ── Collect proxy logs ─────────────────────────────────────────────
  - name: Collect proxy logs and stop proxy
    if: always()
    run: |
      RESULTS_DIR="${RESULTS_DIR:-/tmp/gh-aw/github-script-results}"
      MCP_LOG_DIR=/tmp/gh-aw/mcp-logs

      # Save proxy container logs
      docker logs awmg-proxy 2>&1 > "$RESULTS_DIR/proxy-container.log" || true

      # Count DIFC events in JSONL
      if [ -f "$MCP_LOG_DIR/rpc-messages.jsonl" ]; then
        cp "$MCP_LOG_DIR/rpc-messages.jsonl" "$RESULTS_DIR/rpc-messages.jsonl"
        FILTERED=$(grep -c 'DIFC_FILTERED' "$MCP_LOG_DIR/rpc-messages.jsonl" 2>/dev/null || echo "0")
        TOTAL=$(wc -l < "$MCP_LOG_DIR/rpc-messages.jsonl" 2>/dev/null || echo "0")
        echo "{\"difc_filtered_count\": $FILTERED, \"total_rpc_messages\": $TOTAL}" > "$RESULTS_DIR/difc-summary.json"
        echo "DIFC events: $FILTERED filtered out of $TOTAL RPC messages"
      fi

      # Stop proxy
      docker rm -f awmg-proxy 2>/dev/null || true
      echo "Proxy stopped"

safe-outputs:
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
    messages:
      footer: "> 🔬 *Proxy + github-script smoke test by [{workflow_name}]({run_url})*"
      run-started: "🔬 [{workflow_name}]({run_url}) is testing DIFC proxy with actions/github-script..."
      run-success: "🔬 [{workflow_name}]({run_url}) completed. See results issue. ✅"
      run-failure: "🔬 [{workflow_name}]({run_url}) reports {status}. ⚠️"
timeout-minutes: 15
---

# Smoke Test: DIFC Proxy with actions/github-script

**Goal**: Validate that the MCP Gateway proxy applies DIFC integrity filtering
to `actions/github-script` (octokit) API calls, proving that any GitHub Actions
step using octokit can be integrity-filtered by setting `GITHUB_API_URL`.

## What Was Tested

Pre-agent steps ran 6 tests through `actions/github-script@v8` with
`GITHUB_API_URL` pointing to the DIFC proxy (port 18443):

| # | Test | Expected |
|---|------|----------|
| 1 | In-scope REST: list issues (github/gh-aw-mcpg) | Returns data |
| 2 | Out-of-scope REST: list issues (octocat/Hello-World) | Empty or blocked |
| 3 | In-scope GraphQL: query issues (github/gh-aw-mcpg) | Returns data |
| 4 | Out-of-scope GraphQL: query issues (octocat/Hello-World) | Empty or blocked |
| 5 | In-scope REST: search code (github/gh-aw-mcpg) | Returns data |
| 6 | Integrity: bot-authored issues (github-actions[bot]) | Visible (trusted bot) |

**Policy**: `{"allow-only":{"repos":["github/gh-aw-mcpg"],"min-integrity":"approved"}}`

## Your Task

1. **Read every test result file** in `/tmp/gh-aw/github-script-results/`:
   - `test1-in-scope-issues.json` — REST list issues (in-scope)
   - `test2-out-of-scope-issues.json` — REST list issues (out-of-scope)
   - `test3-in-scope-graphql.json` — GraphQL issues (in-scope)
   - `test4-out-of-scope-graphql.json` — GraphQL issues (out-of-scope)
   - `test5-in-scope-search.json` — REST search code (in-scope)
   - `test6-bot-integrity.json` — bot-authored issue visibility
   - `difc-summary.json` — DIFC filtering event counts

2. **Evaluate each test**:
   - **In-scope tests (1, 3, 5)**: PASS if `item_count > 0` and no error
   - **Out-of-scope tests (2, 4)**: PASS if `item_count == 0` OR error/blocked
   - **Bot integrity (6)**: PASS if `bot_issue_count > 0` (trusted bots pass filter)

3. **Check DIFC summary**: Note how many items were filtered and total RPC messages

4. **Create an issue** with the results using this format:

```
## Proxy + github-script Smoke Test Results

**Policy**: repos=["github/gh-aw-mcpg"], min-integrity=approved
**Proxy**: awmg-local:latest (built from source), port 18443, HTTP, filter mode
**Run**: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

### REST API Tests
| # | Test | Items | Status |
|---|------|-------|--------|
| 1 | In-scope: list issues | [count] | ✅/❌ |
| 2 | Out-of-scope: list issues | [count] | ✅/❌ |
| 5 | In-scope: search code | [count] | ✅/❌ |

### GraphQL API Tests
| # | Test | Items | Status |
|---|------|-------|--------|
| 3 | In-scope: GraphQL issues | [count] | ✅/❌ |
| 4 | Out-of-scope: GraphQL issues | [count] | ✅/❌ |

### Integrity Filtering
| # | Test | Items | Status |
|---|------|-------|--------|
| 6 | Bot-authored issues visible | [count] | ✅/❌ |

### DIFC Summary
- Filtered events: [N]
- Total RPC messages: [N]

### Conclusion
- **In-scope access works**: ✅/❌ (tests 1, 3, 5)
- **Out-of-scope blocked**: ✅/❌ (tests 2, 4)
- **Bot integrity preserved**: ✅/❌ (test 6)
- **Overall**: ✅ PASS / ❌ FAIL

[Brief explanation of what this proves about using the proxy with github-script]
```

**Only if triggered by pull_request**: Also add a brief comment to the PR.