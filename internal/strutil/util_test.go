package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetStringFromMap(t *testing.T) {
	t.Parallel()

	t.Run("returns string value when present", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": "octo"}
		assert.Equal(t, "octo", GetStringFromMap(m, "owner"))
	})

	t.Run("returns empty string for missing key", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": "octo"}
		assert.Equal(t, "", GetStringFromMap(m, "repo"))
	})

	t.Run("returns empty string for non-string value", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"number": float64(1)}
		assert.Equal(t, "", GetStringFromMap(m, "number"))
	})

	t.Run("returns empty string for nil map", func(t *testing.T) {
		t.Parallel()
		var m map[string]interface{}
		assert.Equal(t, "", GetStringFromMap(m, "owner"))
	})
}
