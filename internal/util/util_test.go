package util

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDeepCloneJSON(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns nil", func(t *testing.T) {
		t.Parallel()
		result := DeepCloneJSON(nil)
		assert.Nil(t, result)
	})

	t.Run("string returns same value", func(t *testing.T) {
		t.Parallel()
		result := DeepCloneJSON("hello")
		assert.Equal(t, "hello", result)
	})

	t.Run("float64 returns same value", func(t *testing.T) {
		t.Parallel()
		result := DeepCloneJSON(float64(3.14))
		assert.Equal(t, float64(3.14), result)
	})

	t.Run("bool true returns same value", func(t *testing.T) {
		t.Parallel()
		result := DeepCloneJSON(true)
		assert.Equal(t, true, result)
	})

	t.Run("bool false returns same value", func(t *testing.T) {
		t.Parallel()
		result := DeepCloneJSON(false)
		assert.Equal(t, false, result)
	})

	t.Run("empty map returns empty map", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be map[string]interface{}")
		assert.Empty(t, cloned)
	})

	t.Run("flat map with primitive values", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"name":   "alice",
			"age":    float64(30),
			"active": true,
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be map[string]interface{}")
		assert.Equal(t, input, cloned)
	})

	t.Run("flat map clone is independent from original", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"key": "original",
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)

		cloned["key"] = "modified"
		assert.Equal(t, "original", input["key"], "original map should not be affected by clone modification")
	})

	t.Run("nested map deep clones nested maps", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"outer": map[string]interface{}{
				"inner": "value",
			},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, input, cloned)
	})

	t.Run("nested map clone is independent from original", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"outer": map[string]interface{}{
				"inner": "original",
			},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)

		innerClone, ok := cloned["outer"].(map[string]interface{})
		require.True(t, ok)
		innerClone["inner"] = "modified"

		innerOrig := input["outer"].(map[string]interface{})
		assert.Equal(t, "original", innerOrig["inner"], "original nested map should not be affected")
	})

	t.Run("empty slice returns empty slice", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok, "result should be []interface{}")
		assert.Empty(t, cloned)
	})

	t.Run("flat slice with primitive values", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{"a", float64(1), true, nil}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok, "result should be []interface{}")
		assert.Equal(t, input, cloned)
	})

	t.Run("flat slice clone is independent from original", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{"original", float64(42)}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok)

		cloned[0] = "modified"
		assert.Equal(t, "original", input[0], "original slice should not be affected by clone modification")
	})

	t.Run("nested slice deep clones nested slices", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{
			[]interface{}{"a", "b"},
			[]interface{}{float64(1), float64(2)},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok)
		assert.Equal(t, input, cloned)
	})

	t.Run("nested slice clone is independent from original", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{
			[]interface{}{"original"},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok)

		innerClone, ok := cloned[0].([]interface{})
		require.True(t, ok)
		innerClone[0] = "modified"

		innerOrig := input[0].([]interface{})
		assert.Equal(t, "original", innerOrig[0], "original nested slice should not be affected")
	})

	t.Run("map containing slices", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"items": []interface{}{"x", "y", "z"},
			"count": float64(3),
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, input, cloned)

		// Verify independence of nested slice
		clonedItems, ok := cloned["items"].([]interface{})
		require.True(t, ok)
		clonedItems[0] = "modified"

		origItems := input["items"].([]interface{})
		assert.Equal(t, "x", origItems[0], "original slice inside map should not be affected")
	})

	t.Run("slice containing maps", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{
			map[string]interface{}{"name": "alice", "score": float64(95)},
			map[string]interface{}{"name": "bob", "score": float64(87)},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok)
		assert.Equal(t, input, cloned)

		// Verify independence of nested map
		clonedMap, ok := cloned[0].(map[string]interface{})
		require.True(t, ok)
		clonedMap["name"] = "charlie"

		origMap := input[0].(map[string]interface{})
		assert.Equal(t, "alice", origMap["name"], "original map inside slice should not be affected")
	})

	t.Run("deeply nested structure", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"level1": map[string]interface{}{
				"level2": map[string]interface{}{
					"level3": []interface{}{
						map[string]interface{}{
							"leaf": "value",
						},
					},
				},
			},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, input, cloned)

		// Verify deep independence
		l1 := cloned["level1"].(map[string]interface{})
		l2 := l1["level2"].(map[string]interface{})
		l3 := l2["level3"].([]interface{})
		leaf := l3[0].(map[string]interface{})
		leaf["leaf"] = "modified"

		origL1 := input["level1"].(map[string]interface{})
		origL2 := origL1["level2"].(map[string]interface{})
		origL3 := origL2["level3"].([]interface{})
		origLeaf := origL3[0].(map[string]interface{})
		assert.Equal(t, "value", origLeaf["leaf"], "deeply nested original should not be affected")
	})

	t.Run("map with null value", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"key": nil,
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Nil(t, cloned["key"])
	})

	t.Run("slice with null element", func(t *testing.T) {
		t.Parallel()
		input := []interface{}{nil, "value", nil}
		result := DeepCloneJSON(input)
		cloned, ok := result.([]interface{})
		require.True(t, ok)
		assert.Nil(t, cloned[0])
		assert.Equal(t, "value", cloned[1])
		assert.Nil(t, cloned[2])
	})

	t.Run("map preserves all key-value pairs", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{
			"a": "alpha",
			"b": float64(2),
			"c": true,
			"d": nil,
			"e": []interface{}{"x"},
			"f": map[string]interface{}{"nested": "yes"},
		}
		result := DeepCloneJSON(input)
		cloned, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Len(t, cloned, len(input))
		assert.Equal(t, input, cloned)
	})
}

func TestInterfaceToIntString(t *testing.T) {
	t.Parallel()

	t.Run("float64 integer", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(float64(42))
		assert.True(t, ok)
		assert.Equal(t, "42", s)
	})

	t.Run("float64 zero", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(float64(0))
		assert.True(t, ok)
		assert.Equal(t, "0", s)
	})

	t.Run("float64 negative integer", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(float64(-7))
		assert.True(t, ok)
		assert.Equal(t, "-7", s)
	})

	t.Run("float64 non-integer returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(float64(1.5))
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("float64 truncatable decimal returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(float64(123.9))
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("float64 out of int64 range returns false", func(t *testing.T) {
		t.Parallel()
		// 1e20 exceeds int64 max; explicit out-of-range guard rejects it
		s, ok := InterfaceToIntString(float64(1e20))
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("json.Number integer", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(json.Number("999"))
		assert.True(t, ok)
		assert.Equal(t, "999", s)
	})

	t.Run("json.Number leading zeros canonicalized", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(json.Number("00123"))
		assert.True(t, ok)
		assert.Equal(t, "123", s)
	})

	t.Run("json.Number large value within int64", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(json.Number("9223372036854775807"))
		assert.True(t, ok)
		assert.Equal(t, "9223372036854775807", s)
	})

	t.Run("json.Number decimal returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(json.Number("123.45"))
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("json.Number out of int64 range returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(json.Number("99999999999999999999"))
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("string returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString("42")
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("int returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(42)
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})

	t.Run("nil returns false", func(t *testing.T) {
		t.Parallel()
		s, ok := InterfaceToIntString(nil)
		assert.False(t, ok)
		assert.Equal(t, "", s)
	})
}
