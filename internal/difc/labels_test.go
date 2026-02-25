package difc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLabel_Union tests the Union method for merging tags from another label.
func TestLabel_Union(t *testing.T) {
	tests := []struct {
		name      string
		setup     []Tag
		other     []Tag // nil means nil *Label is passed
		nilOther  bool
		wantTags  []Tag
		notInTags []Tag
	}{
		{
			name:     "merges tags from other label",
			setup:    []Tag{"a", "b"},
			other:    []Tag{"c", "d"},
			wantTags: []Tag{"a", "b", "c", "d"},
		},
		{
			name:     "nil other label is a no-op",
			setup:    []Tag{"a", "b"},
			nilOther: true,
			wantTags: []Tag{"a", "b"},
		},
		{
			name:     "handles overlapping tags without duplicates",
			setup:    []Tag{"shared", "local"},
			other:    []Tag{"shared", "remote"},
			wantTags: []Tag{"shared", "local", "remote"},
		},
		{
			name:     "merge into empty label adds all tags",
			setup:    nil,
			other:    []Tag{"x", "y"},
			wantTags: []Tag{"x", "y"},
		},
		{
			name:     "merge empty other label is a no-op",
			setup:    []Tag{"existing"},
			other:    nil,
			wantTags: []Tag{"existing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLabel()
			for _, tag := range tt.setup {
				l.Add(tag)
			}

			if tt.nilOther {
				l.Union(nil)
			} else {
				other := NewLabel()
				for _, tag := range tt.other {
					other.Add(tag)
				}
				l.Union(other)
			}

			for _, tag := range tt.wantTags {
				assert.True(t, l.Contains(tag), "expected label to contain %q", tag)
			}
			for _, tag := range tt.notInTags {
				assert.False(t, l.Contains(tag), "expected label NOT to contain %q", tag)
			}
		})
	}
}

// TestLabel_Clone tests that Clone produces an independent deep copy.
func TestLabel_Clone(t *testing.T) {
	t.Run("clone is independent from original", func(t *testing.T) {
		orig := NewLabel()
		orig.Add("tag1")
		orig.Add("tag2")

		cloned := orig.Clone()
		require.NotNil(t, cloned)

		// Cloned label should have same tags
		assert.True(t, cloned.Contains("tag1"))
		assert.True(t, cloned.Contains("tag2"))

		// Modifying the original does not affect the clone
		orig.Add("tag3")
		assert.False(t, cloned.Contains("tag3"), "clone should not reflect changes to original")
	})

	t.Run("clone of empty label is empty", func(t *testing.T) {
		orig := NewLabel()
		cloned := orig.Clone()
		assert.True(t, cloned.IsEmpty())
	})
}

// TestLabel_GetTags tests that GetTags returns all tags.
func TestLabel_GetTags(t *testing.T) {
	t.Run("returns all added tags", func(t *testing.T) {
		l := NewLabel()
		l.Add("alpha")
		l.Add("beta")
		l.Add("gamma")

		tags := l.GetTags()
		assert.Len(t, tags, 3)
		assert.ElementsMatch(t, []Tag{"alpha", "beta", "gamma"}, tags)
	})

	t.Run("returns empty slice for empty label", func(t *testing.T) {
		l := NewLabel()
		tags := l.GetTags()
		assert.Empty(t, tags)
	})
}

// TestSecrecyLabel_CheckFlow tests CheckFlow for SecrecyLabel.
func TestSecrecyLabel_CheckFlow(t *testing.T) {
	tests := []struct {
		name        string
		src         []Tag // source label tags
		target      []Tag // target label tags
		nilSrc      bool
		nilTarget   bool
		wantOK      bool
		wantViolate []Tag // tags expected in violation list (subset check)
	}{
		{
			name:   "nil source can flow to anything",
			nilSrc: true,
			target: []Tag{"any"},
			wantOK: true,
		},
		{
			name:      "nil target: empty source can flow",
			src:       nil,
			nilTarget: true,
			wantOK:    true,
		},
		{
			name:        "nil target: non-empty source cannot flow",
			src:         []Tag{"secret"},
			nilTarget:   true,
			wantOK:      false,
			wantViolate: []Tag{"secret"},
		},
		{
			name:   "source subset of target: allowed",
			src:    []Tag{"tag1"},
			target: []Tag{"tag1", "tag2"},
			wantOK: true,
		},
		{
			name:        "source has extra tags not in target: denied",
			src:         []Tag{"tag1", "extra"},
			target:      []Tag{"tag1"},
			wantOK:      false,
			wantViolate: []Tag{"extra"},
		},
		{
			name:   "empty source can flow to empty target",
			src:    nil,
			target: nil,
			wantOK: true,
		},
		{
			name:        "source has multiple extra tags",
			src:         []Tag{"a", "b", "c"},
			target:      []Tag{"a"},
			wantOK:      false,
			wantViolate: []Tag{"b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src *SecrecyLabel
			if !tt.nilSrc {
				src = NewSecrecyLabelWithTags(tt.src)
			}

			var target *SecrecyLabel
			if !tt.nilTarget {
				target = NewSecrecyLabelWithTags(tt.target)
			}

			ok, violatingTags := src.CheckFlow(target)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Empty(t, violatingTags)
			} else {
				assert.NotEmpty(t, violatingTags)
				for _, expectedTag := range tt.wantViolate {
					assert.Contains(t, violatingTags, expectedTag)
				}
			}
		})
	}
}

// TestIntegrityLabel_CheckFlow tests CheckFlow for IntegrityLabel.
func TestIntegrityLabel_CheckFlow(t *testing.T) {
	tests := []struct {
		name        string
		src         []Tag
		target      []Tag
		nilSrc      bool
		nilTarget   bool
		wantOK      bool
		wantViolate []Tag
	}{
		{
			name:      "nil source, nil target: allowed",
			nilSrc:    true,
			nilTarget: true,
			wantOK:    true,
		},
		{
			name:   "nil source, empty target: allowed",
			nilSrc: true,
			target: nil,
			wantOK: true,
		},
		{
			name:      "any source, nil target: allowed",
			src:       []Tag{"trust"},
			nilTarget: true,
			wantOK:    true,
		},
		{
			name:   "source superset of target: allowed",
			src:    []Tag{"t1", "t2"},
			target: []Tag{"t1"},
			wantOK: true,
		},
		{
			name:        "source missing tag from target: denied",
			src:         []Tag{"t1"},
			target:      []Tag{"t1", "t2"},
			wantOK:      false,
			wantViolate: []Tag{"t2"},
		},
		{
			name:        "empty source, non-empty target: denied",
			src:         nil,
			target:      []Tag{"required"},
			wantOK:      false,
			wantViolate: []Tag{"required"},
		},
		{
			name:        "source missing multiple tags",
			src:         []Tag{"a"},
			target:      []Tag{"a", "b", "c"},
			wantOK:      false,
			wantViolate: []Tag{"b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src *IntegrityLabel
			if !tt.nilSrc {
				src = NewIntegrityLabelWithTags(tt.src)
			}

			var target *IntegrityLabel
			if !tt.nilTarget {
				target = NewIntegrityLabelWithTags(tt.target)
			}

			ok, violatingTags := src.CheckFlow(target)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Empty(t, violatingTags)
			} else {
				assert.NotEmpty(t, violatingTags)
				for _, expectedTag := range tt.wantViolate {
					assert.Contains(t, violatingTags, expectedTag)
				}
			}
		})
	}
}

// TestSecrecyLabel_Clone tests that Clone produces an independent copy.
func TestSecrecyLabel_Clone(t *testing.T) {
	t.Run("nil secrecy label clones to empty label", func(t *testing.T) {
		var l *SecrecyLabel
		cloned := l.Clone()
		require.NotNil(t, cloned)
		assert.True(t, cloned.Label.IsEmpty())
	})

	t.Run("secrecy label with nil inner Label clones to empty", func(t *testing.T) {
		l := &SecrecyLabel{Label: nil}
		cloned := l.Clone()
		require.NotNil(t, cloned)
		assert.True(t, cloned.Label.IsEmpty())
	})

	t.Run("clone is independent from original", func(t *testing.T) {
		orig := NewSecrecyLabelWithTags([]Tag{"confidential", "private"})
		cloned := orig.Clone()
		require.NotNil(t, cloned)

		assert.True(t, cloned.Label.Contains("confidential"))
		assert.True(t, cloned.Label.Contains("private"))

		// Modify original; clone should be unaffected
		orig.Label.Add("new-tag")
		assert.False(t, cloned.Label.Contains("new-tag"), "clone should not reflect changes to original")
	})
}

// TestIntegrityLabel_Clone tests that Clone produces an independent copy.
func TestIntegrityLabel_Clone(t *testing.T) {
	t.Run("nil integrity label clones to empty label", func(t *testing.T) {
		var l *IntegrityLabel
		cloned := l.Clone()
		require.NotNil(t, cloned)
		assert.True(t, cloned.Label.IsEmpty())
	})

	t.Run("integrity label with nil inner Label clones to empty", func(t *testing.T) {
		l := &IntegrityLabel{Label: nil}
		cloned := l.Clone()
		require.NotNil(t, cloned)
		assert.True(t, cloned.Label.IsEmpty())
	})

	t.Run("clone is independent from original", func(t *testing.T) {
		orig := NewIntegrityLabelWithTags([]Tag{"trusted", "verified"})
		cloned := orig.Clone()
		require.NotNil(t, cloned)

		assert.True(t, cloned.Label.Contains("trusted"))
		assert.True(t, cloned.Label.Contains("verified"))

		// Modify original; clone should be unaffected
		orig.Label.Add("extra-trust")
		assert.False(t, cloned.Label.Contains("extra-trust"), "clone should not reflect changes to original")
	})
}

// TestNewSecrecyLabelWithTags tests that NewSecrecyLabelWithTags initializes correctly.
func TestNewSecrecyLabelWithTags(t *testing.T) {
	t.Run("creates label with all provided tags", func(t *testing.T) {
		tags := []Tag{"t1", "t2", "t3"}
		l := NewSecrecyLabelWithTags(tags)
		require.NotNil(t, l)
		for _, tag := range tags {
			assert.True(t, l.Label.Contains(tag))
		}
		assert.False(t, l.Label.IsEmpty())
	})

	t.Run("creates empty label from nil tags", func(t *testing.T) {
		l := NewSecrecyLabelWithTags(nil)
		require.NotNil(t, l)
		assert.True(t, l.Label.IsEmpty())
	})
}

// TestNewIntegrityLabelWithTags tests that NewIntegrityLabelWithTags initializes correctly.
func TestNewIntegrityLabelWithTags(t *testing.T) {
	t.Run("creates label with all provided tags", func(t *testing.T) {
		tags := []Tag{"trust1", "trust2"}
		l := NewIntegrityLabelWithTags(tags)
		require.NotNil(t, l)
		for _, tag := range tags {
			assert.True(t, l.Label.Contains(tag))
		}
	})

	t.Run("creates empty label from nil tags", func(t *testing.T) {
		l := NewIntegrityLabelWithTags(nil)
		require.NotNil(t, l)
		assert.True(t, l.Label.IsEmpty())
	})
}

// TestViolationError_Error tests the Error() method with all branching paths.
func TestViolationError_Error(t *testing.T) {
	tests := []struct {
		name         string
		err          ViolationError
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "secrecy violation with extra tags",
			err: ViolationError{
				Type:      SecrecyViolation,
				Resource:  "classified-doc",
				ExtraTags: []Tag{"secret", "top-secret"},
			},
			wantContains: []string{
				"Secrecy violation",
				"classified-doc",
				"[secret top-secret]",
				"cannot flow to resource",
				"Remediation:",
			},
		},
		{
			name: "secrecy violation with no extra tags",
			err: ViolationError{
				Type:      SecrecyViolation,
				Resource:  "public-endpoint",
				ExtraTags: nil,
			},
			wantContains: []string{
				"Secrecy violation",
				"public-endpoint",
			},
			// No tag list or remediation when ExtraTags is empty
			wantAbsent: []string{"cannot flow to resource"},
		},
		{
			name: "integrity write violation with missing tags",
			err: ViolationError{
				Type:        IntegrityViolation,
				Resource:    "prod-db",
				IsWrite:     true,
				MissingTags: []Tag{"production", "verified"},
			},
			wantContains: []string{
				"Integrity violation for write",
				"prod-db",
				"missing required integrity tags",
				"Remediation:",
				"production",
				"verified",
			},
		},
		{
			name: "integrity write violation with no missing tags",
			err: ViolationError{
				Type:        IntegrityViolation,
				Resource:    "prod-db",
				IsWrite:     true,
				MissingTags: nil,
			},
			wantContains: []string{
				"Integrity violation for write",
				"prod-db",
			},
			wantAbsent: []string{"missing required integrity tags"},
		},
		{
			name: "integrity read violation with missing tags",
			err: ViolationError{
				Type:        IntegrityViolation,
				Resource:    "trusted-source",
				IsWrite:     false,
				MissingTags: []Tag{"certified"},
			},
			wantContains: []string{
				"Integrity violation for read",
				"trusted-source",
				"missing integrity tags",
				"Remediation:",
				"certified",
			},
		},
		{
			name: "integrity read violation with no missing tags",
			err: ViolationError{
				Type:        IntegrityViolation,
				Resource:    "trusted-source",
				IsWrite:     false,
				MissingTags: nil,
			},
			wantContains: []string{
				"Integrity violation for read",
				"trusted-source",
			},
			wantAbsent: []string{"missing integrity tags"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			assert.NotEmpty(t, msg)
			for _, want := range tt.wantContains {
				assert.True(t, strings.Contains(msg, want),
					"expected %q in error message %q", want, msg)
			}
			for _, absent := range tt.wantAbsent {
				assert.False(t, strings.Contains(msg, absent),
					"expected %q NOT to be in error message %q", absent, msg)
			}
		})
	}
}

// TestViolationError_Detailed tests that Detailed() extends Error() with tag context.
func TestViolationError_Detailed(t *testing.T) {
	t.Run("includes agent and resource tag context", func(t *testing.T) {
		err := ViolationError{
			Type:         SecrecyViolation,
			Resource:     "sensitive-file",
			ExtraTags:    []Tag{"private"},
			AgentTags:    []Tag{"private", "public"},
			ResourceTags: []Tag{"public"},
		}

		detailed := err.Detailed()
		base := err.Error()

		// Detailed should contain the base error message
		assert.True(t, strings.Contains(detailed, base), "detailed should include base error")

		// Detailed should include agent and resource tag context
		assert.Contains(t, detailed, "Agent")
		assert.Contains(t, detailed, "Resource")
		assert.Contains(t, detailed, "private")
		assert.Contains(t, detailed, "public")
	})

	t.Run("integrity violation detailed message", func(t *testing.T) {
		err := ViolationError{
			Type:         IntegrityViolation,
			Resource:     "write-target",
			IsWrite:      true,
			MissingTags:  []Tag{"trusted"},
			AgentTags:    []Tag{},
			ResourceTags: []Tag{"trusted"},
		}

		detailed := err.Detailed()
		assert.Contains(t, detailed, "write-target")
		assert.Contains(t, detailed, "trusted")
		// Must include newlines separating context sections
		assert.Contains(t, detailed, "\n")
	})

	t.Run("detailed contains more than Error", func(t *testing.T) {
		err := ViolationError{
			Type:         SecrecyViolation,
			Resource:     "r",
			ExtraTags:    []Tag{"x"},
			AgentTags:    []Tag{"x", "y"},
			ResourceTags: []Tag{"z"},
		}
		assert.Greater(t, len(err.Detailed()), len(err.Error()),
			"Detailed() should produce a longer message than Error()")
	})
}

// TestViolationError_implementsError verifies ViolationError satisfies the error interface.
func TestViolationError_implementsError(t *testing.T) {
	var _ error = (*ViolationError)(nil)
}

// TestSecrecyLabel_CanFlowTo_NilCases tests nil-receiver and nil-argument edge cases.
func TestSecrecyLabel_CanFlowTo_NilCases(t *testing.T) {
	t.Run("nil receiver can flow to anything", func(t *testing.T) {
		var l *SecrecyLabel
		target := NewSecrecyLabelWithTags([]Tag{"any"})
		assert.True(t, l.CanFlowTo(target))
	})

	t.Run("nil receiver can flow to nil target", func(t *testing.T) {
		var l *SecrecyLabel
		assert.True(t, l.CanFlowTo(nil))
	})

	t.Run("non-empty source cannot flow to nil target", func(t *testing.T) {
		l := NewSecrecyLabelWithTags([]Tag{"restricted"})
		assert.False(t, l.CanFlowTo(nil))
	})

	t.Run("empty source can flow to nil target", func(t *testing.T) {
		l := NewSecrecyLabel()
		assert.True(t, l.CanFlowTo(nil))
	})
}

// TestIntegrityLabel_CanFlowTo_NilCases tests nil-receiver and nil-argument edge cases.
func TestIntegrityLabel_CanFlowTo_NilCases(t *testing.T) {
	t.Run("nil receiver with empty target: allowed", func(t *testing.T) {
		var l *IntegrityLabel
		target := NewIntegrityLabel()
		assert.True(t, l.CanFlowTo(target))
	})

	t.Run("nil receiver with non-empty target: denied", func(t *testing.T) {
		var l *IntegrityLabel
		target := NewIntegrityLabelWithTags([]Tag{"required"})
		assert.False(t, l.CanFlowTo(target))
	})

	t.Run("non-empty source can flow to nil target", func(t *testing.T) {
		l := NewIntegrityLabelWithTags([]Tag{"trusted"})
		assert.True(t, l.CanFlowTo(nil))
	})
}
