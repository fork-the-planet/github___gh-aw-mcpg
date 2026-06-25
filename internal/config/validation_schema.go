package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/version"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// embeddedSchemaBytes holds the bundled MCP Gateway configuration JSON Schema.
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

// schemaErrPrinter is used to format localized error messages from ValidationError kinds.
var schemaErrPrinter = message.NewPrinter(language.English)

var (
	// Compile regex patterns from schema for additional validation
	containerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]*(:([a-zA-Z0-9._-]+|latest))?(@sha256:[a-fA-F0-9]{64})?$`)
	urlPattern       = regexp.MustCompile(`^https?://.+`)
	mountPattern     = regexp.MustCompile(`^[^:]+:[^:]+:(ro|rw)$`)
	domainVarPattern = regexp.MustCompile(`^\$\{[A-Z_][A-Z0-9_]*\}$`)
	// domainHostnamePattern matches a single RFC-1123 hostname label (no dots), e.g.
	// "awmg-mcpg" or "localhost". This allows network-isolation topology hostnames
	// that resolve from ${MCP_GATEWAY_DOMAIN} or are passed directly.
	domainHostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

	// logSchema is the debug logger for schema validation
	logSchema = logger.New("config:validation_schema")

	// Schema caching to avoid recompiling the JSON schema on every validation
	// This improves performance by compiling the schema once and reusing it
	schemaOnce   sync.Once
	cachedSchema *jsonschema.Schema
	schemaErr    error
)

// fetchSchema fetches the JSON schema bytes from the remote URL with retry logic.
// This function is used for fetching custom server schemas from remote URLs.
func fetchSchema(url string) ([]byte, error) {
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

	return schemaBytes, nil
}

// newCompiler creates a JSON Schema compiler using the library defaults (Draft 2020-12).
func newCompiler() *jsonschema.Compiler {
	return jsonschema.NewCompiler()
}

// getOrCompileSchema retrieves the cached compiled schema or compiles it on first use.
// This function uses sync.Once to ensure thread-safe, one-time schema compilation,
// which significantly improves performance by avoiding repeated schema compilation
// on every validation call.
//
// The schema is bundled in the binary via go:embed and read on first call.
// If schema compilation fails, the error is also cached to avoid repeated attempts.
//
// Returns:
//   - Compiled JSON schema on success
//   - Error if schema compilation fails
func getOrCompileSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		logSchema.Print("Compiling JSON schema for the first time")

		// Parse the embedded schema bytes into a JSON document
		schemaDoc, parseErr := jsonschema.UnmarshalJSON(bytes.NewReader(embeddedSchemaBytes))
		if parseErr != nil {
			schemaErr = fmt.Errorf("failed to process embedded schema: %w", parseErr)
			logSchema.Printf("Schema compilation failed: %v", schemaErr)
			return
		}

		// Extract $id from the parsed document to use as the schema URL
		schemaID := embeddedSchemaID
		if schemaObj, ok := schemaDoc.(map[string]any); ok {
			if id, ok := schemaObj["$id"].(string); ok && id != "" {
				schemaID = id
			}
		}

		// Compile the schema
		compiler := newCompiler()

		// Add the schema using its $id URL so internal $ref references resolve correctly
		if addErr := compiler.AddResource(schemaID, schemaDoc); addErr != nil {
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

	// Parse the configuration using jsonschema.UnmarshalJSON for proper number handling
	parseStart := time.Now()
	configObj, parseErr := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if parseErr != nil {
		return fmt.Errorf("failed to parse configuration JSON: %w", parseErr)
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
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Configuration validation error (MCP Gateway version: %s):\n\n", version.Get()))

		// Recursively format all errors
		formatValidationErrorRecursive(ve, &sb, 0)

		AppendConfigDocsFooter(&sb)

		return fmt.Errorf("%s", sb.String())
	}

	return fmt.Errorf("configuration validation error (version: %s): %s", version.Get(), err.Error())
}

// formatValidationErrorRecursive recursively formats validation errors with proper indentation
func formatValidationErrorRecursive(ve *jsonschema.ValidationError, sb *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)

	// Format location and message
	location := strings.Join(ve.InstanceLocation, "/")
	if location == "" {
		location = "<root>"
	}
	fmt.Fprintf(sb, "%sLocation: %s\n", indent, location)
	fmt.Fprintf(sb, "%sError: %s\n", indent, ve.ErrorKind.LocalizedString(schemaErrPrinter))

	// Add detailed context based on the error kind
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

// detailForKeyword returns the addDetail arguments (key and detail lines) for a
// recognised JSON Schema keyword. Returns "", nil for unknown keywords.
// This is used by formatErrorContext to centralise the guidance text so it is
// only written in one place and always stays in sync between the keyword-location
// switch and the message-fallback checks.
func detailForKeyword(keyword string) (string, []string) {
	switch keyword {
	case "additionalProperties":
		return "additionalProperties", []string{
			"Details: Configuration contains field(s) that are not defined in the schema",
			"  → Check for typos in field names or remove unsupported fields",
		}
	case "type":
		return "type", []string{
			"Details: Type mismatch - the value type doesn't match what's expected",
			"  → Verify the value is the correct type (string, number, boolean, object, array)",
		}
	case "enum":
		return "enum", []string{
			"Details: Invalid value - the field has a restricted set of allowed values",
			"  → Check the documentation for the list of valid values",
		}
	case "required":
		return "required", []string{
			"Details: Required field(s) are missing",
			"  → Add the required field(s) to your configuration",
		}
	case "pattern":
		return "pattern", []string{
			"Details: Value format is incorrect",
			"  → The value must match a specific format or pattern",
		}
	case "range":
		return "range", []string{
			"Details: Value is outside the allowed range",
			"  → Adjust the value to be within the valid range",
		}
	case "oneOf":
		return "oneOf", []string{
			"Details: Configuration doesn't match any of the expected formats",
			"  → Review the structure and ensure it matches one of the valid configuration types",
		}
	}
	return "", nil
}

// formatErrorContext provides additional context about what caused the validation error.
// It uses type-switching on ErrorKind for robust, version-stable keyword dispatch.
func formatErrorContext(ve *jsonschema.ValidationError, prefix string) string {
	var sb strings.Builder
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

	addFromKeyword := func(keyword string) {
		if key, lines := detailForKeyword(keyword); key != "" {
			addDetail(key, lines...)
		}
	}

	// Dispatch on ErrorKind type for robust keyword identification.
	switch ve.ErrorKind.(type) {
	case *kind.AdditionalProperties, *kind.AdditionalItems:
		addFromKeyword("additionalProperties")
	case *kind.Type:
		addFromKeyword("type")
	case *kind.Enum, *kind.Const:
		addFromKeyword("enum")
	case *kind.Required, *kind.DependentRequired:
		addFromKeyword("required")
	case *kind.Pattern:
		addFromKeyword("pattern")
	case *kind.OneOf, *kind.AnyOf:
		addFromKeyword("oneOf")
	case *kind.Minimum, *kind.Maximum, *kind.ExclusiveMinimum, *kind.ExclusiveMaximum,
		*kind.MinLength, *kind.MaxLength, *kind.MinItems, *kind.MaxItems,
		*kind.MinProperties, *kind.MaxProperties:
		addFromKeyword("range")
	}

	return sb.String()
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
		if IsStdioServerType(server.Type) {
			if server.Container != "" && !containerPattern.MatchString(server.Container) {
				return InvalidPattern("container", server.Container,
					fmt.Sprintf("%s.container", jsonPath),
					"Use a valid container image format (e.g., 'ghcr.io/owner/image:tag', 'owner/image:latest', or 'ghcr.io/owner/image:tag@sha256:<digest>')")
			}

			// Validate mount patterns
			for i, mount := range server.Mounts {
				if !mountPattern.MatchString(mount) {
					return InvalidPattern("mounts", mount,
						fmt.Sprintf("%s.mounts[%d]", jsonPath, i),
						"Use format 'source:dest:mode' where mode is 'ro' or 'rw'")
				}
			}

			// Validate entrypoint is not empty if provided
			if server.Entrypoint != "" && len(strings.TrimSpace(server.Entrypoint)) == 0 {
				return InvalidValue("entrypoint", "entrypoint cannot be empty or whitespace only",
					fmt.Sprintf("%s.entrypoint", jsonPath),
					"Provide a valid entrypoint path or remove the field")
			}
		}

		// Validate URL pattern for HTTP servers
		if server.Type == "http" {
			if server.URL != "" && !urlPattern.MatchString(server.URL) {
				return InvalidPattern("url", server.URL,
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

		// Validate domain: must be "localhost", "host.docker.internal", an RFC-1123
		// single-label hostname (e.g. "awmg-mcpg" for network-isolation topology), or
		// a variable expression like "${MCP_GATEWAY_DOMAIN}".
		// Note: this validation runs post-expansion, so resolved env values must also pass.
		if stdinCfg.Gateway.Domain != "" {
			domain := stdinCfg.Gateway.Domain
			if domain != "localhost" && domain != "host.docker.internal" &&
				!domainVarPattern.MatchString(domain) && !domainHostnamePattern.MatchString(domain) {
				return InvalidValue("domain",
					fmt.Sprintf("domain '%s' must be 'localhost', 'host.docker.internal', an RFC-1123 hostname label (e.g. 'awmg-mcpg'), or a variable expression", domain),
					"gateway.domain",
					"Use 'localhost', 'host.docker.internal', a topology hostname like 'awmg-mcpg', or a variable like '${MCP_GATEWAY_DOMAIN}'")
			}
		}
	}

	return nil
}
