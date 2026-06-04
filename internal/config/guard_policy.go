package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGuardPolicy = logger.New("config:guard_policy")

const (
	IntegrityNone       = "none"
	IntegrityUnapproved = "unapproved"
	IntegrityApproved   = "approved"
	IntegrityMerged     = "merged"
)

var allIntegrityLevels = []string{IntegrityNone, IntegrityUnapproved, IntegrityApproved, IntegrityMerged}

// AllIntegrityLevels returns the canonical ordered list of all valid integrity-level values.
// The returned slice is a defensive copy and safe for callers to modify.
func AllIntegrityLevels() []string {
	return append([]string(nil), allIntegrityLevels...)
}

var validMinIntegrityValues = map[string]struct{}{
	IntegrityNone:       {},
	IntegrityUnapproved: {},
	IntegrityApproved:   {},
	IntegrityMerged:     {},
}

// GuardPolicy represents the policy payload passed to guard label_agent.
type GuardPolicy struct {
	// Hyphenated keys intentionally match the policy schema field names.
	AllowOnly *AllowOnlyPolicy `toml:"allow-only" json:"allow-only,omitempty"`
	WriteSink *WriteSinkPolicy `toml:"write-sink" json:"write-sink,omitempty"`
}

// WriteSinkPolicy configures a write-sink guard that accepts writes from
// agents carrying the listed secrecy labels.
type WriteSinkPolicy struct {
	Accept []string `toml:"accept" json:"accept"`
}

// AllowOnlyPolicy configures scope and minimum required integrity.
type AllowOnlyPolicy struct {
	Repos                interface{}    `toml:"repos" json:"repos"`
	MinIntegrity         string         `toml:"min-integrity" json:"min-integrity"`
	ToolCallLimits       map[string]int `toml:"tool-call-limits" json:"tool-call-limits,omitempty"`
	BlockedUsers         []string       `toml:"blocked-users" json:"blocked-users,omitempty"`
	ApprovalLabels       []string       `toml:"approval-labels" json:"approval-labels,omitempty"`
	TrustedUsers         []string       `toml:"trusted-users" json:"trusted-users,omitempty"`
	EndorsementReactions []string       `toml:"endorsement-reactions" json:"endorsement-reactions,omitempty"`
	DisapprovalReactions []string       `toml:"disapproval-reactions" json:"disapproval-reactions,omitempty"`
	DisapprovalIntegrity string         `toml:"disapproval-integrity" json:"disapproval-integrity,omitempty"`
	EndorserMinIntegrity string         `toml:"endorser-min-integrity" json:"endorser-min-integrity,omitempty"`
	PromotionLabel       string         `toml:"promotion-label" json:"promotion-label,omitempty"`
	DemotionLabel        string         `toml:"demotion-label" json:"demotion-label,omitempty"`
}

// NormalizedGuardPolicy is a canonical policy representation for caching and observability.
type NormalizedGuardPolicy struct {
	ScopeKind            string         `json:"scope_kind"`
	ScopeValues          []string       `json:"scope_values,omitempty"`
	MinIntegrity         string         `json:"min-integrity"`
	ToolCallLimits       map[string]int `json:"tool-call-limits,omitempty"`
	BlockedUsers         []string       `json:"blocked-users,omitempty"`
	ApprovalLabels       []string       `json:"approval-labels,omitempty"`
	TrustedUsers         []string       `json:"trusted-users,omitempty"`
	EndorsementReactions []string       `json:"endorsement-reactions,omitempty"`
	DisapprovalReactions []string       `json:"disapproval-reactions,omitempty"`
	DisapprovalIntegrity string         `json:"disapproval-integrity,omitempty"`
	EndorserMinIntegrity string         `json:"endorser-min-integrity,omitempty"`
	PromotionLabel       string         `json:"promotion-label,omitempty"`
	DemotionLabel        string         `json:"demotion-label,omitempty"`
}

func (p *GuardPolicy) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	logGuardPolicy.Printf("UnmarshalJSON: parsing guard policy, keys=%d", len(raw))

	var allowOnlyRaw json.RawMessage
	var writeSinkRaw json.RawMessage
	for key, value := range raw {
		switch strings.ToLower(key) {
		case "allow-only", "allowonly":
			allowOnlyRaw = value
		case "write-sink", "writesink":
			writeSinkRaw = value
		default:
			return fmt.Errorf("policy contains unsupported field %q", key)
		}
	}

	if len(allowOnlyRaw) == 0 && len(writeSinkRaw) == 0 {
		return fmt.Errorf("policy must include allow-only or write-sink")
	}
	if len(allowOnlyRaw) > 0 && len(writeSinkRaw) > 0 {
		return fmt.Errorf("policy must include either allow-only or write-sink, not both")
	}

	if len(allowOnlyRaw) > 0 {
		var allowOnly AllowOnlyPolicy
		if err := json.Unmarshal(allowOnlyRaw, &allowOnly); err != nil {
			return err
		}
		p.AllowOnly = &allowOnly
	}

	if len(writeSinkRaw) > 0 {
		var writeSink WriteSinkPolicy
		if err := json.Unmarshal(writeSinkRaw, &writeSink); err != nil {
			return err
		}
		p.WriteSink = &writeSink
	}

	if len(allowOnlyRaw) > 0 {
		logGuardPolicy.Print("UnmarshalJSON: guard policy type is allow-only")
	} else {
		logGuardPolicy.Print("UnmarshalJSON: guard policy type is write-sink")
	}
	return nil
}

func (p GuardPolicy) MarshalJSON() ([]byte, error) {
	type serializedPolicy struct {
		AllowOnly *AllowOnlyPolicy `json:"allow-only,omitempty"`
		WriteSink *WriteSinkPolicy `json:"write-sink,omitempty"`
	}

	return json.Marshal(serializedPolicy(p))
}

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
	if payload == nil {
		return nil, fmt.Errorf("policy must decode to a JSON object")
	}

	return payload, nil
}

func (p *AllowOnlyPolicy) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	logGuardPolicy.Printf("UnmarshalJSON: parsing allow-only policy, fields=%d", len(raw))

	for key, value := range raw {
		switch strings.ToLower(key) {
		case "repos":
			if err := json.Unmarshal(value, &p.Repos); err != nil {
				return fmt.Errorf("invalid allow-only.repos: %w", err)
			}
		case "min-integrity", "integrity":
			if err := json.Unmarshal(value, &p.MinIntegrity); err != nil {
				return fmt.Errorf("invalid allow-only.min-integrity: %w", err)
			}
		case "tool-call-limits":
			if err := json.Unmarshal(value, &p.ToolCallLimits); err != nil {
				return fmt.Errorf("invalid allow-only.tool-call-limits: %w", err)
			}
		case "blocked-users":
			if err := json.Unmarshal(value, &p.BlockedUsers); err != nil {
				return fmt.Errorf("invalid allow-only.blocked-users: %w", err)
			}
		case "approval-labels":
			if err := json.Unmarshal(value, &p.ApprovalLabels); err != nil {
				return fmt.Errorf("invalid allow-only.approval-labels: %w", err)
			}
		case "trusted-users":
			if err := json.Unmarshal(value, &p.TrustedUsers); err != nil {
				return fmt.Errorf("invalid allow-only.trusted-users: %w", err)
			}
		case "endorsement-reactions":
			if err := json.Unmarshal(value, &p.EndorsementReactions); err != nil {
				return fmt.Errorf("invalid allow-only.endorsement-reactions: %w", err)
			}
		case "disapproval-reactions":
			if err := json.Unmarshal(value, &p.DisapprovalReactions); err != nil {
				return fmt.Errorf("invalid allow-only.disapproval-reactions: %w", err)
			}
		case "disapproval-integrity":
			if err := json.Unmarshal(value, &p.DisapprovalIntegrity); err != nil {
				return fmt.Errorf("invalid allow-only.disapproval-integrity: %w", err)
			}
		case "endorser-min-integrity":
			if err := json.Unmarshal(value, &p.EndorserMinIntegrity); err != nil {
				return fmt.Errorf("invalid allow-only.endorser-min-integrity: %w", err)
			}
		case "promotion-label":
			if err := json.Unmarshal(value, &p.PromotionLabel); err != nil {
				return fmt.Errorf("invalid allow-only.promotion-label: %w", err)
			}
		case "demotion-label":
			if err := json.Unmarshal(value, &p.DemotionLabel); err != nil {
				return fmt.Errorf("invalid allow-only.demotion-label: %w", err)
			}
		default:
			return fmt.Errorf("allow-only contains unsupported field %q", key)
		}
	}

	if p.Repos == nil {
		return fmt.Errorf("allow-only must include repos")
	}
	if strings.TrimSpace(p.MinIntegrity) == "" {
		return fmt.Errorf("allow-only must include min-integrity")
	}

	logGuardPolicy.Printf("UnmarshalJSON: allow-only policy parsed, repos=%T, minIntegrity=%s", p.Repos, p.MinIntegrity)
	return nil
}

func (p AllowOnlyPolicy) MarshalJSON() ([]byte, error) {
	type serializedAllowOnly struct {
		Repos                interface{}    `json:"repos"`
		MinIntegrity         string         `json:"min-integrity"`
		ToolCallLimits       map[string]int `json:"tool-call-limits,omitempty"`
		BlockedUsers         []string       `json:"blocked-users,omitempty"`
		ApprovalLabels       []string       `json:"approval-labels,omitempty"`
		TrustedUsers         []string       `json:"trusted-users,omitempty"`
		EndorsementReactions []string       `json:"endorsement-reactions,omitempty"`
		DisapprovalReactions []string       `json:"disapproval-reactions,omitempty"`
		DisapprovalIntegrity string         `json:"disapproval-integrity,omitempty"`
		EndorserMinIntegrity string         `json:"endorser-min-integrity,omitempty"`
		PromotionLabel       string         `json:"promotion-label,omitempty"`
		DemotionLabel        string         `json:"demotion-label,omitempty"`
	}

	return json.Marshal(serializedAllowOnly(p))
}

// WriteSinkAcceptRules documents the mapping from allow-only repos configuration
// to the required write-sink accept values.
//
// The write-sink accept field must be a superset of the agent's secrecy tags,
// which are determined by the allow-only repos configuration:
//
//	repos = "all"              → agent secrecy = []           → accept = ["*"] (wildcard)
//	repos = "public"           → agent secrecy = []           → accept = ["*"] (wildcard)
//	repos = ["O/R"]            → agent secrecy = ["private:O/R"]
//	                             accept = ["private:O/R"]
//	repos = ["O/*"]            → agent secrecy = ["private:O"]
//	                             accept = ["private:O"]
//	repos = ["O/P*"]           → agent secrecy = ["private:O/P*"]
//	                             accept = ["private:O/P*"]
//	repos = ["O/R1", "O/R2"]  → agent secrecy = ["private:O/R1", "private:O/R2"]
//	                             accept = ["private:O/R1", "private:O/R2"]
//	repos = ["O1/*", "O2/R"]  → agent secrecy = ["private:O1", "private:O2/R"]
//	                             accept = ["private:O1", "private:O2/R"]
//
// The transformation rule:
//
//	repos entry "O/*"  (owner wildcard)  → accept "private:O"    (bare owner)
//	repos entry "O/P*" (prefix wildcard) → accept "private:O/P*" (prefix preserved)
//	repos entry "O/R"  (exact repo)      → accept "private:O/R"  (exact preserved)
//
// Wildcard accept:
//
//	accept = ["*"] means "accept writes from any agent regardless of secrecy".
//	This is the correct configuration for repos="all" and repos="public" where
//	the agent has no secrecy tags. The write-sink is still required to prevent
//	the noop guard integrity violation (see WriteSinkGuard godoc).
//	The wildcard "*" must be the sole entry — it cannot be mixed with other patterns.
//
// Note: min-integrity has no effect on these rules (it only affects integrity labels).
var WriteSinkAcceptRules = "see godoc" // exists for documentation only

// IsWriteSinkPolicy returns true if this policy configures a write-sink guard.
func (p *GuardPolicy) IsWriteSinkPolicy() bool {
	return p != nil && p.WriteSink != nil
}
