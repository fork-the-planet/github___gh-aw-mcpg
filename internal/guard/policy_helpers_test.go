package guard

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyToMap(t *testing.T) {
	t.Run("returns deep copy for map input", func(t *testing.T) {
		policy := map[string]interface{}{
			"allow-only": map[string]interface{}{
				"repos":         "public",
				"min-integrity": "none",
			},
		}

		payload, err := PolicyToMap(policy)
		require.NoError(t, err)
		require.NotNil(t, payload)

		allowOnly, ok := payload["allow-only"].(map[string]interface{})
		require.True(t, ok)
		allowOnly["repos"] = "all"

		original, ok := policy["allow-only"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "public", original["repos"])
	})

	t.Run("nil policy returns error", func(t *testing.T) {
		_, err := PolicyToMap(nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "policy is required")
	})

	t.Run("non-object policy returns error", func(t *testing.T) {
		_, err := PolicyToMap([]string{"not-an-object"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "policy must decode to a JSON object")
	})

	t.Run("unmarshalable policy returns error", func(t *testing.T) {
		_, err := PolicyToMap(math.NaN())
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to serialize policy")
	})
}
