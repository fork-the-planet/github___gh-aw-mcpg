package guard

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// AllowedIntegrityLevels is derived from the canonical integrity levels in config.
var AllowedIntegrityLevels = config.AllIntegrityLevels()

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
	if _, err := config.NormalizeIntegrityLevel(s, false); err == nil {
		return nil
	}
	return invalidIntegrityFieldError(fieldName)
}

// validateStringArray checks that raw is a []interface{} of non-empty strings.
// When requireNonEmpty is true, a zero-length array is also rejected.
func validateStringArray(fieldName string, raw interface{}, requireNonEmpty bool) error {
	return config.ValidateStringArrayField(fieldName, raw, requireNonEmpty)
}

// isValidAllowOnlyRepos returns true if repos is either a recognised string
// shorthand ("all" or "public") or a non-empty array of strings.
func isValidAllowOnlyRepos(repos interface{}) bool {
	return config.IsValidAllowOnlyReposValue(repos)
}

// checkBoolFailure returns a non-nil error if the given raw response map
// contains field key set to false, extracting the "error" message if present.
func checkBoolFailure(raw map[string]interface{}, resultJSON []byte, key string) error {
	val, ok := raw[key].(bool)
	if !ok || val {
		return nil // field absent or true — not a failure
	}
	if message, msgOK := raw["error"].(string); msgOK && strings.TrimSpace(message) != "" {
		logWasm.Printf("label_agent response indicated failure: error=%s, response=%s", message, string(resultJSON))
		return fmt.Errorf("label_agent rejected policy: %s", message)
	}
	logWasm.Printf("label_agent response indicated non-success status: response=%s", string(resultJSON))
	return fmt.Errorf("label_agent returned non-success status")
}
