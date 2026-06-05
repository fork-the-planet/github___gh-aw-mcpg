package strutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCopyTrimmedStringIntMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input map[string]int
		want  map[string]int
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty map returns nil",
			input: map[string]int{},
			want:  nil,
		},
		{
			name:  "single entry copied",
			input: map[string]int{"get_file": 10},
			want:  map[string]int{"get_file": 10},
		},
		{
			name:  "multiple entries copied",
			input: map[string]int{"get_file": 10, "create_issue": 5, "search_code": 100},
			want:  map[string]int{"get_file": 10, "create_issue": 5, "search_code": 100},
		},
		{
			name:  "leading and trailing spaces trimmed from keys",
			input: map[string]int{"  get_file  ": 10},
			want:  map[string]int{"get_file": 10},
		},
		{
			name:  "tab characters trimmed from keys",
			input: map[string]int{"\tsearch_code\t": 5},
			want:  map[string]int{"search_code": 5},
		},
		{
			name:  "zero limit values are preserved",
			input: map[string]int{"get_file": 0},
			want:  map[string]int{"get_file": 0},
		},
		{
			name:  "negative limit values are preserved",
			input: map[string]int{"get_file": -1},
			want:  map[string]int{"get_file": -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CopyTrimmedStringIntMap(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCopyTrimmedStringIntMap_DefensiveCopy(t *testing.T) {
	t.Parallel()

	original := map[string]int{"get_file": 10, "create_issue": 5}
	copied := CopyTrimmedStringIntMap(original)
	require.NotNil(t, copied)

	copied["get_file"] = 999
	copied["new_tool"] = 1

	assert.Equal(t, 10, original["get_file"], "mutation of copy must not affect original")
	assert.NotContains(t, original, "new_tool", "new keys in copy must not appear in original")
}

func TestCopyTrimmedStringIntMap_OriginalMutationDoesNotAffectCopy(t *testing.T) {
	t.Parallel()

	original := map[string]int{"get_file": 10}
	result := CopyTrimmedStringIntMap(original)
	require.NotNil(t, result)

	original["get_file"] = 999
	original["new_tool"] = 1

	assert.Equal(t, 10, result["get_file"], "mutation of original must not affect copy")
	assert.NotContains(t, result, "new_tool", "new keys in original must not appear in copy")
}
