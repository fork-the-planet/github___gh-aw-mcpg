package dockerutil

import (
	"os"
	"testing"
)

func TestExpandEnvArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		envVars  map[string]string
		expected []string
	}{
		{
			name:     "no -e flags",
			args:     []string{"run", "--rm", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "--rm", "image"},
		},
		{
			name:     "expand single env variable",
			args:     []string{"run", "-e", "VAR_NAME", "image"},
			envVars:  map[string]string{"VAR_NAME": "value1"},
			expected: []string{"run", "-e", "VAR_NAME=value1", "image"},
		},
		{
			name:     "expand multiple env variables",
			args:     []string{"run", "-e", "VAR1", "-e", "VAR2", "image"},
			envVars:  map[string]string{"VAR1": "value1", "VAR2": "value2"},
			expected: []string{"run", "-e", "VAR1=value1", "-e", "VAR2=value2", "image"},
		},
		{
			name:     "preserve existing key=value format",
			args:     []string{"run", "-e", "VAR=predefined", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "-e", "VAR=predefined", "image"},
		},
		{
			name:     "mixed: expand and preserve",
			args:     []string{"run", "-e", "VAR1", "-e", "VAR2=fixed", "image"},
			envVars:  map[string]string{"VAR1": "value1"},
			expected: []string{"run", "-e", "VAR1=value1", "-e", "VAR2=fixed", "image"},
		},
		{
			name:     "undefined env variable",
			args:     []string{"run", "-e", "UNDEFINED_VAR", "image"},
			envVars:  map[string]string{},
			expected: []string{"run", "-e", "UNDEFINED_VAR", "image"},
		},
		{
			name:     "empty env variable value",
			args:     []string{"run", "-e", "EMPTY_VAR", "image"},
			envVars:  map[string]string{"EMPTY_VAR": ""},
			expected: []string{"run", "-e", "EMPTY_VAR=", "image"},
		},
		{
			name:     "-e at end of args (no following arg)",
			args:     []string{"run", "image", "-e"},
			envVars:  map[string]string{},
			expected: []string{"run", "image", "-e"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables for test
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			// Clean up after test
			t.Cleanup(func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			})

			result := ExpandEnvArgs(tt.args)

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d args, got %d: %v", len(tt.expected), len(result), result)
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("Arg %d: expected '%s', got '%s'", i, tt.expected[i], result[i])
				}
			}
		})
	}
}
