package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config/rules"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/version"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// embeddedSchemaBytes holds the bundled MCP Gateway configuration JSON Schema (v0.64.4).
// Embedding the schema in the binary eliminates the runtime network request that was
// previously needed to fetch it from GitHub, improving startup reliability and removing
// a potential point of failure in network-restricted environments.
//
// To update the schema to a newer version:
//  1. Download the new schema from https://github.com/github/gh-aw/releases
//  2. Replace internal/config/schema/mcp-gateway-config.schema.json
//  3. Update the version comment above
//  4. Run tests to ensure compatibility: make test
//
//go:embed schema/mcp-gateway-config.schema.json
var embeddedSchemaBytes []byte

const (
	// maxSchemaFetchRetries is the number of fetch attempts before giving up.
	maxSchemaFetchRetries = 3

	// maxSchemaFetchBytes bounds fetched schema size to avoid unbounded memory usage.
	maxSchemaFetchBytes = 10 * 1024 * 1024 // 10 MiB

	// embeddedSchemaID is the $id URL used when registering the embedded schema with
	// the JSON Schema compiler. It matches the $id field in the bundled schema file.
	embeddedSchemaID = "https://docs.github.com/gh-aw/schemas/mcp-gateway-config.schema.json"
)

// schemaFetchRetryDelay is the base delay between retry attempts using exponential
// backoff (1×, 2×, 4×, …). It is a variable so tests can override it to zero for
// fast execution.
var schemaFetchRetryDelay = time.Second

// schemaHTTPClientTimeout is the per-attempt HTTP request timeout. It is a variable
// so tests can shorten it to avoid long waits when testing timeout behaviour.
var schemaHTTPClientTimeout = 10 * time.Second

var (
	// Compile regex patterns from schema for additional validation
	containerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]*(:([a-zA-Z0-9._-]+|latest))?(@sha256:[a-fA-F0-9]{64})?$`)
	urlPattern       = regexp.MustCompile(`^https?://.+`)
	mountPattern     = regexp.MustCompile(`^[^:]+:[^:]+:(ro|rw)$`)
	domainVarPattern = regexp.MustCompile(`^\$\{[A-Z_][A-Z0-9_]*\}$`)

	// logSchema is the debug logger for schema validation
	logSchema = logger.New("config:validation_schema")

	// Schema caching to avoid recompiling the JSON schema on every validation
	// This improves performance by compiling the schema once and reusing it
	schemaOnce   sync.Once
	cachedSchema *jsonschema.Schema
	schemaErr    error
)

// fixSchemaBytes applies workarounds to a raw schema JSON for JSON Schema Draft 7 limitations.
//
// Background:
// The MCP Gateway configuration schema uses regex patterns with negative lookahead
// assertions (e.g., "(?!stdio|http)") to exclude specific values. However, JSON Schema
// Draft 7's pattern validation uses ECMA-262 regex syntax, which does not support
// negative lookahead in all implementations.
//
// Workaround Strategy:
// Instead of using pattern-based exclusions, we replace them with semantic equivalents:
//
//  1. For customServerConfig.type:
//     - Original: pattern: "^(?!stdio$|http$).*"
//     - Fixed: not: { enum: ["stdio", "http"] }
//     - This achieves the same validation goal using JSON Schema's "not" keyword
//
//  2. For customSchemas patternProperties:
//     - Original: "^(?!stdio$|http$)[a-z][a-z0-9-]*$"
//     - Fixed: "^[a-z][a-z0-9-]*$" (combined with oneOf constraint)
//     - The oneOf logic in the schema ensures stdio/http are validated separately
//
// These replacements maintain semantic equivalence while using only Draft 7 features.
//
// Future Consideration:
// TODO: Investigate if JSON Schema v6 (library upgrade) or Draft 2019-09+/2020-12
// (newer spec) eliminate this workaround. The jsonschema/v6 Go library may handle
// these patterns natively, potentially allowing removal of this function entirely.
func fixSchemaBytes(schemaBytes []byte) ([]byte, error) {
	fixStart := time.Now()
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Fix the customServerConfig pattern that uses negative lookahead
	// The oneOf constraint in mcpServerConfig will still ensure that stdio/http
	// types are validated correctly. We replace the pattern with an enum that excludes
	// stdio and http, which achieves the same validation goal without negative lookahead.
	if definitions, ok := schema["definitions"].(map[string]interface{}); ok {
		if customServerConfig, ok := definitions["customServerConfig"].(map[string]interface{}); ok {
			if properties, ok := customServerConfig["properties"].(map[string]interface{}); ok {
				typeValue, exists := properties["type"]
				if !exists {
					logSchema.Print("Skipped customServerConfig type fix: type field not found in customServerConfig properties")
				} else if typeField, ok := typeValue.(map[string]interface{}); ok {
					patternValue, hasPattern := typeField["pattern"]
					pattern, patternIsString := patternValue.(string)
					if !hasPattern {
						logSchema.Print("Skipped customServerConfig type fix: type field has no pattern to replace")
					} else if !patternIsString {
						logSchema.Print("Skipped customServerConfig type fix: type field pattern is not a string")
					} else if !strings.Contains(pattern, "(?!") {
						logSchema.Print("Skipped customServerConfig type fix: type field pattern does not use negative lookahead")
					} else {
						// Remove the pattern entirely - the oneOf logic combined with the fact
						// that stdioServerConfig has enum: ["stdio"] and httpServerConfig has
						// enum: ["http"] will ensure proper validation
						delete(typeField, "pattern")
						// Also remove the type constraint since we want it to only match in the oneOf context
						delete(typeField, "type")
						// Add a not constraint to exclude stdio and http
						typeField["not"] = map[string]interface{}{
							"enum": []string{"stdio", "http"},
						}
						logSchema.Print("Applied customServerConfig type fix: replaced negative-lookahead pattern with not-enum constraint")
					}
				} else {
					logSchema.Print("Skipped customServerConfig type fix: type field is not an object")
				}
			} else {
				logSchema.Print("Skipped customServerConfig type fix: customServerConfig properties is missing or not an object")
			}
		} else {
			logSchema.Print("Skipped customServerConfig type fix: customServerConfig definition is missing or not an object")
		}
	} else {
		logSchema.Print("Skipped customServerConfig type fix: schema definitions is missing or not an object")
	}

	// Fix the customSchemas patternProperties
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		if customSchemas, ok := properties["customSchemas"].(map[string]interface{}); ok {
			if patternProps, ok := customSchemas["patternProperties"].(map[string]interface{}); ok {
				// Find and replace the pattern property key with negative lookahead
				for key, value := range patternProps {
					if strings.Contains(key, "(?!") {
						// Replace with a simple pattern that matches any lowercase word
						// The validation logic will handle ensuring it's not stdio/http
						logSchema.Printf("Applied customSchemas patternProperties fix: replaced negative-lookahead key %q with ^[a-z][a-z0-9-]*$", key)
						delete(patternProps, key)
						patternProps["^[a-z][a-z0-9-]*$"] = value
						break
					}
				}
			}
		}
	}

	// Add registry and guard-policies fields to stdioServerConfig and httpServerConfig.
	// These are workarounds for fields supported by this gateway implementation that are
	// not present in the upstream schema:
	//   - registry: Spec Section 4.1.2 defines this as a valid optional field.
	//   - guard-policies: Actively used in this implementation for server-level access control.
	//     The upstream schema previously included this field and may add it back in a future version.
	if definitions, ok := schema["definitions"].(map[string]interface{}); ok {
		// Define the registry property schema
		registryProperty := map[string]interface{}{
			"type":        "string",
			"description": "URI to the installation location when MCP is installed from a registry. This is an informational field used for documentation and tooling discovery.",
		}

		// Define the guard-policies property schema
		guardPoliciesProperty := map[string]interface{}{
			"type":                 "object",
			"description":          "Guard policies for access control at the MCP gateway level. The structure of guard policies is server-specific.",
			"additionalProperties": true,
		}

		ensureProperty := func(props map[string]interface{}, key string, value map[string]interface{}) string {
			action := "Added"
			if _, exists := props[key]; exists {
				action = "Updated"
			}
			props[key] = value
			return action
		}

		// Add registry and guard-policies to stdioServerConfig
		if stdioConfig, ok := definitions["stdioServerConfig"].(map[string]interface{}); ok {
			if props, ok := stdioConfig["properties"].(map[string]interface{}); ok {
				registryAction := ensureProperty(props, "registry", registryProperty)
				guardPoliciesAction := ensureProperty(props, "guard-policies", guardPoliciesProperty)
				logSchema.Printf("%s registry and %s guard-policies fields in stdioServerConfig", registryAction, guardPoliciesAction)
			}
		}

		// Add registry and guard-policies to httpServerConfig
		if httpConfig, ok := definitions["httpServerConfig"].(map[string]interface{}); ok {
			if props, ok := httpConfig["properties"].(map[string]interface{}); ok {
				registryAction := ensureProperty(props, "registry", registryProperty)
				guardPoliciesAction := ensureProperty(props, "guard-policies", guardPoliciesProperty)
				logSchema.Printf("%s registry and %s guard-policies fields in httpServerConfig", registryAction, guardPoliciesAction)
			}
		}

		// Add trustedBots to gatewayConfig.
		// Spec §4.1.3.4: optional array of additional trusted bot identity strings.
		// This field is in the v1.9.0 spec (main) but not yet in the v0.62.2 released schema.
		if gatewayConfig, ok := definitions["gatewayConfig"].(map[string]interface{}); ok {
			if props, ok := gatewayConfig["properties"].(map[string]interface{}); ok {
				props["trustedBots"] = map[string]interface{}{
					"type":        "array",
					"description": "Additional GitHub bot identity strings passed to the gateway and merged with its built-in trusted identity list. This field is additive — it extends the internal list but cannot remove built-in entries.",
					"items": map[string]interface{}{
						"type":      "string",
						"minLength": 1,
						"pattern":   ".*\\S.*",
					},
					"minItems": 1,
				}

				// Add keepaliveInterval to gatewayConfig.
				// Spec §4.1.3.5: optional integer keepalive ping interval in seconds for HTTP
				// MCP backends. The Go struct (StdinGatewayConfig.KeepaliveInterval) supports
				// this field, but the embedded schema (v0.64.4) omits it, causing schema
				// validation to reject the field when additionalProperties is false.
				props["keepaliveInterval"] = map[string]interface{}{
					"type":        "integer",
					"description": "Keepalive ping interval in seconds for HTTP MCP backends. Use -1 to disable, 0 or unset for gateway default (1500s), or a positive integer for a custom interval.",
				}
				logSchema.Print("Added trustedBots and keepaliveInterval fields to gatewayConfig")
			}
		}
	}

	fixedBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fixed schema: %w", err)
	}
	logSchema.Printf("Schema fixes applied in %v", time.Since(fixStart))

	return fixedBytes, nil
}

// fetchAndFixSchema fetches the JSON schema from the remote URL and applies
// workarounds for JSON Schema Draft 7 limitations via fixSchemaBytes.
// This function is used for fetching custom server schemas from remote URLs.
func fetchAndFixSchema(url string) ([]byte, error) {
	startTime := time.Now()
	logSchema.Printf("Fetching schema from URL: %s", url)

	client := &http.Client{
		Timeout: schemaHTTPClientTimeout,
	}

	var resp *http.Response
	var lastErr error

	for attempt := 1; attempt <= maxSchemaFetchRetries; attempt++ {
		if attempt > 1 {
			delay := schemaFetchRetryDelay << uint(attempt-2) // 1×, 2×, 4× base delay
			logSchema.Printf("Retrying schema fetch (attempt %d/%d) after %v: %v", attempt, maxSchemaFetchRetries, delay, lastErr)
			time.Sleep(delay)
		}

		fetchStart := time.Now()
		var err error
		resp, err = client.Get(url)
		if err != nil {
			logSchema.Printf("Schema fetch attempt %d failed after %v: %v", attempt, time.Since(fetchStart), err)
			lastErr = fmt.Errorf("failed to fetch schema from %s: %w", url, err)
			resp = nil
			continue
		}
		logSchema.Printf("HTTP request attempt %d completed in %v with status %d", attempt, time.Since(fetchStart), resp.StatusCode)

		if resp.StatusCode == http.StatusOK {
			lastErr = nil
			break
		}

		if httputil.IsTransientHTTPError(resp.StatusCode) {
			lastErr = fmt.Errorf("failed to fetch schema: HTTP %d", resp.StatusCode)
			logSchema.Printf("Schema fetch attempt %d returned transient error: HTTP %d, will retry", attempt, resp.StatusCode)
			resp.Body.Close()
			resp = nil
			continue
		}

		// Permanent HTTP error (404, 403, 401, etc.) — do not retry.
		lastErr = fmt.Errorf("failed to fetch schema: HTTP %d", resp.StatusCode)
		logSchema.Printf("Schema fetch returned permanent error: HTTP %d", resp.StatusCode)
		resp.Body.Close()
		resp = nil
		break
	}

	if resp == nil {
		return nil, lastErr
	}
	defer resp.Body.Close()
	logSchema.Printf("HTTP request completed in %v", time.Since(startTime))

	readStart := time.Now()
	limitedBody := io.LimitReader(resp.Body, maxSchemaFetchBytes+1)
	schemaBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema response: %w", err)
	}
	if len(schemaBytes) > maxSchemaFetchBytes {
		return nil, fmt.Errorf("schema response too large: exceeds %d bytes", maxSchemaFetchBytes)
	}
	logSchema.Printf("Schema read completed in %v (size: %d bytes)", time.Since(readStart), len(schemaBytes))

	return fixSchemaBytes(schemaBytes)
}

// newDraft7Compiler creates a JSON Schema compiler configured for Draft 7.
func newDraft7Compiler() *jsonschema.Compiler {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	return compiler
}

// getOrCompileSchema retrieves the cached compiled schema or compiles it on first use.
// This function uses sync.Once to ensure thread-safe, one-time schema compilation,
// which significantly improves performance by avoiding repeated schema compilation
// on every validation call.
//
// The schema is bundled in the binary via go:embed and fixed on first call.
// If schema compilation fails, the error is also cached to avoid repeated attempts.
//
// Returns:
//   - Compiled JSON schema on success
//   - Error if schema compilation fails
func getOrCompileSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		logSchema.Print("Compiling JSON schema for the first time")

		// Apply fixes to the embedded schema bytes
		schemaJSON, fixErr := fixSchemaBytes(embeddedSchemaBytes)
		if fixErr != nil {
			schemaErr = fmt.Errorf("failed to process embedded schema: %w", fixErr)
			logSchema.Printf("Schema compilation failed: %v", schemaErr)
			return
		}

		// Parse the schema to extract its $id
		var schemaObj map[string]interface{}
		if parseErr := json.Unmarshal(schemaJSON, &schemaObj); parseErr != nil {
			schemaErr = fmt.Errorf("failed to parse schema JSON: %w", parseErr)
			return
		}

		schemaID, ok := schemaObj["$id"].(string)
		if !ok || schemaID == "" {
			schemaID = embeddedSchemaID
		}

		// Compile the schema
		compiler := newDraft7Compiler()

		// Add the schema using its $id URL so internal $ref references resolve correctly
		if addErr := compiler.AddResource(schemaID, strings.NewReader(string(schemaJSON))); addErr != nil {
			schemaErr = fmt.Errorf("failed to add schema resource: %w", addErr)
			return
		}

		cachedSchema, schemaErr = compiler.Compile(schemaID)
		if schemaErr != nil {
			schemaErr = fmt.Errorf("failed to compile schema: %w", schemaErr)
			logSchema.Printf("Schema compilation failed: %v", schemaErr)
			return
		}

		logSchema.Print("Schema compiled and cached successfully")
	})

	return cachedSchema, schemaErr
}

// validateJSONSchema validates the raw JSON configuration against the JSON schema
func validateJSONSchema(data []byte) error {
	startTime := time.Now()
	logSchema.Printf("Starting JSON schema validation: data_size=%d bytes", len(data))

	// Get the cached compiled schema (or compile it on first use)
	schemaStart := time.Now()
	schema, err := getOrCompileSchema()
	if err != nil {
		return err
	}
	logSchema.Printf("Schema compilation/retrieval took: %v", time.Since(schemaStart))

	// Parse the configuration
	parseStart := time.Now()
	var configObj interface{}
	if err := json.Unmarshal(data, &configObj); err != nil {
		return fmt.Errorf("failed to parse configuration JSON: %w", err)
	}
	logSchema.Printf("JSON parsing took: %v", time.Since(parseStart))

	// Validate the configuration
	validationStart := time.Now()
	if err := schema.Validate(configObj); err != nil {
		logSchema.Printf("Schema validation failed after %v: %v", time.Since(validationStart), err)
		return formatSchemaError(err)
	}
	logSchema.Printf("Schema validation took: %v", time.Since(validationStart))

	logSchema.Printf("Total validation completed successfully in %v", time.Since(startTime))
	return nil
}

// formatSchemaError formats JSON schema validation errors to be user-friendly
func formatSchemaError(err error) error {
	if err == nil {
		return nil
	}

	// The jsonschema library returns a ValidationError type with detailed info
	if ve, ok := err.(*jsonschema.ValidationError); ok {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Configuration validation error (MCP Gateway version: %s):\n\n", version.Get()))

		// Recursively format all errors
		formatValidationErrorRecursive(ve, &sb, 0)

		rules.AppendConfigDocsFooter(&sb)

		return fmt.Errorf("%s", sb.String())
	}

	return fmt.Errorf("configuration validation error (version: %s): %s", version.Get(), err.Error())
}

// formatValidationErrorRecursive recursively formats validation errors with proper indentation
func formatValidationErrorRecursive(ve *jsonschema.ValidationError, sb *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)

	// Format location and message
	location := ve.InstanceLocation
	if location == "" {
		location = "<root>"
	}
	fmt.Fprintf(sb, "%sLocation: %s\n", indent, location)
	fmt.Fprintf(sb, "%sError: %s\n", indent, ve.Message)

	// Add detailed context based on the error message
	context := formatErrorContext(ve, indent)
	if context != "" {
		sb.WriteString(context)
	}

	// Recursively process nested causes
	if len(ve.Causes) > 0 {
		for _, cause := range ve.Causes {
			formatValidationErrorRecursive(cause, sb, depth+1)
		}
	}

	// Add spacing between sibling errors at the same level
	if depth == 0 {
		sb.WriteString("\n")
	}
}

// formatErrorContext provides additional context about what caused the validation error
func formatErrorContext(ve *jsonschema.ValidationError, prefix string) string {
	var sb strings.Builder
	msg := ve.Message
	keyword := keywordFromLocation(ve.KeywordLocation)
	added := map[string]bool{}

	addDetail := func(key string, lines ...string) {
		if added[key] {
			return
		}
		for _, line := range lines {
			sb.WriteString(fmt.Sprintf("%s%s\n", prefix, line))
		}
		added[key] = true
	}

	switch keyword {
	case "additionalProperties":
		addDetail("additionalProperties",
			"Details: Configuration contains field(s) that are not defined in the schema",
			"  → Check for typos in field names or remove unsupported fields")
	case "type":
		addDetail("type",
			"Details: Type mismatch - the value type doesn't match what's expected",
			"  → Verify the value is the correct type (string, number, boolean, object, array)")
	case "enum":
		addDetail("enum",
			"Details: Invalid value - the field has a restricted set of allowed values",
			"  → Check the documentation for the list of valid values")
	case "required":
		addDetail("required",
			"Details: Required field(s) are missing",
			"  → Add the required field(s) to your configuration")
	case "pattern":
		addDetail("pattern",
			"Details: Value format is incorrect",
			"  → The value must match a specific format or pattern")
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
		addDetail("range",
			"Details: Value is outside the allowed range",
			"  → Adjust the value to be within the valid range")
	case "oneOf":
		addDetail("oneOf",
			"Details: Configuration doesn't match any of the expected formats",
			"  → Review the structure and ensure it matches one of the valid configuration types")
	}

	// For additional properties errors, explain what's wrong
	if strings.Contains(msg, "additionalProperties") || strings.Contains(msg, "additional property") {
		addDetail("additionalProperties",
			"Details: Configuration contains field(s) that are not defined in the schema",
			"  → Check for typos in field names or remove unsupported fields")
	}

	// For type errors, show the mismatch
	if strings.Contains(msg, "expected") && (strings.Contains(msg, "but got") || strings.Contains(msg, "type")) {
		addDetail("type",
			"Details: Type mismatch - the value type doesn't match what's expected",
			"  → Verify the value is the correct type (string, number, boolean, object, array)")
	}

	// For enum errors (invalid values from a set of allowed values)
	if strings.Contains(msg, "value must be one of") || strings.Contains(msg, "must be") {
		addDetail("enum",
			"Details: Invalid value - the field has a restricted set of allowed values",
			"  → Check the documentation for the list of valid values")
	}

	// For missing required properties
	if strings.Contains(msg, "missing properties") || strings.Contains(msg, "required") {
		addDetail("required",
			"Details: Required field(s) are missing",
			"  → Add the required field(s) to your configuration")
	}

	// For pattern validation failures (regex patterns)
	if strings.Contains(msg, "does not match pattern") || strings.Contains(msg, "pattern") {
		addDetail("pattern",
			"Details: Value format is incorrect",
			"  → The value must match a specific format or pattern")
	}

	// For minimum/maximum constraint violations
	if strings.Contains(msg, "must be >=") || strings.Contains(msg, "must be <=") || strings.Contains(msg, "minimum") || strings.Contains(msg, "maximum") {
		addDetail("range",
			"Details: Value is outside the allowed range",
			"  → Adjust the value to be within the valid range")
	}

	// For oneOf errors (typically type selection issues)
	if strings.Contains(msg, "doesn't validate with any of") || strings.Contains(msg, "oneOf") {
		addDetail("oneOf",
			"Details: Configuration doesn't match any of the expected formats",
			"  → Review the structure and ensure it matches one of the valid configuration types")
	}

	// Add keyword location if it provides useful context
	if ve.KeywordLocation != "" && ve.KeywordLocation != ve.InstanceLocation {
		sb.WriteString(fmt.Sprintf("%sSchema location: %s\n", prefix, ve.KeywordLocation))
	}

	return sb.String()
}

// keywordFromLocation extracts the terminal JSON Schema keyword from a
// keyword-location path (for example "/properties/foo/type" -> "type").
func keywordFromLocation(keywordLocation string) string {
	keywordLocation = strings.TrimSuffix(strings.TrimSpace(keywordLocation), "/")
	if keywordLocation == "" {
		return ""
	}
	lastSlash := strings.LastIndex(keywordLocation, "/")
	if lastSlash == -1 {
		return keywordLocation
	}
	return keywordLocation[lastSlash+1:]
}

// validateStringPatterns validates string fields against regex patterns from the schema
// This provides additional validation beyond the JSON schema validation
func validateStringPatterns(stdinCfg *StdinConfig) error {
	logSchema.Printf("Validating string patterns: server_count=%d", len(stdinCfg.MCPServers))

	// Validate server configurations
	for name, server := range stdinCfg.MCPServers {
		jsonPath := fmt.Sprintf("mcpServers.%s", name)
		logSchema.Printf("Validating server: name=%s, type=%s", name, server.Type)

		// Validate container pattern for stdio servers
		if server.Type == "" || server.Type == "stdio" || server.Type == "local" {
			if server.Container != "" && !containerPattern.MatchString(server.Container) {
				return rules.InvalidPattern("container", server.Container,
					fmt.Sprintf("%s.container", jsonPath),
					"Use a valid container image format (e.g., 'ghcr.io/owner/image:tag', 'owner/image:latest', or 'ghcr.io/owner/image:tag@sha256:<digest>')")
			}

			// Validate mount patterns
			for i, mount := range server.Mounts {
				if !mountPattern.MatchString(mount) {
					return rules.InvalidPattern("mounts", mount,
						fmt.Sprintf("%s.mounts[%d]", jsonPath, i),
						"Use format 'source:dest:mode' where mode is 'ro' or 'rw'")
				}
			}

			// Validate entrypoint is not empty if provided
			if server.Entrypoint != "" && len(strings.TrimSpace(server.Entrypoint)) == 0 {
				return rules.InvalidValue("entrypoint", "entrypoint cannot be empty or whitespace only",
					fmt.Sprintf("%s.entrypoint", jsonPath),
					"Provide a valid entrypoint path or remove the field")
			}
		}

		// Validate URL pattern for HTTP servers
		if server.Type == "http" {
			if server.URL != "" && !urlPattern.MatchString(server.URL) {
				return rules.InvalidPattern("url", server.URL,
					fmt.Sprintf("%s.url", jsonPath),
					"Use a valid HTTP or HTTPS URL (e.g., 'https://api.example.com/mcp')")
			}
		}
	}

	// Validate gateway configuration patterns
	if stdinCfg.Gateway != nil {
		// Delegate port, timeout, and payloadDir validation to validateGatewayConfig
		// to avoid duplicating those checks here.
		if err := validateGatewayConfig(stdinCfg.Gateway); err != nil {
			return err
		}

		// Validate domain: must be "localhost", "host.docker.internal", or variable expression
		if stdinCfg.Gateway.Domain != "" {
			domain := stdinCfg.Gateway.Domain
			if domain != "localhost" && domain != "host.docker.internal" && !domainVarPattern.MatchString(domain) {
				return rules.InvalidValue("domain",
					fmt.Sprintf("domain '%s' must be 'localhost', 'host.docker.internal', or a variable expression", domain),
					"gateway.domain",
					"Use 'localhost', 'host.docker.internal', or a variable like '${MCP_GATEWAY_DOMAIN}'")
			}
		}
	}

	return nil
}
