package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandMapInPlace tests the expandMapInPlace helper which expands
// ${VAR} expressions in a map[string]string in-place via pointer.
//
// expandMapInPlace has three distinct code paths:
//  1. Empty/nil map fast path → returns nil without calling expandEnvVariables.
//  2. Expansion error path → wraps the error with server name and field description.
//  3. Happy path → replaces *m with the expanded copy.
func TestExpandMapInPlace(t *testing.T) {
	t.Run("nil map pointer dereferences to empty - returns nil", func(t *testing.T) {
		var m map[string]string // nil map; len(nil) == 0
		err := expandMapInPlace(&m, "my-server", "environment variable(s)")
		require.NoError(t, err)
		// m must remain nil (not mutated)
		assert.Nil(t, m)
	})

	t.Run("empty map returns nil without mutation", func(t *testing.T) {
		m := map[string]string{}
		err := expandMapInPlace(&m, "my-server", "environment variable(s)")
		require.NoError(t, err)
		assert.Empty(t, m)
	})

	t.Run("all variables defined - map mutated in place", func(t *testing.T) {
		t.Setenv("MY_TOKEN", "tok_abc123")
		t.Setenv("MY_HOST", "api.example.com")

		m := map[string]string{
			"TOKEN": "${MY_TOKEN}",
			"HOST":  "${MY_HOST}",
		}
		err := expandMapInPlace(&m, "github", "environment variable(s)")
		require.NoError(t, err)
		assert.Equal(t, "tok_abc123", m["TOKEN"])
		assert.Equal(t, "api.example.com", m["HOST"])
	})

	t.Run("literal values pass through unchanged", func(t *testing.T) {
		m := map[string]string{
			"STATIC_KEY": "static-value",
			"ANOTHER":    "no-vars-here",
		}
		err := expandMapInPlace(&m, "server", "HTTP header(s)")
		require.NoError(t, err)
		assert.Equal(t, "static-value", m["STATIC_KEY"])
		assert.Equal(t, "no-vars-here", m["ANOTHER"])
	})

	t.Run("undefined variable returns wrapped error mentioning server name", func(t *testing.T) {
		m := map[string]string{
			"TOKEN": "${TOTALLY_UNDEFINED_TOKEN_XYZ_123}",
		}
		err := expandMapInPlace(&m, "my-server", "environment variable(s)")
		require.Error(t, err)
		errStr := err.Error()
		assert.Contains(t, errStr, "my-server")
		assert.Contains(t, errStr, "environment variable(s)")
		assert.Contains(t, errStr, "failed to expand")
	})

	t.Run("error includes fieldDesc from HTTP headers call site", func(t *testing.T) {
		m := map[string]string{
			"Authorization": "${MISSING_HEADER_VAR}",
		}
		err := expandMapInPlace(&m, "http-server", "HTTP header(s)")
		require.Error(t, err)
		errStr := err.Error()
		assert.Contains(t, errStr, "http-server")
		assert.Contains(t, errStr, "HTTP header(s)")
	})

	t.Run("pointer is updated - caller sees expanded map", func(t *testing.T) {
		t.Setenv("CALLER_VAR", "caller-value")

		original := map[string]string{"K": "${CALLER_VAR}"}
		// expandMapInPlace writes the expanded entries back through the pointer;
		// verify the caller-visible variable holds the expanded value.
		ptr := &original
		err := expandMapInPlace(ptr, "srv", "env vars")
		require.NoError(t, err)
		assert.Equal(t, "caller-value", original["K"],
			"caller's variable should see the expanded value after in-place update")
	})

	t.Run("mixed defined and undefined - error on undefined", func(t *testing.T) {
		t.Setenv("DEFINED_MAP_VAR", "yes")

		m := map[string]string{
			"GOOD":    "${DEFINED_MAP_VAR}",
			"MISSING": "${TOTALLY_ABSENT_VAR_XYZ}",
		}
		err := expandMapInPlace(&m, "mixed-server", "environment variable(s)")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mixed-server")
	})

	t.Run("variable expanded to empty string is valid", func(t *testing.T) {
		t.Setenv("EMPTY_MAP_VAR", "")

		m := map[string]string{
			"KEY": "${EMPTY_MAP_VAR}",
		}
		err := expandMapInPlace(&m, "srv", "environment variable(s)")
		require.NoError(t, err)
		assert.Equal(t, "", m["KEY"])
	})

	t.Run("error wraps underlying undefined-variable error", func(t *testing.T) {
		m := map[string]string{"X": "${WRAP_TEST_UNDEFINED_VAR}"}
		err := expandMapInPlace(&m, "wrap-server", "environment variable(s)")
		require.Error(t, err)
		// The wrapped error should contain the undefined variable name.
		assert.Contains(t, err.Error(), "WRAP_TEST_UNDEFINED_VAR")
	})

	t.Run("server name with special characters is quoted in error", func(t *testing.T) {
		m := map[string]string{"TOKEN": "${MISSING_QUOTED_VAR}"}
		err := expandMapInPlace(&m, "my.special-server", "environment variable(s)")
		require.Error(t, err)
		// The fmt.Errorf uses %q for serverName so the name appears quoted.
		assert.Contains(t, err.Error(), `"my.special-server"`)
	})
}
