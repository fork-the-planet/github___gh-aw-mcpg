package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGuardPolicies_ReposAllFormat tests repos field with "all" value
func TestGuardPolicies_ReposAllFormat(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": "all", "min-integrity": "unapproved"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with repos='all'")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	assert.Equal(t, "all", githubPolicy["repos"], "repos should be 'all'")
	assert.Equal(t, "unapproved", githubPolicy["min-integrity"], "min-integrity should be 'unapproved'")
}

// TestGuardPolicies_ReposPublicFormat tests repos field with "public" value
func TestGuardPolicies_ReposPublicFormat(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": "public", "min-integrity": "none"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with repos='public'")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	assert.Equal(t, "public", githubPolicy["repos"], "repos should be 'public'")
	assert.Equal(t, "none", githubPolicy["min-integrity"], "min-integrity should be 'none'")
}

// TestGuardPolicies_ReposWithWildcards tests repos field with wildcard patterns
func TestGuardPolicies_ReposWithWildcards(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": ["myorg/*", "partner/shared-repo", "docs/api-*"], "min-integrity": "approved"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with wildcard patterns")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	repos := githubPolicy["repos"].([]interface{})
	assert.Len(t, repos, 3, "repos should have 3 patterns")
	assert.Equal(t, "myorg/*", repos[0], "First pattern should be 'myorg/*'")
	assert.Equal(t, "partner/shared-repo", repos[1], "Second pattern should be exact match")
	assert.Equal(t, "docs/api-*", repos[2], "Third pattern should be prefix wildcard")
	assert.Equal(t, "approved", githubPolicy["min-integrity"], "min-integrity should be 'approved'")
}

// TestGuardPolicies_AllMinIntegrityLevels tests all valid min-integrity values
func TestGuardPolicies_AllMinIntegrityLevels(t *testing.T) {
	testCases := []struct {
		name         string
		minIntegrity string
	}{
		{"none", "none"},
		{"unapproved", "unapproved"},
		{"approved", "approved"},
		{"merged", "merged"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": "all", "min-integrity": "` + tc.minIntegrity + `"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

			r, w, _ := os.Pipe()
			oldStdin := os.Stdin
			os.Stdin = r
			go func() {
				w.Write([]byte(jsonConfig))
				w.Close()
			}()

			cfg, err := LoadFromStdin()
			os.Stdin = oldStdin

			require.NoError(t, err, "LoadFromStdin() should succeed with min-integrity='%s'", tc.minIntegrity)
			require.NotNil(t, cfg, "Config should not be nil")

			server := cfg.Servers["github"]
			githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
			assert.Equal(t, tc.minIntegrity, githubPolicy["min-integrity"], "min-integrity should be '%s'", tc.minIntegrity)
		})
	}
}

// TestGuardPolicies_TOML_ReposAllFormat tests TOML format with repos="all"
func TestGuardPolicies_TOML_ReposAllFormat(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.github.guard_policies.github]
repos = "all"
min-integrity = "unapproved"
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err, "LoadFromFile() should succeed with repos='all'")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	assert.Equal(t, "all", githubPolicy["repos"], "repos should be 'all' in TOML")
	assert.Equal(t, "unapproved", githubPolicy["min-integrity"], "min-integrity should be 'unapproved' in TOML")
}

// TestGuardPolicies_TOML_ReposWithWildcards tests TOML format with wildcard patterns
func TestGuardPolicies_TOML_ReposWithWildcards(t *testing.T) {
	path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.github.guard_policies.github]
repos = ["myorg/*", "partner/shared-repo", "docs/api-*"]
min-integrity = "approved"
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err, "LoadFromFile() should succeed with wildcard patterns")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	repos := githubPolicy["repos"].([]interface{})
	assert.Len(t, repos, 3, "repos should have 3 patterns in TOML")
	assert.Equal(t, "myorg/*", repos[0], "First pattern should be 'myorg/*' in TOML")
	assert.Equal(t, "partner/shared-repo", repos[1], "Second pattern should be exact match in TOML")
	assert.Equal(t, "docs/api-*", repos[2], "Third pattern should be prefix wildcard in TOML")
	assert.Equal(t, "approved", githubPolicy["min-integrity"], "min-integrity should be 'approved' in TOML")
}

// TestGuardPolicies_TOML_AllMinIntegrityLevels tests all valid min-integrity values in TOML
func TestGuardPolicies_TOML_AllMinIntegrityLevels(t *testing.T) {
	testCases := []struct {
		name         string
		minIntegrity string
	}{
		{"none", "none"},
		{"unapproved", "unapproved"},
		{"approved", "approved"},
		{"merged", "merged"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempTOML(t, `
[servers.github]
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/github-mcp-server:latest"]

[servers.github.guard_policies.github]
repos = "all"
min-integrity = "`+tc.minIntegrity+`"
`)

			cfg, err := LoadFromFile(path)
			require.NoError(t, err, "LoadFromFile() should succeed with min-integrity='%s' in TOML", tc.minIntegrity)
			require.NotNil(t, cfg, "Config should not be nil")

			server := cfg.Servers["github"]
			githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
			assert.Equal(t, tc.minIntegrity, githubPolicy["min-integrity"], "min-integrity should be '%s' in TOML", tc.minIntegrity)
		})
	}
}

// TestGuardPolicies_ExactRepoPatterns tests exact repository pattern matching
func TestGuardPolicies_ExactRepoPatterns(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": ["github/gh-aw-mcpg", "github/gh-aw", "frontend/ui-components"], "min-integrity": "merged"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with exact patterns")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	repos := githubPolicy["repos"].([]interface{})
	assert.Len(t, repos, 3, "repos should have 3 exact patterns")
	assert.Equal(t, "github/gh-aw-mcpg", repos[0], "First pattern should be exact match")
	assert.Equal(t, "github/gh-aw", repos[1], "Second pattern should be exact match")
	assert.Equal(t, "frontend/ui-components", repos[2], "Third pattern should be exact match")
	assert.Equal(t, "merged", githubPolicy["min-integrity"], "min-integrity should be 'merged'")
}

// TestGuardPolicies_MixedPatterns tests combination of exact matches and wildcards
func TestGuardPolicies_MixedPatterns(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {"github": {"repos": ["github/gh-aw-mcpg", "myorg/*", "partner/shared-*", "docs/api-reference"], "min-integrity": "approved"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with mixed patterns")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	repos := githubPolicy["repos"].([]interface{})
	assert.Len(t, repos, 4, "repos should have 4 mixed patterns")
	assert.Equal(t, "github/gh-aw-mcpg", repos[0], "First should be exact match")
	assert.Equal(t, "myorg/*", repos[1], "Second should be org wildcard")
	assert.Equal(t, "partner/shared-*", repos[2], "Third should be prefix wildcard")
	assert.Equal(t, "docs/api-reference", repos[3], "Fourth should be exact match")
}

// TestGuardPolicies_EmptyGuardPolicies tests that empty guard-policies is allowed
func TestGuardPolicies_EmptyGuardPolicies(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "guard-policies": {}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with empty guard-policies")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil even when empty")
	assert.Empty(t, server.GuardPolicies, "GuardPolicies map should be empty")
}

// TestGuardPolicies_MissingGuardPolicies tests that missing guard-policies is allowed
func TestGuardPolicies_MissingGuardPolicies(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest"}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed without guard-policies")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]
	// GuardPolicies should be nil when not specified
	assert.Nil(t, server.GuardPolicies, "GuardPolicies should be nil when not specified")
}

// TestGuardPolicies_PreservesOtherServerConfig tests that guard policies don't interfere with other config
func TestGuardPolicies_PreservesOtherServerConfig(t *testing.T) {
	jsonConfig := `{"mcpServers": {"github": {"type": "stdio", "container": "ghcr.io/github/github-mcp-server:latest", "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "", "DEBUG": "true"}, "guard-policies": {"github": {"repos": ["myorg/*"], "min-integrity": "unapproved"}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["github"]

	// Verify guard policies are present
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")
	githubPolicy := server.GuardPolicies["github"].(map[string]interface{})
	assert.NotNil(t, githubPolicy, "GitHub policy should exist")

	// Verify other config is preserved
	assert.Equal(t, "docker", server.Command, "Command should be 'docker'")
	assert.Contains(t, server.Args, "ghcr.io/github/github-mcp-server:latest", "Container should be in args")

	// Check environment variables are present
	hasDebug := false
	hasToken := false
	for i := 0; i < len(server.Args); i++ {
		if server.Args[i] == "-e" && i+1 < len(server.Args) {
			if server.Args[i+1] == "DEBUG=true" {
				hasDebug = true
			}
			if server.Args[i+1] == "GITHUB_PERSONAL_ACCESS_TOKEN" {
				hasToken = true
			}
		}
	}
	assert.True(t, hasDebug, "DEBUG env var should be present")
	assert.True(t, hasToken, "GITHUB_PERSONAL_ACCESS_TOKEN should be present")
}

// TestGuardPolicies_WriteSink tests write-sink guard policy via JSON stdin
func TestGuardPolicies_WriteSink(t *testing.T) {
	jsonConfig := `{"mcpServers": {"safeoutputs": {"type": "stdio", "container": "ghcr.io/github/safe-outputs:latest", "guard-policies": {"write-sink": {"accept": ["private:github/gh-aw*"]}}}}, "gateway": {"port": 3000, "domain": "localhost", "apiKey": "test-key"}}`

	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(jsonConfig))
		w.Close()
	}()

	cfg, err := LoadFromStdin()
	os.Stdin = oldStdin

	require.NoError(t, err, "LoadFromStdin() should succeed with write-sink policy")
	require.NotNil(t, cfg, "Config should not be nil")

	server := cfg.Servers["safeoutputs"]
	require.NotNil(t, server.GuardPolicies, "GuardPolicies should not be nil")

	writeSinkRaw, ok := server.GuardPolicies["write-sink"]
	require.True(t, ok, "write-sink key should exist in guard-policies")
	writeSinkMap := writeSinkRaw.(map[string]interface{})
	acceptRaw := writeSinkMap["accept"].([]interface{})
	assert.Len(t, acceptRaw, 1)
	assert.Equal(t, "private:github/gh-aw*", acceptRaw[0])
}

// TestGuardPolicies_WriteSinkTOML tests write-sink guard policy via TOML file
func TestGuardPolicies_WriteSinkTOML(t *testing.T) {
	path := writeTempTOML(t, `
[gateway]
port = 3000
api_key = "test-key"
domain = "localhost"

[servers.safeoutputs]
type = "stdio"
command = "docker"
args = ["run", "--rm", "-i", "ghcr.io/github/safe-outputs:latest"]

[servers.safeoutputs.guard_policies.write-sink]
Accept = ["private:github/gh-aw*"]
`)

	cfg, err := LoadFromFile(path)
	require.NoError(t, err, "LoadFromFile() should succeed with write-sink TOML config")
	require.NotNil(t, cfg)

	server := cfg.Servers["safeoutputs"]
	require.NotNil(t, server)
	require.NotNil(t, server.GuardPolicies)

	writeSinkRaw, ok := server.GuardPolicies["write-sink"]
	require.True(t, ok, "write-sink key should exist in guard-policies")
	writeSinkMap := writeSinkRaw.(map[string]interface{})
	acceptRaw := writeSinkMap["Accept"].([]interface{})
	assert.Len(t, acceptRaw, 1)
	assert.Equal(t, "private:github/gh-aw*", acceptRaw[0])
}

// =============================================================================
// Write-Sink Accept Entry Validation Tests
//
// These tests verify that validateAcceptEntry correctly validates all accept
// entry formats, including bare owner names (for owner-wildcard scopes).
// =============================================================================

func TestValidateAcceptEntry_AllFormats(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		wantErr bool
		desc    string
	}{
		// Valid: exact repo with visibility prefix
		{
			name:  "private:owner/repo",
			entry: "private:github/gh-aw",
			desc:  "exact repo with private prefix",
		},
		{
			name:  "public:owner/repo",
			entry: "public:github/gh-aw",
			desc:  "exact repo with public prefix",
		},
		{
			name:  "internal:owner/repo",
			entry: "internal:github/gh-aw",
			desc:  "exact repo with internal prefix",
		},
		// Valid: prefix wildcard with visibility prefix
		{
			name:  "private:owner/prefix*",
			entry: "private:github/gh-aw*",
			desc:  "prefix wildcard (repos=[\"github/gh-aw*\"])",
		},
		// Valid: owner wildcard with visibility prefix
		{
			name:  "private:owner/*",
			entry: "private:github/*",
			desc:  "owner wildcard scope (repos=[\"github/*\"])",
		},
		// Valid: bare owner (for repos=["owner/*"] → agent secrecy "private:owner")
		{
			name:  "private:owner (bare)",
			entry: "private:myorg",
			desc:  "bare owner name — matches repos=[\"myorg/*\"] agent secrecy",
		},
		{
			name:  "private:owner with hyphen",
			entry: "private:my-org",
			desc:  "bare owner with hyphen",
		},
		{
			name:  "private:owner with numbers",
			entry: "private:org123",
			desc:  "bare owner with numbers",
		},
		// Valid: without visibility prefix
		{
			name:  "owner/repo (no prefix)",
			entry: "github/gh-aw",
			desc:  "repo scope without visibility prefix",
		},
		{
			name:  "owner/prefix* (no prefix)",
			entry: "github/gh-aw*",
			desc:  "prefix wildcard without visibility prefix",
		},
		{
			name:  "owner/* (no prefix)",
			entry: "github/*",
			desc:  "owner wildcard without visibility prefix",
		},
		{
			name:  "owner (no prefix, bare)",
			entry: "myorg",
			desc:  "bare owner without visibility prefix",
		},
		// Invalid entries
		{
			name:    "invalid visibility prefix",
			entry:   "secret:github/gh-aw",
			wantErr: true,
			desc:    "unknown visibility prefix",
		},
		{
			name:    "empty string",
			entry:   "",
			wantErr: true,
			desc:    "empty entry is invalid",
		},
		{
			name:    "wildcard in middle",
			entry:   "private:github/gh*aw",
			wantErr: true,
			desc:    "wildcard must be at end only",
		},
		{
			name:    "double wildcard",
			entry:   "private:github/gh**",
			wantErr: true,
			desc:    "multiple wildcards not allowed",
		},
		{
			name:    "owner too long",
			entry:   "private:" + string(make([]byte, 40)),
			wantErr: true,
			desc:    "owner name exceeds 39 chars",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAcceptEntry(tc.entry)
			if tc.wantErr {
				assert.Error(t, err, "entry %q should be invalid: %s", tc.entry, tc.desc)
			} else {
				assert.NoError(t, err, "entry %q should be valid: %s", tc.entry, tc.desc)
			}
		})
	}
}

// TestValidateWriteSinkPolicy_BareOwnerAccept tests that a write-sink policy with
// bare owner accept entries (for repos=["owner/*"]) passes validation.
func TestValidateWriteSinkPolicy_BareOwnerAccept(t *testing.T) {
	policy := &WriteSinkPolicy{
		Accept: []string{"private:myorg", "private:partner/shared-lib"},
	}
	err := ValidateWriteSinkPolicy(policy)
	assert.NoError(t, err, "bare owner + repo pattern should both validate")
}

// TestValidateWriteSinkPolicy_AllScopeAcceptFormats tests validation for accept
// entries derived from every repos scope type.
func TestValidateWriteSinkPolicy_AllScopeAcceptFormats(t *testing.T) {
	tests := []struct {
		name   string
		repos  string // description of the repos config
		accept []string
	}{
		{
			name:   "exact repo",
			repos:  `["github/gh-aw"]`,
			accept: []string{"private:github/gh-aw"},
		},
		{
			name:   "owner wildcard",
			repos:  `["myorg/*"]`,
			accept: []string{"private:myorg"},
		},
		{
			name:   "prefix wildcard",
			repos:  `["github/gh-aw*"]`,
			accept: []string{"private:github/gh-aw*"},
		},
		{
			name:   "multiple exact repos",
			repos:  `["github/repo1", "github/repo2"]`,
			accept: []string{"private:github/repo1", "private:github/repo2"},
		},
		{
			name:   "mixed owner + exact",
			repos:  `["myorg/*", "partner/lib"]`,
			accept: []string{"private:myorg", "private:partner/lib"},
		},
		{
			name:   "mixed prefix + exact + owner",
			repos:  `["github/gh-aw*", "github/copilot", "partner/*"]`,
			accept: []string{"private:github/gh-aw*", "private:github/copilot", "private:partner"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := &WriteSinkPolicy{Accept: tc.accept}
			err := ValidateWriteSinkPolicy(policy)
			assert.NoError(t, err,
				"repos=%s → accept=%v should pass validation", tc.repos, tc.accept)
		})
	}
}

// TestValidateWriteSinkPolicy_WildcardAccept tests that accept=["*"] passes validation.
func TestValidateWriteSinkPolicy_WildcardAccept(t *testing.T) {
	policy := &WriteSinkPolicy{Accept: []string{"*"}}
	err := ValidateWriteSinkPolicy(policy)
	assert.NoError(t, err, `accept=["*"] should be valid (wildcard)`)
}

// TestValidateWriteSinkPolicy_WildcardWithOtherEntries tests that "*" cannot
// be mixed with other accept entries.
func TestValidateWriteSinkPolicy_WildcardWithOtherEntries(t *testing.T) {
	policy := &WriteSinkPolicy{Accept: []string{"*", "private:org/repo"}}
	err := ValidateWriteSinkPolicy(policy)
	assert.Error(t, err, `accept=["*", "private:org/repo"] should be invalid`)
	assert.ErrorContains(t, err, "wildcard")
}

// TestValidateWriteSinkPolicy_WildcardNotFirst tests that "*" anywhere in a
// multi-entry list is rejected.
func TestValidateWriteSinkPolicy_WildcardNotFirst(t *testing.T) {
	policy := &WriteSinkPolicy{Accept: []string{"private:org/repo", "*"}}
	err := ValidateWriteSinkPolicy(policy)
	assert.Error(t, err, `accept=["private:org/repo", "*"] should be invalid`)
	assert.ErrorContains(t, err, "wildcard")
}
