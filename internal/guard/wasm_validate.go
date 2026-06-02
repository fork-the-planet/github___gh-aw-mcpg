package guard

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// AllowedIntegrityLevels is derived from the canonical integrity constants in config.
var AllowedIntegrityLevels = []string{
	config.IntegrityNone,
	config.IntegrityUnapproved,
	config.IntegrityApproved,
	config.IntegrityMerged,
}

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

// validateStringArray checks that raw is a []interface{} of non-empty strings.
// When requireNonEmpty is true, a zero-length array is also rejected.
func validateStringArray(fieldName string, raw interface{}, requireNonEmpty bool) error {
	arr, ok := raw.([]interface{})
	if !ok {
		if requireNonEmpty {
			return fmt.Errorf("invalid %s value: expected non-empty array of strings", fieldName)
		}
		return fmt.Errorf("invalid %s value: expected array of strings", fieldName)
	}
	if requireNonEmpty && len(arr) == 0 {
		return fmt.Errorf("invalid %s value: must be a non-empty array when present", fieldName)
	}
	for _, entry := range arr {
		if s, ok := entry.(string); !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("invalid %s value: each entry must be a non-empty string", fieldName)
		}
	}
	return nil
}

// isValidAllowOnlyRepos returns true if repos is either a recognised string
// shorthand ("all" or "public") or a non-empty array of strings.
func isValidAllowOnlyRepos(repos interface{}) bool {
	switch value := repos.(type) {
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(value))
		return trimmed == "all" || trimmed == "public"
	case []interface{}:
		if len(value) == 0 {
			return false
		}
		for _, entry := range value {
			if _, ok := entry.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
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
