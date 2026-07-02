package config

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	containerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]*(:([a-zA-Z0-9._-]+|latest))?(@sha256:[a-fA-F0-9]{64})?$`)
	urlPattern       = regexp.MustCompile(`^https?://.+`)
	domainVarPattern = regexp.MustCompile(`^\$\{[A-Z_][A-Z0-9_]*\}$`)
	// domainHostnamePattern matches a single RFC-1123 hostname label (no dots), e.g.
	// "awmg-mcpg" or "localhost". This allows network-isolation topology hostnames
	// that resolve from ${MCP_GATEWAY_DOMAIN} or are passed directly.
	domainHostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// PortRange validates that a port is in the valid range (1-65535)
// Returns nil if valid, *ValidationError if invalid
func PortRange(port int, jsonPath string) *ValidationError {
	logValidation.Printf("Validating port range: port=%d, jsonPath=%s", port, jsonPath)
	if port < 1 || port > 65535 {
		return newValidationError(
			fmt.Sprintf("Port validation failed: port=%d out of range", port),
			"port",
			fmt.Sprintf("port must be between 1 and 65535, got %d", port),
			jsonPath,
			"Use a valid port number (e.g., 8080)",
		)
	}
	return nil
}

// TimeoutPositive validates that a timeout value is at least 1.
// Returns nil if valid, *ValidationError if invalid.
// It delegates validation to TimeoutMinimum with min=1, then overrides the
// Suggestion with a more specific message: "Use a positive number of seconds (e.g., 30)".
func TimeoutPositive(timeout int, fieldName, jsonPath string) *ValidationError {
	if err := TimeoutMinimum(timeout, 1, fieldName, jsonPath); err != nil {
		err.Suggestion = "Use a positive number of seconds (e.g., 30)"
		return err
	}
	return nil
}

// PositiveInteger validates that a value is at least 1.
// Returns nil if valid, *ValidationError if invalid.
// It delegates to TimeoutMinimum with min=1, then overrides Message and
// Suggestion to use "positive integer" phrasing instead of the generic
// "must be at least N" wording produced by TimeoutMinimum.
func PositiveInteger(value int, fieldName, jsonPath string) *ValidationError {
	logValidation.Printf("Validating positive integer: field=%s, value=%d, jsonPath=%s", fieldName, value, jsonPath)
	if err := TimeoutMinimum(value, 1, fieldName, jsonPath); err != nil {
		logValidation.Printf("Positive integer validation failed: %s=%d is not positive", fieldName, value)
		err.Message = fmt.Sprintf("%s must be a positive integer (>= 1), got %d", fieldName, value)
		err.Suggestion = fmt.Sprintf("Use a positive integer (>= 1) for %s", fieldName)
		return err
	}
	return nil
}

// TimeoutMinimum validates that a timeout value is at least min.
// Returns nil if valid, *ValidationError if below the minimum.
func TimeoutMinimum(timeout, min int, fieldName, jsonPath string) *ValidationError {
	logValidation.Printf("Validating timeout minimum: field=%s, value=%d, min=%d, jsonPath=%s", fieldName, timeout, min, jsonPath)
	if timeout < min {
		return newValidationError(
			fmt.Sprintf("Timeout minimum validation failed: %s=%d is below minimum %d", fieldName, timeout, min),
			fieldName,
			fmt.Sprintf("%s must be at least %d, got %d", fieldName, min, timeout),
			jsonPath,
			fmt.Sprintf("Use a value of at least %d seconds", min),
		)
	}
	return nil
}

// TimeoutRange validates that a timeout value is within [min, max] (inclusive).
// Returns nil if valid, *ValidationError if outside the range.
func TimeoutRange(timeout, min, max int, fieldName, jsonPath string) *ValidationError {
	logValidation.Printf("Validating timeout range: field=%s, value=%d, min=%d, max=%d, jsonPath=%s", fieldName, timeout, min, max, jsonPath)
	if timeout < min || timeout > max {
		suggestedTimeout := min + (max-min)/2
		return newValidationError(
			fmt.Sprintf("Timeout range validation failed: %s=%d is outside [%d, %d]", fieldName, timeout, min, max),
			fieldName,
			fmt.Sprintf("%s must be between %d and %d, got %d", fieldName, min, max, timeout),
			jsonPath,
			fmt.Sprintf("Use a value between %d and %d seconds (e.g., %d)", min, max, suggestedTimeout),
		)
	}
	return nil
}

func mountValidationError(jsonPath string, index int, message, suggestion string) *ValidationError {
	return &ValidationError{
		Field:      "mounts",
		Message:    message,
		JSONPath:   fmt.Sprintf("%s.mounts[%d]", jsonPath, index),
		Suggestion: suggestion,
	}
}

// MountFormat validates a mount specification in the format "source:dest:mode"
// Returns nil if valid, *ValidationError if invalid
// Per MCP Gateway specification v1.8.0 section 4.1.5:
// - Host path MUST be an absolute path
// - Container path MUST be an absolute path
// - Mode MUST be either "ro" (read-only) or "rw" (read-write)
func MountFormat(mount, jsonPath string, index int) *ValidationError {
	logValidation.Printf("Validating mount format: mount=%s, jsonPath=%s, index=%d", mount, jsonPath, index)
	parts := strings.Split(mount, ":")
	if len(parts) != 3 {
		logValidation.Printf("Mount format validation failed: invalid part count=%d", len(parts))
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("invalid mount format '%s' (expected 'source:dest:mode')", mount),
			"Use format 'source:dest:mode' where mode is 'ro' (read-only) or 'rw' (read-write), e.g. '/host/path:/container/path:ro'",
		)
	}

	source := parts[0]
	dest := parts[1]
	mode := parts[2]

	// Validate source is not empty
	if source == "" {
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("mount source cannot be empty in '%s'", mount),
			"Provide a valid absolute source path (e.g., '/host/path')",
		)
	}

	// Validate source is an absolute path (MCP spec requirement)
	if !strings.HasPrefix(source, "/") {
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("mount source must be an absolute path, got '%s'", source),
			"Use an absolute path starting with '/' (e.g., '/var/data' instead of 'data')",
		)
	}

	// Validate dest is not empty
	if dest == "" {
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("mount destination cannot be empty in '%s'", mount),
			"Provide a valid absolute destination path (e.g., '/app/data')",
		)
	}

	// Validate dest is an absolute path (MCP spec requirement)
	if !strings.HasPrefix(dest, "/") {
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("mount destination must be an absolute path, got '%s'", dest),
			"Use an absolute path starting with '/' (e.g., '/app/data' instead of 'app/data')",
		)
	}

	// Validate mode
	if mode != "ro" && mode != "rw" {
		return mountValidationError(jsonPath, index,
			fmt.Sprintf("invalid mount mode '%s' (must be 'ro' or 'rw')", mode),
			"Use 'ro' for read-only or 'rw' for read-write",
		)
	}

	return nil
}

// RequiredStringField validates that a required string field is not empty.
// It returns a *ValidationError with structured context (Field, JSONPath,
// Suggestion) so that callers get consistent, machine-readable validation
// errors instead of plain fmt.Errorf strings.
// Returns nil if the value is non-empty.
func RequiredStringField(value, fieldName, jsonPath, suggestion string) *ValidationError {
	if value == "" {
		return &ValidationError{
			Field:      fieldName,
			Message:    fmt.Sprintf("%s is required", fieldName),
			JSONPath:   jsonPath,
			Suggestion: suggestion,
		}
	}
	return nil
}

// NonEmptyString validates that a string field is not empty (minLength: 1)
// Returns nil if valid, *ValidationError if invalid
func NonEmptyString(value, fieldName, jsonPath string) *ValidationError {
	if value == "" {
		return &ValidationError{
			Field:      fieldName,
			Message:    fmt.Sprintf("%s cannot be empty", fieldName),
			JSONPath:   jsonPath,
			Suggestion: fmt.Sprintf("Provide a non-empty value for %s", fieldName),
		}
	}
	return nil
}

// AbsolutePath validates that a directory path is an absolute path
// Per MCP Gateway schema: Unix paths start with '/', Windows paths start with a drive letter followed by ':\'
// Pattern: ^(/|[A-Za-z]:\\)
// Returns nil if valid, *ValidationError if invalid
func AbsolutePath(value, fieldName, jsonPath string) *ValidationError {
	logValidation.Printf("Validating absolute path: field=%s, value=%s, jsonPath=%s", fieldName, value, jsonPath)
	if value == "" {
		return newValidationError(
			fmt.Sprintf("Absolute path validation failed: %s is empty", fieldName),
			fieldName,
			fmt.Sprintf("%s cannot be empty", fieldName),
			jsonPath,
			fmt.Sprintf("Provide an absolute path for %s", fieldName),
		)
	}

	// Check for Unix absolute path (starts with /)
	if strings.HasPrefix(value, "/") {
		logValidation.Printf("Valid Unix absolute path: %s", value)
		return nil
	}

	// Check for Windows absolute path (drive letter followed by :\)
	// Pattern: [A-Za-z]:\\
	if len(value) >= 3 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':' && value[2] == '\\' {
		logValidation.Printf("Valid Windows absolute path: %s", value)
		return nil
	}

	return newValidationError(
		fmt.Sprintf("Absolute path validation failed: %s=%s is not absolute", fieldName, value),
		fieldName,
		fmt.Sprintf("%s must be an absolute path, got '%s'", fieldName, value),
		jsonPath,
		"Use an absolute path: Unix paths start with '/' (e.g., '/tmp/payloads'), Windows paths start with a drive letter (e.g., 'C:\\payloads')",
	)
}
