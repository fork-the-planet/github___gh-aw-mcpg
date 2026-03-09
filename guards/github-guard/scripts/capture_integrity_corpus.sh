#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CORPUS_FILE="$ROOT_DIR/src/testdata/integrity/corpus_v1.json"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: gh CLI is required"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required"
  exit 1
fi

echo "Capturing open-source corpus data with gh api..."

gh api 'repos/cli/cli/commits?per_page=2' > "$TMP_DIR/cli_commits.json"
gh api 'repos/octocat/Hello-World/commits?per_page=2' > "$TMP_DIR/octocat_commits.json"

CLI_COMMITS_JSON="$(jq '[.[0:2][] | {sha, html_url}]' "$TMP_DIR/cli_commits.json")"
OCTO_COMMITS_JSON="$(jq '[.[0:2][] | {sha, html_url}]' "$TMP_DIR/octocat_commits.json")"

CLI_SHA0="$(jq -r '.[0].sha' "$TMP_DIR/cli_commits.json")"
CLI_SHA1="$(jq -r '.[1].sha' "$TMP_DIR/cli_commits.json")"
OCTO_SHA0="$(jq -r '.[0].sha' "$TMP_DIR/octocat_commits.json")"
OCTO_SHA1="$(jq -r '.[1].sha' "$TMP_DIR/octocat_commits.json")"

CLI_SHORT0="${CLI_SHA0:0:7}"
CLI_SHORT1="${CLI_SHA1:0:7}"
OCTO_SHORT0="${OCTO_SHA0:0:7}"
OCTO_SHORT1="${OCTO_SHA1:0:7}"

CAPTURED_AT="$(date +%Y-%m-%d)"

mkdir -p "$(dirname "$CORPUS_FILE")"
cat > "$CORPUS_FILE" <<EOF
{
  "version": "v1",
  "captured_at": "${CAPTURED_AT}",
  "source_repositories": [
    "cli/cli",
    "octocat/Hello-World"
  ],
  "allow_only_policy": {
    "repos": [
      "cli/cli",
      "cli/gh-*"
    ],
    "min-integrity": "merged"
  },
  "expected_agent": {
    "difc_mode": "filter",
    "normalized_policy": {
      "scope_kind": "Composite",
      "min-integrity": "merged"
    },
    "secrecy": [
      "private:cli/cli",
      "private:cli/gh-*"
    ],
    "integrity": [
      "integrity=none;scopes=cli/cli,cli/gh-*",
      "integrity=unapproved;scopes=cli/cli,cli/gh-*",
      "integrity=approved;scopes=cli/cli,cli/gh-*",
      "integrity=merged;scopes=cli/cli,cli/gh-*"
    ]
  },
  "backend_replay": [
    {
      "tool": "search_repositories",
      "args": {
        "query": "repo:cli/cli",
        "perPage": 10
      },
      "response": {
        "items": [
          {
            "full_name": "cli/cli",
            "private": false
          }
        ]
      }
    },
    {
      "tool": "search_repositories",
      "args": {
        "query": "repo:octocat/Hello-World",
        "perPage": 10
      },
      "response": {
        "items": [
          {
            "full_name": "octocat/Hello-World",
            "private": false
          }
        ]
      }
    }
  ],
  "resource_cases": [
    {
      "name": "in_scope_list_commits",
      "tool_name": "list_commits",
      "tool_args": {
        "owner": "cli",
        "repo": "cli",
        "perPage": 2
      },
      "expected": {
        "operation": "read",
        "description": "resource:list_commits",
        "secrecy": [],
        "integrity": [
          "integrity=none;scopes=cli/cli,cli/gh-*",
          "integrity=unapproved;scopes=cli/cli,cli/gh-*",
          "integrity=approved;scopes=cli/cli,cli/gh-*",
          "integrity=merged;scopes=cli/cli,cli/gh-*"
        ]
      }
    },
    {
      "name": "out_of_scope_list_commits",
      "tool_name": "list_commits",
      "tool_args": {
        "owner": "octocat",
        "repo": "Hello-World",
        "perPage": 2
      },
      "expected": {
        "operation": "read",
        "description": "resource:list_commits",
        "secrecy": [],
        "integrity": [
          "none:octocat/Hello-World",
          "unapproved:octocat/Hello-World",
          "approved:octocat/Hello-World",
          "merged:octocat/Hello-World"
        ]
      }
    }
  ],
  "response_cases": [
    {
      "name": "in_scope_list_commits_response",
      "tool_name": "list_commits",
      "tool_args": {
        "owner": "cli",
        "repo": "cli",
        "perPage": 2
      },
      "tool_result": ${CLI_COMMITS_JSON},
      "expected_paths": [
        {
          "path": "/0",
          "labels": {
            "description": "commit:cli/cli@${CLI_SHORT0}",
            "secrecy": [],
            "integrity": [
              "integrity=none;scopes=cli/cli,cli/gh-*",
              "integrity=unapproved;scopes=cli/cli,cli/gh-*",
              "integrity=approved;scopes=cli/cli,cli/gh-*",
              "integrity=merged;scopes=cli/cli,cli/gh-*"
            ]
          }
        },
        {
          "path": "/1",
          "labels": {
            "description": "commit:cli/cli@${CLI_SHORT1}",
            "secrecy": [],
            "integrity": [
              "integrity=none;scopes=cli/cli,cli/gh-*",
              "integrity=unapproved;scopes=cli/cli,cli/gh-*",
              "integrity=approved;scopes=cli/cli,cli/gh-*",
              "integrity=merged;scopes=cli/cli,cli/gh-*"
            ]
          }
        }
      ],
      "expected_default": {
        "description": "commit",
        "secrecy": [],
        "integrity": [
          "integrity=none;scopes=cli/cli,cli/gh-*",
          "integrity=unapproved;scopes=cli/cli,cli/gh-*",
          "integrity=approved;scopes=cli/cli,cli/gh-*",
          "integrity=merged;scopes=cli/cli,cli/gh-*"
        ]
      }
    },
    {
      "name": "out_of_scope_list_commits_response",
      "tool_name": "list_commits",
      "tool_args": {
        "owner": "octocat",
        "repo": "Hello-World",
        "perPage": 2
      },
      "tool_result": ${OCTO_COMMITS_JSON},
      "expected_paths": [
        {
          "path": "/0",
          "labels": {
            "description": "commit:octocat/Hello-World@${OCTO_SHORT0}",
            "secrecy": [],
            "integrity": [
              "none:octocat/Hello-World",
              "unapproved:octocat/Hello-World",
              "approved:octocat/Hello-World",
              "merged:octocat/Hello-World"
            ]
          }
        },
        {
          "path": "/1",
          "labels": {
            "description": "commit:octocat/Hello-World@${OCTO_SHORT1}",
            "secrecy": [],
            "integrity": [
              "none:octocat/Hello-World",
              "unapproved:octocat/Hello-World",
              "approved:octocat/Hello-World",
              "merged:octocat/Hello-World"
            ]
          }
        }
      ],
      "expected_default": {
        "description": "commit",
        "secrecy": [],
        "integrity": [
          "none:octocat/Hello-World",
          "unapproved:octocat/Hello-World",
          "approved:octocat/Hello-World",
          "merged:octocat/Hello-World"
        ]
      }
    }
  ]
}
EOF

echo "Updated corpus fixture: $CORPUS_FILE"