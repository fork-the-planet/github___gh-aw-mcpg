---
name: Nightly Workflow Compiler
description: Nightly workflow that ensures all workflows are compiled with the latest agentic workflow release
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
network:
  allowed:
    - defaults
tools:
  github:
    toolsets: [default]
  bash: ["*"]
safe-outputs:
  create-pull-request:
    title-prefix: "🤖 "
    labels: [agentic-workflows, automation, maintenance]
    draft: true
    expires: 7d
  missing-tool:
    create-issue: true
timeout-minutes: 15
---

<!-- Edit the file linked below to modify the agent without recompilation. Feel free to move the entire markdown body to that file. -->
@./agentics/nightly-workflow-compiler.md
