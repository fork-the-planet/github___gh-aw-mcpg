package guard

import (
	"github.com/github/gh-aw-mcpg/internal/config"
)

// validateIntegrityField returns an error if raw is not a valid integrity-level
// string. fieldName is used in the error message (e.g. "disapproval-integrity").
// It delegates to config.ValidateAndNormalizeIntegrityField for validation.
func validateIntegrityField(fieldName string, raw interface{}) error {
	s, ok := raw.(string)
	if !ok {
		s = ""
	}
	_, err := config.ValidateAndNormalizeIntegrityField(fieldName, s, false)
	return err
}
