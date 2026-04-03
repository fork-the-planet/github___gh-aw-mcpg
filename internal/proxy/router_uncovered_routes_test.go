package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMatchRoute_UncoveredRoutes covers route patterns not exercised by the
// existing TestMatchRoute and TestMatchRoute_AdditionalRoutes tests:
//
//   - Environment-scoped Actions secrets and variables (list_environment_config)
//   - Org-scoped Actions secrets, variables, and named variable (list_org_config)
//   - Individual discussion and discussion comments (list_discussions, get_discussion_comments)
//   - Commit check-suites (same tool as check-runs: pull_request_read)
//   - User SSH signing keys and GPG keys (get_me)
//   - Actions variable accessed by name (list_variables with optional suffix)
//   - Paths that match no route (nil return)
func TestMatchRoute_UncoveredRoutes(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantTool string
		wantArgs map[string]interface{}
		wantNil  bool
	}{
		// Environment-scoped Actions configuration
		{
			name:     "environment secrets",
			path:     "/repos/org/repo/environments/production/secrets",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "method": "list_environment_config"},
		},
		{
			name:     "environment variables",
			path:     "/repos/org/repo/environments/staging/variables",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "method": "list_environment_config"},
		},
		{
			name:     "environment secrets with hyphenated environment name",
			path:     "/repos/github/my-app/environments/prod-east-1/secrets",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "github", "repo": "my-app", "method": "list_environment_config"},
		},

		// Org-scoped Actions secrets and variables
		{
			name:     "org secrets",
			path:     "/orgs/myorg/actions/secrets",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "myorg", "method": "list_org_config"},
		},
		{
			name:     "org variables",
			path:     "/orgs/myorg/actions/variables",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "myorg", "method": "list_org_config"},
		},
		{
			name:     "org named variable",
			path:     "/orgs/myorg/actions/variables/MY_VAR",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "myorg", "method": "list_org_config"},
		},
		{
			name:     "org named secret",
			path:     "/orgs/myorg/actions/secrets/MY_SECRET",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "myorg", "method": "list_org_config"},
		},

		// Discussion detail (single discussion) and discussion comments
		{
			name:     "get single discussion",
			path:     "/repos/org/repo/discussions/42",
			wantTool: "list_discussions",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "discussion_number": "42"},
		},
		{
			name:     "get discussion comments",
			path:     "/repos/org/repo/discussions/7/comments",
			wantTool: "get_discussion_comments",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "discussion_number": "7"},
		},
		{
			name:     "get discussion comments — large number",
			path:     "/repos/github/gh-aw/discussions/1234/comments",
			wantTool: "get_discussion_comments",
			wantArgs: map[string]interface{}{"owner": "github", "repo": "gh-aw", "discussion_number": "1234"},
		},

		// Commit check-suites — same tool as check-runs
		{
			name:     "commit check-suites",
			path:     "/repos/org/repo/commits/abc123/check-suites",
			wantTool: "pull_request_read",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "sha": "abc123", "method": "get_check_runs"},
		},
		{
			name:     "commit check-suites with full SHA",
			path:     "/repos/github/copilot/commits/deadbeefdeadbeef1234567890abcdef12345678/check-suites",
			wantTool: "pull_request_read",
			wantArgs: map[string]interface{}{"owner": "github", "repo": "copilot", "sha": "deadbeefdeadbeef1234567890abcdef12345678", "method": "get_check_runs"},
		},

		// User SSH signing keys and GPG keys
		{
			name:     "user SSH signing keys",
			path:     "/user/ssh_signing_keys",
			wantTool: "get_me",
			wantArgs: map[string]interface{}{},
		},
		{
			name:     "user GPG keys",
			path:     "/user/gpg_keys",
			wantTool: "get_me",
			wantArgs: map[string]interface{}{},
		},

		// Actions variable accessed by name (optional suffix in pattern)
		{
			name:     "actions variable by name",
			path:     "/repos/org/repo/actions/variables/MY_VARIABLE",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "method": "list_variables"},
		},

		// Query string is stripped before matching
		{
			name:     "environment secrets with query string",
			path:     "/repos/org/repo/environments/prod/secrets?per_page=30",
			wantTool: "actions_list",
			wantArgs: map[string]interface{}{"owner": "org", "repo": "repo", "method": "list_environment_config"},
		},

		// Paths that should return nil (no route matches)
		{
			name:    "completely unknown path",
			path:    "/unknown-path",
			wantNil: true,
		},
		{
			name:    "unknown search endpoint",
			path:    "/search/users",
			wantNil: true,
		},
		{
			name:    "repos without owner or repo",
			path:    "/repos",
			wantNil: true,
		},
		{
			name:    "repos without repo",
			path:    "/repos/only-owner",
			wantNil: true,
		},
		{
			name:    "orgs without actions path",
			path:    "/orgs/myorg/members",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := MatchRoute(tt.path)
			if tt.wantNil {
				assert.Nil(t, match, "expected no route match for %s", tt.path)
				return
			}
			require.NotNil(t, match, "expected route match for %s", tt.path)
			assert.Equal(t, tt.wantTool, match.ToolName, "tool name mismatch for %s", tt.path)
			assert.Equal(t, tt.wantArgs, match.Args, "args mismatch for %s", tt.path)
		})
	}
}
