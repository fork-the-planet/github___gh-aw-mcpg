package config

import (
	"encoding/json"
	"fmt"
)

// GuardPolicyToMap converts a policy value to a generic map through a JSON
// roundtrip. It returns an error if the value cannot be serialized or does not
// decode to a JSON object.
func GuardPolicyToMap(policy interface{}) (map[string]interface{}, error) {
	if policy == nil {
		return nil, fmt.Errorf("policy is required")
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize policy: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(policyJSON, &payload); err != nil {
		return nil, fmt.Errorf("policy must decode to a JSON object: %w", err)
	}

	return payload, nil
}
