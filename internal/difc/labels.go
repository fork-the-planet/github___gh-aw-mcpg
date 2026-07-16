package difc

import (
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logLabels = logger.New("difc:labels")

// Tag represents a single DIFC tag (e.g., "repo:owner/name", "agent:demo-agent")
type Tag string

// WildcardTag is a special tag that matches all other tags in subset checks.
// When the "superset" side of a flow check contains WildcardTag, the check
// passes regardless of what tags the other side has. This is used by write-sink
// guards with accept=["*"] to allow writes from agents with any secrecy.
const WildcardTag = Tag("*")

// Label represents a set of DIFC tags
type Label struct {
	tagSet
}

// NewLabel creates a new empty label
func NewLabel() *Label {
	return &Label{tagSet: newTagSet()}
}

// newLabelWithTags is a helper function that creates a label with the given tags.
func newLabelWithTags(tags []Tag) *Label {
	logLabels.Printf("Creating label with %d initial tags: %v", len(tags), tags)
	label := NewLabel()
	label.AddAll(tags)
	return label
}

// Add adds a tag to this label
func (l *Label) Add(tag Tag) {
	l.add(tag)
}

// AddAll adds multiple tags to this label
func (l *Label) AddAll(tags []Tag) {
	l.addAll(tags)
}

// Remove removes a single tag from this label
func (l *Label) Remove(tag Tag) {
	l.remove(tag)
}

// RemoveAll removes multiple tags from this label
func (l *Label) RemoveAll(tags []Tag) {
	l.removeAll(tags)
}

// Contains checks if this label contains a specific tag
func (l *Label) Contains(tag Tag) bool {
	return l.contains(tag)
}

// Union merges another label into this label
func (l *Label) Union(other *Label) {
	if other == nil {
		return
	}
	other.mu.RLock()
	l.mu.Lock()
	added := 0
	for tag := range other.tags {
		if _, exists := l.tags[tag]; !exists {
			l.tags[tag] = struct{}{}
			added++
		}
	}
	l.mu.Unlock()
	other.mu.RUnlock()
	if added > 0 {
		logLabels.Printf("Label union: merged %d new tags from other label", added)
	}
}

// Intersect removes tags from this label that are not in the other label
// After this operation, this label contains only tags present in both labels
func (l *Label) Intersect(other *Label) {
	if other == nil {
		// Intersection with nil/empty is empty
		l.mu.Lock()
		before := len(l.tags)
		l.tags = make(map[Tag]struct{})
		l.mu.Unlock()
		logLabels.Printf("Intersect with nil: cleared %d tags", before)
		return
	}
	other.mu.RLock()
	l.mu.Lock()
	// Remove tags not in other
	removed := 0
	for tag := range l.tags {
		if _, ok := other.tags[tag]; !ok {
			delete(l.tags, tag)
			removed++
		}
	}
	remaining := len(l.tags)
	l.mu.Unlock()
	other.mu.RUnlock()
	if removed > 0 {
		logLabels.Printf("Intersect: removed %d tags, %d remaining", removed, remaining)
	}
}

// Clone creates a copy of this label
func (l *Label) Clone() *Label {
	l.mu.RLock()
	newLabel := NewLabel()
	for tag := range l.tags {
		newLabel.tags[tag] = struct{}{}
	}
	count := len(newLabel.tags)
	l.mu.RUnlock()
	logLabels.Printf("Cloned label: %d tags", count)
	return newLabel
}

// GetTags returns all tags in this label as a slice
func (l *Label) GetTags() []Tag {
	return l.getAll()
}

// IsEmpty returns true if this label has no tags
func (l *Label) IsEmpty() bool {
	return l.isEmpty()
}

// cloneLabelOrNew clones inner if it is non-nil, otherwise returns a new empty Label.
func cloneLabelOrNew(inner *Label) *Label {
	if inner == nil {
		return NewLabel()
	}
	return inner.Clone()
}

// StringsToTags converts a slice of strings to a slice of Tags,
// trimming whitespace and skipping empty values.
func StringsToTags(values []string) []Tag {
	logLabels.Printf("StringsToTags: converting %d string values to tags", len(values))
	tags := make([]Tag, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			tags = append(tags, Tag(trimmed))
		}
	}
	logLabels.Printf("StringsToTags: produced %d tags from %d input values", len(tags), len(values))
	return tags
}

// TagsToStrings converts a slice of Tags to a slice of strings.
func TagsToStrings(tags []Tag) []string {
	logLabels.Printf("TagsToStrings: converting %d tags to strings", len(tags))
	values := make([]string, 0, len(tags))
	for _, tag := range tags {
		values = append(values, string(tag))
	}
	return values
}
