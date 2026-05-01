package guard

import (
	"fmt"
	"strings"
)

// validateIntegrityField returns an error if raw is not a valid integrity-level
// string. fieldName is used in the error message (e.g. "disapproval-integrity").
func validateIntegrityField(fieldName string, raw interface{}) error {
	s, ok := raw.(string)
	if !ok {
		return fmt.Errorf("invalid %s value: expected one of none|unapproved|approved|merged", fieldName)
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "unapproved", "approved", "merged":
		return nil
	default:
		return fmt.Errorf("invalid %s value: expected one of none|unapproved|approved|merged", fieldName)
	}
}
