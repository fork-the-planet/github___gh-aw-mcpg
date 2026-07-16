package difc

import (
	"fmt"
	"strings"
)

// ViolationType indicates what kind of DIFC violation occurred
type ViolationType string

const (
	SecrecyViolation   ViolationType = "secrecy"
	IntegrityViolation ViolationType = "integrity"
)

// ViolationError provides detailed information about a DIFC (Decentralized Information Flow Control) violation.
// It describes what kind of violation occurred, which resource was involved, and what needs to be
// done to resolve the violation.
//
// This error type implements the error interface and provides human-readable error messages
// that explain the violation and suggest remediation steps. DIFC violations occur when:
//   - Secrecy: An agent tries to access a resource but has secrecy tags that would leak sensitive information
//   - Integrity: An agent tries to write to a resource but lacks the required integrity tags to ensure trustworthiness
//
// Fields:
//   - Type: The kind of violation (SecrecyViolation or IntegrityViolation)
//   - Resource: Human-readable description of the resource being accessed
//   - IsWrite: true for write operations, false for read operations
//   - MissingTags: Tags the agent needs but doesn't have (for integrity violations)
//   - ExtraTags: Tags the agent has but shouldn't (for secrecy violations)
//   - AgentTags: Complete set of the agent's tags (for context)
//   - ResourceTags: Complete set of the resource's tags (for context)
type ViolationError struct {
	Type         ViolationType
	Resource     string // Resource description
	IsWrite      bool   // true for write, false for read
	MissingTags  []Tag  // Tags the agent needs but doesn't have
	ExtraTags    []Tag  // Tags the agent has but shouldn't
	AgentTags    []Tag  // All agent tags (for context)
	ResourceTags []Tag  // All resource tags (for context)
}

func (e *ViolationError) Error() string {
	logLabels.Printf("Formatting %s violation: resource=%s, isWrite=%v", e.Type, e.Resource, e.IsWrite)
	var msg string

	if e.Type == SecrecyViolation {
		msg = fmt.Sprintf("Secrecy violation for resource '%s': ", e.Resource)
		if len(e.ExtraTags) > 0 {
			msg += fmt.Sprintf("the agent is not authorized to access data with secrecy level %s.", formatSecrecyLevel(e.ExtraTags))
		}
	} else {
		if e.IsWrite {
			msg = fmt.Sprintf("Integrity violation for write to resource '%s': ", e.Resource)
			if len(e.MissingTags) > 0 {
				msg += fmt.Sprintf("the agent's integrity level is insufficient; it needs %s integrity.", formatIntegrityLevel(e.MissingTags))
			}
		} else {
			msg = fmt.Sprintf("Integrity violation for read from resource '%s': ", e.Resource)
			if len(e.MissingTags) > 0 {
				msg += fmt.Sprintf("the agent cannot read data with integrity below %s.", formatIntegrityLevel(e.MissingTags))
			}
		}
	}

	return msg
}

// Detailed returns a detailed error message with full context
func (e *ViolationError) Detailed() string {
	msg := e.Error()
	msg += fmt.Sprintf("\n  Agent %s tags: %v", e.Type, e.AgentTags)
	msg += fmt.Sprintf("\n  Resource %s tags: %v", e.Type, e.ResourceTags)
	return msg
}

// FormatViolationError creates a detailed error message explaining the violation and its implications.
func FormatViolationError(result *EvaluationResult, agentSecrecy *SecrecyLabel, agentIntegrity *IntegrityLabel, resource *LabeledResource) error {
	logLabels.Printf("FormatViolationError: decision=%s, reason=%q", result.Decision, result.Reason)
	if result.Decision == AccessAllow {
		return nil
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("DIFC Violation: %s\n\n", result.Reason))

	if len(result.SecrecyToAdd) > 0 {
		msg.WriteString(fmt.Sprintf("Required Action: Add secrecy tags %v\n", result.SecrecyToAdd))
		msg.WriteString("\nImplications of adding secrecy tags:\n")
		msg.WriteString("  - Agent will be restricted from writing to resources that lack these tags\n")
		msg.WriteString("  - This includes public resources (e.g., public repositories, public internet)\n")
		msg.WriteString("  - Agent will be marked as handling sensitive information\n")
		msg.WriteString(fmt.Sprintf("  - Future writes must target resources with tags: %v\n", result.SecrecyToAdd))
	}

	if len(result.IntegrityToDrop) > 0 {
		msg.WriteString(fmt.Sprintf("\nRequired Action: Drop integrity tags %v\n", result.IntegrityToDrop))
		msg.WriteString("\nImplications of dropping integrity tags:\n")
		msg.WriteString("  - Agent will no longer be able to write to high-integrity resources\n")
		msg.WriteString(fmt.Sprintf("  - Specifically, agent cannot write to resources requiring tags: %v\n", result.IntegrityToDrop))
		msg.WriteString("  - This action acknowledges that agent has been influenced by lower-integrity data\n")
		msg.WriteString("  - Agent's outputs will be considered less trustworthy\n")
	}

	msg.WriteString("\nCurrent Agent Labels:\n")
	msg.WriteString(fmt.Sprintf("  Secrecy: %v\n", agentSecrecy.Label.GetTags()))
	msg.WriteString(fmt.Sprintf("  Integrity: %v\n", agentIntegrity.Label.GetTags()))

	msg.WriteString("\nResource Requirements:\n")
	msg.WriteString(fmt.Sprintf("  Secrecy: %v\n", resource.Secrecy.Label.GetTags()))
	msg.WriteString(fmt.Sprintf("  Integrity: %v\n", resource.Integrity.Label.GetTags()))

	return fmt.Errorf("%s", msg.String())
}

// formatIntegrityLevel converts a list of integrity tags into a human-readable
// integrity level description (e.g., `"approved"` instead of "[unapproved:all approved:all]").
func formatIntegrityLevel(tags []Tag) string {
	if len(tags) == 0 {
		return "none"
	}
	// Find the highest integrity level mentioned in the tags
	highest := ""
	for _, tag := range tags {
		s := string(tag)
		// Strip scope suffix (e.g., "approved:all" → "approved")
		if idx := strings.Index(s, ":"); idx > 0 {
			s = s[:idx]
		}
		switch s {
		case "merged":
			logLabels.Printf("formatIntegrityLevel: resolved to \"merged\" from tags=%v", tags)
			return "\"merged\""
		case "approved":
			highest = "\"approved\""
		case "unapproved":
			if highest == "" {
				highest = "\"unapproved\""
			}
		}
	}
	if highest != "" {
		logLabels.Printf("formatIntegrityLevel: resolved to %s from tags=%v", highest, tags)
		return highest
	}
	return fmt.Sprintf("%v", tags)
}

// formatSecrecyLevel converts a list of secrecy tags into a human-readable
// secrecy scope description (e.g., "private (org/repo)" instead of "[private:org/repo]").
func formatSecrecyLevel(tags []Tag) string {
	if len(tags) == 0 {
		return "public"
	}

	bestScope := ""
	hasPrivate := false

	for _, tag := range tags {
		s := string(tag)
		if strings.HasPrefix(s, "private:") {
			scope := strings.TrimPrefix(s, "private:")
			if scope != "" && len(scope) > len(bestScope) {
				bestScope = scope
			}
			continue
		}
		if s == "private" {
			hasPrivate = true
		}
	}

	if bestScope != "" {
		logLabels.Printf("formatSecrecyLevel: resolved to private(%s) from tags=%v", bestScope, tags)
		return fmt.Sprintf("private (%s)", bestScope)
	}
	if hasPrivate {
		return "private"
	}
	return fmt.Sprintf("%v", tags)
}
