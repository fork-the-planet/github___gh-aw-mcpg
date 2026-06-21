package guard

import (
	"fmt"
	"strings"

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
