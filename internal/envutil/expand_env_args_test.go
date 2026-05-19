package envutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandEnvArgs tests ExpandEnvArgs with various -e flag combinations.
func TestExpandEnvArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		envVars map[string]string
		want    []string
	}{
		{
			name: "nil input returns empty slice",
			args: nil,
			want: []string{},
		},
		{
			name: "empty input returns empty slice",
			args: []string{},
			want: []string{},
		},
		{
			name: "args without -e flag pass through unchanged",
			args: []string{"run", "--rm", "-i", "ghcr.io/org/image:latest"},
			want: []string{"run", "--rm", "-i", "ghcr.io/org/image:latest"},
		},
		{
			name:    "-e VAR_NAME is expanded when env var is set",
			args:    []string{"-e", "MY_TOKEN"},
			envVars: map[string]string{"MY_TOKEN": "secret-value"},
			want:    []string{"-e", "MY_TOKEN=secret-value"},
		},
		{
			name: "-e VAR_NAME is left unchanged when env var is not set",
			args: []string{"-e", "_DOCKERENV_TEST_TRULY_UNSET_XYZ999"},
			want: []string{"-e", "_DOCKERENV_TEST_TRULY_UNSET_XYZ999"},
		},
		{
			name:    "-e VAR=VALUE (already has =) is passed through unchanged",
			args:    []string{"-e", "MY_VAR=already-set"},
			envVars: map[string]string{"MY_VAR": "other-value"},
			want:    []string{"-e", "MY_VAR=already-set"},
		},
		{
			name: "-e at end of args (no following value) is passed through",
			args: []string{"run", "-e"},
			want: []string{"run", "-e"},
		},
		{
			name:    "multiple -e flags are all expanded",
			args:    []string{"-e", "TOKEN_A", "-e", "TOKEN_B"},
			envVars: map[string]string{"TOKEN_A": "val-a", "TOKEN_B": "val-b"},
			want:    []string{"-e", "TOKEN_A=val-a", "-e", "TOKEN_B=val-b"},
		},
		{
			name:    "mix of set and unset env vars in -e flags",
			args:    []string{"-e", "SET_VAR", "-e", "_DOCKERENV_TEST_TRULY_UNSET_XYZ999"},
			envVars: map[string]string{"SET_VAR": "value"},
			want:    []string{"-e", "SET_VAR=value", "-e", "_DOCKERENV_TEST_TRULY_UNSET_XYZ999"},
		},
		{
			name:    "realistic docker run command with env var expansion",
			args:    []string{"run", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "-i", "ghcr.io/github/github-mcp-server:latest"},
			envVars: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_abc123"},
			want:    []string{"run", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_abc123", "-i", "ghcr.io/github/github-mcp-server:latest"},
		},
		{
			name: "-e VAR where VAR is empty string is passed through unchanged",
			args: []string{"-e", ""},
			want: []string{"-e", ""},
		},
		{
			name:    "env var with empty value is expanded with empty value",
			args:    []string{"-e", "EMPTY_VAR"},
			envVars: map[string]string{"EMPTY_VAR": ""},
			want:    []string{"-e", "EMPTY_VAR="},
		},
		{
			name:    "env var value containing = is expanded correctly",
			args:    []string{"-e", "URL_VAR"},
			envVars: map[string]string{"URL_VAR": "https://example.com?key=val"},
			want:    []string{"-e", "URL_VAR=https://example.com?key=val"},
		},
		{
			name:    "non -e flags between expanded flags are preserved",
			args:    []string{"-e", "VAR_A", "--name", "mycontainer", "-e", "VAR_B"},
			envVars: map[string]string{"VAR_A": "alpha", "VAR_B": "beta"},
			want:    []string{"-e", "VAR_A=alpha", "--name", "mycontainer", "-e", "VAR_B=beta"},
		},
		{
			name:    "same var referenced multiple times is expanded each time",
			args:    []string{"-e", "SHARED_VAR", "-e", "SHARED_VAR"},
			envVars: map[string]string{"SHARED_VAR": "shared"},
			want:    []string{"-e", "SHARED_VAR=shared", "-e", "SHARED_VAR=shared"},
		},
		{
			name: "-e immediately followed by another -e flag where first -e var is unset",
			args: []string{"-e", "-e"},
			// The first "-e" tries to expand the second "-e" as a var name.
			// Since no env var named "-e" is set, the expansion is skipped.
			// Both "-e" args are emitted as-is.
			want: []string{"-e", "-e"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got := ExpandEnvArgs(tt.args)

			require.NotNil(t, got, "ExpandEnvArgs should never return nil")
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExpandEnvArgs_DoesNotMutateInput verifies that the original args slice
// is not modified by ExpandEnvArgs.
func TestExpandEnvArgs_DoesNotMutateInput(t *testing.T) {
	t.Setenv("MY_SECRET", "secret-value")

	original := []string{"-e", "MY_SECRET", "--rm"}
	// Make a copy to compare against after the call
	copyOfOriginal := make([]string, len(original))
	copy(copyOfOriginal, original)

	ExpandEnvArgs(original)

	assert.Equal(t, copyOfOriginal, original, "ExpandEnvArgs must not mutate the input slice")
}

// TestExpandEnvArgs_OutputIsIndependentOfInput verifies that modifications to
// the returned slice do not affect the original input.
func TestExpandEnvArgs_OutputIsIndependentOfInput(t *testing.T) {
	t.Setenv("SOME_VAR", "value")

	args := []string{"run", "-e", "SOME_VAR"}
	result := ExpandEnvArgs(args)

	// Modifying the result should not affect the original
	result[0] = "MODIFIED"
	assert.Equal(t, "run", args[0], "Modifying result should not affect original slice")
}

func TestWalkDockerEnvArgs(t *testing.T) {
	t.Setenv("SET_VAR", "value")
	t.Setenv("EMPTY_VAR", "")

	type walkedArg struct {
		index   int
		varName string
		value   string
		found   bool
	}

	var walked []walkedArg
	WalkDockerEnvArgs([]string{
		"run",
		"-e", "SET_VAR",
		"-e", "EXPLICIT=value",
		"-e", "EMPTY_VAR",
		"-e", "MISSING_VAR",
		"-e",
	}, func(index int, varName, value string, found bool) {
		walked = append(walked, walkedArg{
			index:   index,
			varName: varName,
			value:   value,
			found:   found,
		})
	})

	assert.Equal(t, []walkedArg{
		{index: 1, varName: "SET_VAR", value: "value", found: true},
		{index: 5, varName: "EMPTY_VAR", value: "", found: true},
		{index: 7, varName: "MISSING_VAR", value: "", found: false},
	}, walked)
	assert.Len(t, walked, 3)
}

func TestWalkDockerEnvArgs_IgnoresExplicitAssignmentsAndTrailingFlag(t *testing.T) {
	calls := 0

	WalkDockerEnvArgs([]string{
		"-e", "EXPLICIT=value",
		"-e",
	}, func(index int, varName, value string, found bool) {
		calls++
	})

	assert.Zero(t, calls)
}
