package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringsToAny(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns empty (non-nil) slice", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []interface{}{}, StringsToAny(nil))
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, StringsToAny([]string{}))
	})

	t.Run("converts all entries preserving order", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []interface{}{"octo", "hub", "bot"}, StringsToAny([]string{"octo", "hub", "bot"}))
	})
}
