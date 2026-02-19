package difc

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCapabilities(t *testing.T) {
	caps := NewCapabilities()
	require.NotNil(t, caps)
	assert.Equal(t, 0, caps.Count(), "New capabilities should be empty")
}

func TestCapabilities_Add(t *testing.T) {
	tests := []struct {
		name      string
		tags      []Tag
		wantCount int
	}{
		{
			name:      "add single tag",
			tags:      []Tag{"repo:owner/name"},
			wantCount: 1,
		},
		{
			name:      "add duplicate tag",
			tags:      []Tag{"repo:owner/name", "repo:owner/name"},
			wantCount: 1,
		},
		{
			name:      "add multiple distinct tags",
			tags:      []Tag{"repo:owner/a", "agent:demo", "org:myorg"},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			for _, tag := range tt.tags {
				caps.Add(tag)
			}
			assert.Equal(t, tt.wantCount, caps.Count())
		})
	}
}

func TestCapabilities_AddAll(t *testing.T) {
	tests := []struct {
		name      string
		tags      []Tag
		wantCount int
	}{
		{
			name:      "add empty slice",
			tags:      []Tag{},
			wantCount: 0,
		},
		{
			name:      "add nil slice",
			tags:      nil,
			wantCount: 0,
		},
		{
			name:      "add single tag",
			tags:      []Tag{"repo:owner/name"},
			wantCount: 1,
		},
		{
			name:      "add multiple tags",
			tags:      []Tag{"repo:a", "agent:b", "org:c"},
			wantCount: 3,
		},
		{
			name:      "add tags with duplicates",
			tags:      []Tag{"tag1", "tag2", "tag1"},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			caps.AddAll(tt.tags)
			assert.Equal(t, tt.wantCount, caps.Count())
		})
	}
}

func TestCapabilities_Contains(t *testing.T) {
	tests := []struct {
		name      string
		setup     []Tag
		checkTag  Tag
		wantFound bool
	}{
		{
			name:      "tag present",
			setup:     []Tag{"repo:owner/name"},
			checkTag:  "repo:owner/name",
			wantFound: true,
		},
		{
			name:      "tag absent",
			setup:     []Tag{"repo:owner/name"},
			checkTag:  "repo:other/repo",
			wantFound: false,
		},
		{
			name:      "empty capabilities",
			setup:     []Tag{},
			checkTag:  "repo:owner/name",
			wantFound: false,
		},
		{
			name:      "one of many tags present",
			setup:     []Tag{"tag1", "tag2", "tag3"},
			checkTag:  "tag2",
			wantFound: true,
		},
		{
			name:      "empty tag string",
			setup:     []Tag{""},
			checkTag:  "",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			caps.AddAll(tt.setup)
			assert.Equal(t, tt.wantFound, caps.Contains(tt.checkTag))
		})
	}
}

func TestCapabilities_GetAll(t *testing.T) {
	tests := []struct {
		name     string
		setup    []Tag
		wantTags []Tag
	}{
		{
			name:     "empty capabilities returns empty slice",
			setup:    []Tag{},
			wantTags: []Tag{},
		},
		{
			name:     "returns all added tags",
			setup:    []Tag{"repo:owner/name", "agent:demo", "org:myorg"},
			wantTags: []Tag{"repo:owner/name", "agent:demo", "org:myorg"},
		},
		{
			name:     "deduplicated tags returned once",
			setup:    []Tag{"tag1", "tag1", "tag2"},
			wantTags: []Tag{"tag1", "tag2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			caps.AddAll(tt.setup)
			got := caps.GetAll()
			assert.ElementsMatch(t, tt.wantTags, got)
		})
	}
}

func TestCapabilities_Remove(t *testing.T) {
	tests := []struct {
		name      string
		setup     []Tag
		remove    Tag
		wantCount int
		wantGone  Tag
	}{
		{
			name:      "remove existing tag",
			setup:     []Tag{"tag1", "tag2", "tag3"},
			remove:    "tag2",
			wantCount: 2,
			wantGone:  "tag2",
		},
		{
			name:      "remove non-existing tag is no-op",
			setup:     []Tag{"tag1", "tag2"},
			remove:    "tag99",
			wantCount: 2,
			wantGone:  "tag99",
		},
		{
			name:      "remove from empty capabilities",
			setup:     []Tag{},
			remove:    "tag1",
			wantCount: 0,
			wantGone:  "tag1",
		},
		{
			name:      "remove only tag leaves empty",
			setup:     []Tag{"tag1"},
			remove:    "tag1",
			wantCount: 0,
			wantGone:  "tag1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			caps.AddAll(tt.setup)
			caps.Remove(tt.remove)
			assert.Equal(t, tt.wantCount, caps.Count())
			assert.False(t, caps.Contains(tt.wantGone), "Removed tag should not be present")
		})
	}
}

func TestCapabilities_Clear(t *testing.T) {
	tests := []struct {
		name  string
		setup []Tag
	}{
		{
			name:  "clear populated capabilities",
			setup: []Tag{"tag1", "tag2", "tag3"},
		},
		{
			name:  "clear empty capabilities is no-op",
			setup: []Tag{},
		},
		{
			name:  "clear single tag",
			setup: []Tag{"tag1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities()
			caps.AddAll(tt.setup)
			caps.Clear()
			assert.Equal(t, 0, caps.Count(), "Count should be 0 after Clear")
			assert.Empty(t, caps.GetAll(), "GetAll should return empty slice after Clear")
		})
	}
}

func TestCapabilities_Count(t *testing.T) {
	caps := NewCapabilities()

	assert.Equal(t, 0, caps.Count(), "Empty capabilities should have count 0")

	caps.Add("tag1")
	assert.Equal(t, 1, caps.Count())

	caps.Add("tag2")
	assert.Equal(t, 2, caps.Count())

	// Adding duplicate should not increase count
	caps.Add("tag1")
	assert.Equal(t, 2, caps.Count(), "Duplicate add should not change count")

	caps.Remove("tag1")
	assert.Equal(t, 1, caps.Count())

	caps.Clear()
	assert.Equal(t, 0, caps.Count(), "Count should be 0 after Clear")
}

func TestCapabilities_ClearAndReuse(t *testing.T) {
	caps := NewCapabilities()
	caps.AddAll([]Tag{"tag1", "tag2"})
	caps.Clear()

	// Verify capabilities are usable after Clear
	caps.Add("tag3")
	assert.Equal(t, 1, caps.Count())
	assert.True(t, caps.Contains("tag3"))
	assert.False(t, caps.Contains("tag1"), "Previously cleared tag should not be present")
}

func TestCapabilities_Concurrency(t *testing.T) {
	caps := NewCapabilities()
	const goroutines = 10
	const tagsPerGoroutine = 100

	var wg sync.WaitGroup

	// Concurrent adds
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tagsPerGoroutine; j++ {
				tag := Tag(string(rune('a'+id)) + string(rune('0'+j%10)))
				caps.Add(tag)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = caps.Count()
			_ = caps.GetAll()
			_ = caps.Contains("a0")
		}()
	}
	wg.Wait()

	// Concurrent mixed operations
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			tag := Tag(string(rune('a' + id)))
			caps.Add(tag)
			_ = caps.Contains(tag)
			caps.Remove(tag)
		}(i)
	}
	wg.Wait()

	// Final state: no panics and count is non-negative
	assert.GreaterOrEqual(t, caps.Count(), 0)
}
