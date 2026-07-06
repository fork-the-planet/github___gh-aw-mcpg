package difc

import "sync"

// tagSet is an unexported concurrent set of Tags backed by a map and a RWMutex.
// It provides the common concurrent mutation and read operations shared by Label
// and Capabilities, eliminating duplicated locking logic across both types.
type tagSet struct {
	tags map[Tag]struct{}
	mu   sync.RWMutex
}

// newTagSet creates and initialises an empty tagSet.
func newTagSet() tagSet {
	return tagSet{tags: make(map[Tag]struct{})}
}

func (ts *tagSet) add(tag Tag) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tags[tag] = struct{}{}
}

func (ts *tagSet) addAll(tags []Tag) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, tag := range tags {
		ts.tags[tag] = struct{}{}
	}
}

func (ts *tagSet) remove(tag Tag) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.tags, tag)
}

func (ts *tagSet) removeAll(tags []Tag) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, tag := range tags {
		delete(ts.tags, tag)
	}
}

func (ts *tagSet) contains(tag Tag) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	_, ok := ts.tags[tag]
	return ok
}

func (ts *tagSet) getAll() []Tag {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	tags := make([]Tag, 0, len(ts.tags))
	for tag := range ts.tags {
		tags = append(tags, tag)
	}
	return tags
}

func (ts *tagSet) clear() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tags = make(map[Tag]struct{})
}

func (ts *tagSet) count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.tags)
}

func (ts *tagSet) isEmpty() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.tags) == 0
}
