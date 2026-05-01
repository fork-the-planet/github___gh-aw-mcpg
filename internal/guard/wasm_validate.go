package guard

import (
	"fmt"
	"strings"
)

// allowedIntegrityLevels is the single source of truth for valid integrity-level values.
var allowedIntegrityLevels = []string{"none", "unapproved", "approved", "merged"}

var allowedIntegrityLevelSet = map[string]struct{}{
	"none":       {},
	"unapproved": {},
	"approved":   {},
	"merged":     {},
}

func invalidIntegrityFieldError(fieldName string) error {
	return fmt.Errorf(
		"invalid %s value: expected one of %s",
		fieldName,
		strings.Join(allowedIntegrityLevels, "|"),
	)
}

// validateIntegrityField returns an error if raw is not a valid integrity-level
// string. fieldName is used in the error message (e.g. "disapproval-integrity").
func validateIntegrityField(fieldName string, raw interface{}) error {
	s, ok := raw.(string)
	if !ok {
		return invalidIntegrityFieldError(fieldName)
	}
	normalized := strings.ToLower(strings.TrimSpace(s))
	if _, ok := allowedIntegrityLevelSet[normalized]; ok {
		return nil
	}
	return invalidIntegrityFieldError(fieldName)
}
