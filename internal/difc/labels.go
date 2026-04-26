package difc

import (
	"fmt"
	"strings"
	"sync"

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
	tags map[Tag]struct{}
	mu   sync.RWMutex
}

// NewLabel creates a new empty label
func NewLabel() *Label {
	return &Label{tags: make(map[Tag]struct{})}
}

// newLabelWithTags is a helper function that creates a label with the given tags.
// This helper reduces duplication in NewSecrecyLabelWithTags and NewIntegrityLabelWithTags.
func newLabelWithTags(tags []Tag) *Label {
	logLabels.Printf("Creating label with %d initial tags: %v", len(tags), tags)
	label := NewLabel()
	label.AddAll(tags)
	return label
}

// Add adds a tag to this label
func (l *Label) Add(tag Tag) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tags[tag] = struct{}{}
}

// AddAll adds multiple tags to this label
func (l *Label) AddAll(tags []Tag) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, tag := range tags {
		l.tags[tag] = struct{}{}
	}
}

// Remove removes a single tag from this label
func (l *Label) Remove(tag Tag) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.tags, tag)
}

// RemoveAll removes multiple tags from this label
func (l *Label) RemoveAll(tags []Tag) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, tag := range tags {
		delete(l.tags, tag)
	}
}

// Contains checks if this label contains a specific tag
func (l *Label) Contains(tag Tag) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.tags[tag]
	return ok
}

// Union merges another label into this label
func (l *Label) Union(other *Label) {
	if other == nil {
		return
	}
	other.mu.RLock()
	defer other.mu.RUnlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	added := 0
	for tag := range other.tags {
		if _, exists := l.tags[tag]; !exists {
			l.tags[tag] = struct{}{}
			added++
		}
	}
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
		defer l.mu.Unlock()
		l.tags = make(map[Tag]struct{})
		return
	}
	other.mu.RLock()
	defer other.mu.RUnlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	// Remove tags not in other
	for tag := range l.tags {
		if _, ok := other.tags[tag]; !ok {
			delete(l.tags, tag)
		}
	}
}

// Clone creates a copy of this label
func (l *Label) Clone() *Label {
	l.mu.RLock()
	defer l.mu.RUnlock()
	newLabel := NewLabel()
	for tag := range l.tags {
		newLabel.tags[tag] = struct{}{}
	}
	return newLabel
}

// GetTags returns all tags in this label as a slice
func (l *Label) GetTags() []Tag {
	l.mu.RLock()
	defer l.mu.RUnlock()
	tags := make([]Tag, 0, len(l.tags))
	for tag := range l.tags {
		tags = append(tags, tag)
	}
	return tags
}

// IsEmpty returns true if this label has no tags
func (l *Label) IsEmpty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.tags) == 0
}

// cloneLabelOrNew clones inner if it is non-nil, otherwise returns a new empty Label.
// This helper centralizes the nil-guard logic shared by SecrecyLabel.Clone and IntegrityLabel.Clone.
func cloneLabelOrNew(inner *Label) *Label {
	if inner == nil {
		return NewLabel()
	}
	return inner.Clone()
}

// SecrecyLabel wraps Label with secrecy-specific flow semantics
// Secrecy flow: data can only flow to contexts with equal or more secrecy tags
// l ⊆ target (this has no tags that target doesn't have)
type SecrecyLabel struct {
	Label *Label
}

// NewSecrecyLabel creates a new empty secrecy label
func NewSecrecyLabel() *SecrecyLabel {
	return &SecrecyLabel{Label: NewLabel()}
}

// NewSecrecyLabelWithTags creates a secrecy label with the given tags
func NewSecrecyLabelWithTags(tags []Tag) *SecrecyLabel {
	return &SecrecyLabel{Label: newLabelWithTags(tags)}
}

// getLabel returns the underlying Label, or nil if the receiver or its underlying Label is nil.
func (l *SecrecyLabel) getLabel() *Label {
	if l == nil {
		return nil
	}
	return l.Label
}

// CanFlowTo checks if this secrecy label can flow to target
// Secrecy semantics: l ⊆ target (this has no tags that target doesn't have)
// Data can only flow to contexts with equal or more secrecy tags
func (l *SecrecyLabel) CanFlowTo(target *SecrecyLabel) bool {
	ok, _ := checkFlowHelper(l.getLabel(), target.getLabel(), true, "Secrecy")
	return ok
}

// checkFlowHelper is a generic helper that implements the common CheckFlow pattern.
// It checks if tags can flow from source to target according to the specified flow semantics.
//
// Parameters:
//   - srcLabel: The source label (may be nil)
//   - targetLabel: The target label (may be nil)
//   - checkSubset: If true, checks source ⊆ target (secrecy semantics: source must be subset of target)
//     If false, checks source ⊇ target (integrity semantics: source must be superset of target)
//   - labelType: The type of label being checked ("Secrecy" or "Integrity") for logging
//
// Returns:
//   - bool: true if flow is allowed
//   - []Tag: violating tags (tags that prevent the flow)
func checkFlowHelper(srcLabel *Label, targetLabel *Label, checkSubset bool, labelType string) (bool, []Tag) {
	// Handle nil source
	if srcLabel == nil {
		if checkSubset {
			// Secrecy: nil/empty source can flow to anything
			return true, nil
		}
		// Integrity: nil source can only flow to nil/empty target
		if targetLabel == nil || targetLabel.IsEmpty() {
			return true, nil
		}
		violatingTags := targetLabel.GetTags()
		logLabels.Printf("%s CheckFlow denied: source is nil but target requires tags=%v", labelType, violatingTags)
		return false, violatingTags
	}

	// Handle nil target
	if targetLabel == nil {
		if checkSubset {
			// Secrecy: only empty source can flow to nil target
			if srcLabel.IsEmpty() {
				return true, nil
			}
			violatingTags := srcLabel.GetTags()
			logLabels.Printf("%s CheckFlow denied: target is nil/empty but source has tags=%v", labelType, violatingTags)
			return false, violatingTags
		}
		// Integrity: any source can flow to nil target
		return true, nil
	}

	// Both labels are non-nil, perform the actual check
	srcLabel.mu.RLock()
	defer srcLabel.mu.RUnlock()
	targetLabel.mu.RLock()
	defer targetLabel.mu.RUnlock()

	// Wildcard: "*" in the superset side means "accept all"
	if checkSubset {
		// Secrecy: src ⊆ target — wildcard in target means target accepts all
		if _, ok := targetLabel.tags[WildcardTag]; ok {
			return true, nil
		}
	} else {
		// Integrity: target ⊆ src — wildcard in src means src has all
		if _, ok := srcLabel.tags[WildcardTag]; ok {
			return true, nil
		}
	}

	var violatingTags []Tag
	if checkSubset {
		// Secrecy semantics: Check if all tags in source are in target (source ⊆ target)
		for tag := range srcLabel.tags {
			if _, ok := targetLabel.tags[tag]; !ok {
				violatingTags = append(violatingTags, tag)
			}
		}
		if len(violatingTags) > 0 {
			logLabels.Printf("%s CheckFlow denied: source has tags not in target, extraTags=%v", labelType, violatingTags)
		}
	} else {
		// Integrity semantics: Check if all tags in target are in source (source ⊇ target)
		for tag := range targetLabel.tags {
			if _, ok := srcLabel.tags[tag]; !ok {
				violatingTags = append(violatingTags, tag)
			}
		}
		if len(violatingTags) > 0 {
			logLabels.Printf("%s CheckFlow denied: source missing required tags=%v", labelType, violatingTags)
		}
	}

	return len(violatingTags) == 0, violatingTags
}

// CheckFlow checks if this secrecy label can flow to target and returns violation details if not
func (l *SecrecyLabel) CheckFlow(target *SecrecyLabel) (bool, []Tag) {
	return checkFlowHelper(l.getLabel(), target.getLabel(), true, "Secrecy")
}

// Clone creates a copy of the secrecy label
func (l *SecrecyLabel) Clone() *SecrecyLabel {
	return &SecrecyLabel{Label: cloneLabelOrNew(l.getLabel())}
}

// IntegrityLabel wraps Label with integrity-specific flow semantics
// Integrity flow: data can flow from high integrity to low integrity
// l ⊇ target (this has all tags that target has)
type IntegrityLabel struct {
	Label *Label
}

// NewIntegrityLabel creates a new empty integrity label
func NewIntegrityLabel() *IntegrityLabel {
	return &IntegrityLabel{Label: NewLabel()}
}

// NewIntegrityLabelWithTags creates an integrity label with the given tags
func NewIntegrityLabelWithTags(tags []Tag) *IntegrityLabel {
	return &IntegrityLabel{Label: newLabelWithTags(tags)}
}

// getLabel returns the underlying Label, or nil if the receiver is nil.
func (l *IntegrityLabel) getLabel() *Label {
	if l == nil {
		return nil
	}
	return l.Label
}

// CanFlowTo checks if this integrity label can flow to target
// Integrity semantics: l ⊇ target (this has all tags that target has)
// For writes: agent must have >= integrity than endpoint
// For reads: endpoint must have >= integrity than agent
func (l *IntegrityLabel) CanFlowTo(target *IntegrityLabel) bool {
	ok, _ := checkFlowHelper(l.getLabel(), target.getLabel(), false, "Integrity")
	return ok
}

// CheckFlow checks if this integrity label can flow to target and returns violation details if not
func (l *IntegrityLabel) CheckFlow(target *IntegrityLabel) (bool, []Tag) {
	return checkFlowHelper(l.getLabel(), target.getLabel(), false, "Integrity")
}

// Clone creates a copy of the integrity label
func (l *IntegrityLabel) Clone() *IntegrityLabel {
	return &IntegrityLabel{Label: cloneLabelOrNew(l.getLabel())}
}

// ViolationType indicates what kind of DIFC violation occurred
type ViolationType string

const (
	SecrecyViolation   ViolationType = "secrecy"
	IntegrityViolation ViolationType = "integrity"
)

// ViolationError provides detailed information about a DIFC (Decentralized Information Flow Control) violation.
// It describes what kind of violation occurred, which resource was involved, and what needs to be
// done to resolve the violation.
//
// This error type implements the error interface and provides human-readable error messages
// that explain the violation and suggest remediation steps. DIFC violations occur when:
//   - Secrecy: An agent tries to access a resource but has secrecy tags that would leak sensitive information
//   - Integrity: An agent tries to write to a resource but lacks the required integrity tags to ensure trustworthiness
//
// Fields:
//   - Type: The kind of violation (SecrecyViolation or IntegrityViolation)
//   - Resource: Human-readable description of the resource being accessed
//   - IsWrite: true for write operations, false for read operations
//   - MissingTags: Tags the agent needs but doesn't have (for integrity violations)
//   - ExtraTags: Tags the agent has but shouldn't (for secrecy violations)
//   - AgentTags: Complete set of the agent's tags (for context)
//   - ResourceTags: Complete set of the resource's tags (for context)
type ViolationError struct {
	Type         ViolationType
	Resource     string // Resource description
	IsWrite      bool   // true for write, false for read
	MissingTags  []Tag  // Tags the agent needs but doesn't have
	ExtraTags    []Tag  // Tags the agent has but shouldn't
	AgentTags    []Tag  // All agent tags (for context)
	ResourceTags []Tag  // All resource tags (for context)
}

func (e *ViolationError) Error() string {
	var msg string

	if e.Type == SecrecyViolation {
		msg = fmt.Sprintf("Secrecy violation for resource '%s': ", e.Resource)
		if len(e.ExtraTags) > 0 {
			msg += fmt.Sprintf("the agent is not authorized to access data with secrecy level %s.", formatSecrecyLevel(e.ExtraTags))
		}
	} else {
		if e.IsWrite {
			msg = fmt.Sprintf("Integrity violation for write to resource '%s': ", e.Resource)
			if len(e.MissingTags) > 0 {
				msg += fmt.Sprintf("the agent's integrity level is insufficient; it needs %s integrity.", formatIntegrityLevel(e.MissingTags))
			}
		} else {
			msg = fmt.Sprintf("Integrity violation for read from resource '%s': ", e.Resource)
			if len(e.MissingTags) > 0 {
				msg += fmt.Sprintf("the agent cannot read data with integrity below %s.", formatIntegrityLevel(e.MissingTags))
			}
		}
	}

	return msg
}

// Detailed returns a detailed error message with full context
func (e *ViolationError) Detailed() string {
	msg := e.Error()
	msg += fmt.Sprintf("\n  Agent %s tags: %v", e.Type, e.AgentTags)
	msg += fmt.Sprintf("\n  Resource %s tags: %v", e.Type, e.ResourceTags)
	return msg
}

// formatIntegrityLevel converts a list of integrity tags into a human-readable
// integrity level description (e.g., "approved" instead of "[unapproved:all approved:all]").
func formatIntegrityLevel(tags []Tag) string {
	if len(tags) == 0 {
		return "none"
	}
	// Find the highest integrity level mentioned in the tags
	highest := ""
	for _, tag := range tags {
		s := string(tag)
		// Strip scope suffix (e.g., "approved:all" → "approved")
		if idx := strings.Index(s, ":"); idx > 0 {
			s = s[:idx]
		}
		switch s {
		case "merged":
			return "\"merged\""
		case "approved":
			highest = "\"approved\""
		case "unapproved":
			if highest == "" {
				highest = "\"unapproved\""
			}
		}
	}
	if highest != "" {
		return highest
	}
	return fmt.Sprintf("%v", tags)
}

// formatSecrecyLevel converts a list of secrecy tags into a human-readable
// secrecy scope description (e.g., "private (org/repo)" instead of "[private:org/repo]").
func formatSecrecyLevel(tags []Tag) string {
	if len(tags) == 0 {
		return "public"
	}

	bestScope := ""
	hasPrivate := false

	for _, tag := range tags {
		s := string(tag)
		if strings.HasPrefix(s, "private:") {
			scope := strings.TrimPrefix(s, "private:")
			if scope != "" && len(scope) > len(bestScope) {
				bestScope = scope
			}
			continue
		}
		if s == "private" {
			hasPrivate = true
		}
	}

	if bestScope != "" {
		return fmt.Sprintf("private (%s)", bestScope)
	}
	if hasPrivate {
		return "private"
	}
	return fmt.Sprintf("%v", tags)
}
