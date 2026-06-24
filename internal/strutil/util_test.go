package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortedSetKeys(t *testing.T) {
	t.Parallel()

	t.Run("returns sorted keys", func(t *testing.T) {
		t.Parallel()
		set := map[string]struct{}{"banana": {}, "apple": {}, "cherry": {}}
		assert.Equal(t, []string{"apple", "banana", "cherry"}, SortedSetKeys(set))
	})

	t.Run("returns empty slice for empty set", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, SortedSetKeys(map[string]struct{}{}))
	})

	t.Run("returns single element slice", func(t *testing.T) {
		t.Parallel()
		set := map[string]struct{}{"only": {}}
		assert.Equal(t, []string{"only"}, SortedSetKeys(set))
	})

	t.Run("handles nil map", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, SortedSetKeys(nil))
	})
}

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

	t.Run("returns empty string when value is empty string", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": ""}
		assert.Equal(t, "", GetStringFromMap(m, "owner"))
	})

	t.Run("variadic: returns first non-empty match", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"htmlUrl": "https://example.com"}
		assert.Equal(t, "https://example.com", GetStringFromMap(m, "html_url", "htmlUrl"))
	})

	t.Run("variadic: first key takes priority when both present", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"html_url": "https://first.com", "htmlUrl": "https://second.com"}
		assert.Equal(t, "https://first.com", GetStringFromMap(m, "html_url", "htmlUrl"))
	})

	t.Run("variadic: skips empty first key and returns second", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"html_url": "", "htmlUrl": "https://second.com"}
		assert.Equal(t, "https://second.com", GetStringFromMap(m, "html_url", "htmlUrl"))
	})

	t.Run("variadic: all keys missing returns empty string", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"other": "value"}
		assert.Equal(t, "", GetStringFromMap(m, "html_url", "htmlUrl"))
	})

	t.Run("variadic: skips non-string first key and returns second", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"html_url": 99, "htmlUrl": "https://second.com"}
		assert.Equal(t, "https://second.com", GetStringFromMap(m, "html_url", "htmlUrl"))
	})

	t.Run("no keys returns empty string", func(t *testing.T) {
		t.Parallel()
		m := map[string]interface{}{"owner": "octo"}
		assert.Equal(t, "", GetStringFromMap(m))
	})
}
