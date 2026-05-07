package guard

import (
	"fmt"
	"strings"
)

// AllowedIntegrityLevels is the single source of truth for valid integrity-level values.
var AllowedIntegrityLevels = []string{"none", "unapproved", "approved", "merged"}

var allowedIntegrityLevelSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(AllowedIntegrityLevels))
	for _, level := range AllowedIntegrityLevels {
		m[level] = struct{}{}
	}
	return m
}()

func invalidIntegrityFieldError(fieldName string) error {
	return fmt.Errorf(
		"invalid %s value: expected one of %s",
		fieldName,
		strings.Join(AllowedIntegrityLevels, "|"),
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
