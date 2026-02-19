---
name: Language Support Tester
description: Daily workflow that tests Go, JavaScript, and Python language support using the Serena MCP server
on:
  schedule: daily
permissions:
  contents: read
  issues: read
  pull-requests: read
network:
  allowed:
    - defaults
    - containers
steps:
  - name: Set up Go
    uses: actions/setup-go@4dc6199c7b1a012772edbd06daecab0f50c9053c # v6
    with:
      go-version-file: go.mod
      cache: true
  - name: Set up Docker Buildx
    uses: docker/setup-buildx-action@v3
  - name: Build local MCP Gateway container
    run: |
      VERSION="dev-$(git rev-parse --short HEAD)"
      docker build -t local-awmg:v0.1.4 --build-arg VERSION=${VERSION} .
      echo "✅ Built local MCP Gateway container: local-awmg:v0.1.4 (VERSION=${VERSION})"
  - name: Pull Serena MCP Server Container
    run: docker pull ghcr.io/github/serena-mcp-server:latest
tools:
  serena: ["go", "typescript", "python"]
  github:
    toolsets: [default]

sandbox:
  mcp:
    container: "local-awmg"

safe-outputs:
  create-issue:
    title-prefix: "[language-support] "
    labels: [language-support, serena-mcp, automation]
    expires: 7d
timeout-minutes: 15
strict: true
---

<!-- Edit the file linked below to modify the agent without recompilation. Feel free to move the entire markdown body to that file. -->
@./agentics/language-support-tester.md
