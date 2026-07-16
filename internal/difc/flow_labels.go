package difc

// labelKind is the constraint that phantom kind types must satisfy.
// Each concrete kind type encodes its flow semantics directly via these methods,
// avoiding runtime type assertions inside the hot path of CanFlowTo/CheckFlow.
type labelKind interface {
	// isSubset returns true for secrecy semantics (l ⊆ target) and false for
	// integrity semantics (l ⊇ target).
	isSubset() bool
	// typeName returns the display name used in log messages ("Secrecy" or "Integrity").
	typeName() string
}

// secrecyKind is the phantom type parameter for SecrecyLabel.
// Secrecy flow: l ⊆ target (source must be a subset of target).
type secrecyKind struct{}

func (secrecyKind) isSubset() bool   { return true }
func (secrecyKind) typeName() string { return "Secrecy" }

// integrityKind is the phantom type parameter for IntegrityLabel.
// Integrity flow: l ⊇ target (source must be a superset of target).
type integrityKind struct{}

func (integrityKind) isSubset() bool   { return false }
func (integrityKind) typeName() string { return "Integrity" }

// flowLabel is the internal generic label implementation parameterized by a phantom kind type T.
//
// The kind type T determines flow direction:
//   - flowLabel[secrecyKind]   — secrecy semantics: l ⊆ target (source ⊆ target)
//   - flowLabel[integrityKind] — integrity semantics: l ⊇ target (source ⊇ target)
type flowLabel[T labelKind] struct {
	Label *Label
}

// SecrecyLabel wraps Label with secrecy-specific flow semantics.
// Secrecy flow: data can only flow to contexts with equal or more secrecy tags.
// l ⊆ target (this has no tags that target doesn't have)
type SecrecyLabel = flowLabel[secrecyKind]

// IntegrityLabel wraps Label with integrity-specific flow semantics.
// Integrity flow: data can flow from high integrity to low integrity.
// l ⊇ target (this has all tags that target has)
type IntegrityLabel = flowLabel[integrityKind]

// NewSecrecyLabel creates a new secrecy label, optionally pre-populated with tags.
// Zero arguments produce an empty label; one or more arguments add those tags.
func NewSecrecyLabel(tags ...Tag) *SecrecyLabel {
	if len(tags) == 0 {
		return &SecrecyLabel{Label: NewLabel()}
	}
	return &SecrecyLabel{Label: newLabelWithTags(tags)}
}

// NewIntegrityLabel creates a new integrity label, optionally pre-populated with tags.
// Zero arguments produce an empty label; one or more arguments add those tags.
func NewIntegrityLabel(tags ...Tag) *IntegrityLabel {
	if len(tags) == 0 {
		return &IntegrityLabel{Label: NewLabel()}
	}
	return &IntegrityLabel{Label: newLabelWithTags(tags)}
}

// getLabel returns the underlying Label, or nil if the receiver is nil.
func (l *flowLabel[T]) getLabel() *Label {
	if l == nil {
		return nil
	}
	return l.Label
}

// CanFlowTo checks if this label can flow to target.
// For SecrecyLabel: l ⊆ target (source must have no tags absent from target).
// For IntegrityLabel: l ⊇ target (source must have all tags that target has).
func (l *flowLabel[T]) CanFlowTo(target *flowLabel[T]) bool {
	var kind T
	ok, _ := checkFlowHelper(l.getLabel(), target.getLabel(), kind.isSubset(), kind.typeName())
	return ok
}

// checkFlowHelper implements the common flow-check logic shared by SecrecyLabel and IntegrityLabel.
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

// CheckFlow checks if this label can flow to target and returns violation details if not.
func (l *flowLabel[T]) CheckFlow(target *flowLabel[T]) (bool, []Tag) {
	var kind T
	return checkFlowHelper(l.getLabel(), target.getLabel(), kind.isSubset(), kind.typeName())
}

// Clone creates an independent copy of the label.
func (l *flowLabel[T]) Clone() *flowLabel[T] {
	return &flowLabel[T]{Label: cloneLabelOrNew(l.getLabel())}
}
