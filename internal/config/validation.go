package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/oidc"
	"github.com/itchyny/gojq"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

var logValidation = logger.New("config:validation")

// customSchemaCache stores compiled custom schemas by schema URL to avoid
// repeated fetch + compile work across validations.
var customSchemaCache sync.Map

// validateMounts validates mount specifications using centralized rules
func validateMounts(mounts []string, jsonPath string) error {
	for i, mount := range mounts {
		if err := MountFormat(mount, jsonPath, i); err != nil {
			return err
		}
	}
	return nil
}

// validateServerConfigWithCustomSchemas validates a server configuration with custom schema support
func validateServerConfigWithCustomSchemas(name string, server *StdinServerConfig, customSchemas map[string]interface{}) error {
	logValidation.Printf("Validating server config: name=%s, type=%s", name, server.Type)
	jsonPath := fmt.Sprintf("mcpServers.%s", name)

	// Validate type (empty defaults to stdio)
	if server.Type == "" {
		server.Type = "stdio"
		logValidation.Printf("Server type empty, defaulting to stdio: name=%s", name)
	}

	// Normalize "local" to "stdio"
	if server.Type == "local" {
		server.Type = "stdio"
		logValidation.Printf("Server type normalized from 'local' to 'stdio': name=%s", name)
	}

	// Check if it's a standard type
	if server.Type == "stdio" || server.Type == "http" {
		return validateStandardServerConfig(name, server, jsonPath)
	}

	// It's a custom type - validate against customSchemas
	return validateCustomServerConfig(name, server, customSchemas, jsonPath)
}

// validateStandardServerConfig validates stdio or http server configurations
func validateStandardServerConfig(name string, server *StdinServerConfig, jsonPath string) error {
	// For stdio servers, container is required
	if server.Type == "stdio" || server.Type == "local" {
		if server.Container == "" {
			logValidation.Printf("Validation failed: %s, name=%s, type=%s", "stdio server missing container field", name, server.Type)
			return MissingRequired("container", "stdio", jsonPath, "Add a 'container' field (e.g., \"ghcr.io/owner/image:tag\")")
		}

		// Validate mounts if provided
		if len(server.Mounts) > 0 {
			logValidation.Printf("Validating mounts for server: name=%s, mount_count=%d", name, len(server.Mounts))
			if err := validateMounts(server.Mounts, jsonPath); err != nil {
				return err
			}
		}

	}

	// For HTTP servers, url is required and mounts are not allowed
	if server.Type == "http" {
		if server.URL == "" {
			logValidation.Printf("Validation failed: %s, name=%s, type=%s", "HTTP server missing url field", name, server.Type)
			return MissingRequired("url", "HTTP", jsonPath, "Add a 'url' field (e.g., \"https://example.com/mcp\")")
		}
		if len(server.Mounts) > 0 {
			logValidation.Printf("Validation failed: %s, name=%s, type=%s", "HTTP server has mounts field", name, server.Type)
			return UnsupportedField("mounts", "mounts are only supported for stdio (containerized) servers", jsonPath, "Remove the 'mounts' field from HTTP server configuration; mounts only apply to stdio servers")
		}

	}

	// Validate per-server toolTimeout if provided and non-zero.
	// A value of 0 means "unset – fall back to the global gateway timeout".
	if server.ToolTimeout != nil && *server.ToolTimeout != 0 {
		toolTimeoutField := server.toolTimeoutField()
		if err := TimeoutMinimum(*server.ToolTimeout, ToolTimeoutMin, toolTimeoutField, jsonPath+"."+toolTimeoutField); err != nil {
			logValidation.Printf("Validation failed: %s, name=%s, type=%s", fmt.Sprintf("%s %d is below minimum %d", toolTimeoutField, *server.ToolTimeout, ToolTimeoutMin), name, server.Type)
			return err
		}
	}

	if err := validateCommonServerFields(name, server.Type, server.Auth, server.ToolResponseFilters, jsonPath); err != nil {
		return err
	}

	logValidation.Printf("Server config validation passed: name=%s, type=%s", name, server.Type)
	return nil
}

// validateCommonServerFields validates shared per-server fields used by both the
// TOML and JSON stdin configuration paths.
func validateCommonServerFields(name, serverType string, auth *AuthConfig, toolResponseFilters map[string]string, jsonPath string) error {
	if err := validateServerAuth(auth, serverType, name, jsonPath); err != nil {
		return err
	}
	return validateToolResponseFilters(toolResponseFilters, jsonPath+".tool_response_filters")
}

// validateToolResponseFilters validates tool response filters without permitting jq
// variables, preserving the historical runtime behavior by delegating with nil varNames.
func validateToolResponseFilters(filters map[string]string, jsonPath string) error {
	return validateToolResponseFiltersWithVars(filters, jsonPath, nil)
}

// validateToolResponseFiltersWithVars validates tool response filter expressions that
// reference named variables. varNames must match exactly the variable names that will
// be passed to CompileToolResponseFilterWithVars at runtime (e.g. []string{"$serverID"}).
//
// This must be called instead of validateToolResponseFilters whenever the runtime uses
// CompileToolResponseFilterWithVars, so that startup validation does not falsely reject
// filters that reference variables which are only bound at run time. A nil varNames
// slice preserves validateToolResponseFilters behavior by disallowing jq variables.
func validateToolResponseFiltersWithVars(filters map[string]string, jsonPath string, varNames []string) error {
	if len(filters) == 0 {
		return nil
	}

	for toolName, rawFilter := range filters {
		if strings.TrimSpace(toolName) == "" {
			return fmt.Errorf("%s contains an empty tool name", jsonPath)
		}
		filter := strings.TrimSpace(rawFilter)
		if filter == "" {
			return fmt.Errorf("%s.%s must not be empty", jsonPath, toolName)
		}

		query, err := gojq.Parse(filter)
		if err != nil {
			return fmt.Errorf("%s.%s contains an invalid jq expression: %w", jsonPath, toolName, err)
		}
		if _, err := gojq.Compile(query,
			gojq.WithEnvironLoader(func() []string { return nil }), // match runtime compile options (defense-in-depth)
			gojq.WithVariables(varNames),
		); err != nil {
			return fmt.Errorf("%s.%s contains an invalid jq expression: %w", jsonPath, toolName, err)
		}
	}

	return nil
}

// validateServerAuth validates the auth configuration on any server type,
// rejecting auth on non-HTTP servers and delegating to validateAuthConfig
// for HTTP servers. This is shared by both the TOML (LoadFromFile) and
// JSON stdin (validateStandardServerConfig) paths.
func validateServerAuth(auth *AuthConfig, serverType, name, jsonPath string) error {
	if auth == nil {
		return nil
	}
	if serverType != "http" {
		logValidation.Printf("Validation failed: %s, name=%s, type=%s", fmt.Sprintf("auth is set on non-HTTP server type: %s", serverType), name, serverType)
		return UnsupportedField(
			"auth",
			fmt.Sprintf("server type %q", serverType),
			jsonPath,
			"Remove the auth configuration or change the server type to \"http\"")
	}
	return validateAuthConfig(auth, name, jsonPath)
}

// validateAuthConfig validates the auth configuration for an HTTP server.
func validateAuthConfig(auth *AuthConfig, serverName, jsonPath string) error {
	authPath := jsonPath + ".auth"
	logValidation.Printf("Validating auth config: server=%s, type=%s", serverName, auth.Type)

	if auth.Type == "" {
		logValidation.Printf("Validation failed: %s, name=%s, type=%s", "auth.type is empty", serverName, "http")
		return MissingRequired("type", "auth", authPath, "Specify the authentication type (currently only \"github-oidc\" is supported)")
	}

	if auth.Type != "github-oidc" {
		logValidation.Printf("Validation failed: %s, name=%s, type=%s", fmt.Sprintf("unsupported auth.type: %s", auth.Type), serverName, "http")
		return UnsupportedType("type", auth.Type, authPath, fmt.Sprintf("Unsupported auth type %q. Currently only \"github-oidc\" is supported", auth.Type))
	}

	// Fail-fast: check that required OIDC environment variables are present.
	// This catches misconfigurations at config-load time rather than deferring
	// the error to the first request against this server.
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") == "" {
		logValidation.Printf("Validation failed: %s, name=%s, type=%s", "ACTIONS_ID_TOKEN_REQUEST_URL is not set", serverName, "http")
		return MissingRequired(
			"ACTIONS_ID_TOKEN_REQUEST_URL", "github-oidc", authPath,
			oidc.ErrMissingOIDCEnvVar(serverName).Error())
	}

	logValidation.Printf("Auth config validated: server=%s, type=%s", serverName, auth.Type)
	return nil
}

// validateCustomServerConfig validates custom server type configurations
func validateCustomServerConfig(name string, server *StdinServerConfig, customSchemas map[string]interface{}, jsonPath string) error {
	serverType := server.Type

	// Check if custom type is registered
	schemaValue, exists := customSchemas[serverType]
	if !exists {
		noCustomSchemasSuffix := ""
		if customSchemas == nil {
			noCustomSchemasSuffix = " (no customSchemas)"
		}
		logValidation.Printf("Custom type not registered: name=%s, type=%s%s", name, serverType, noCustomSchemasSuffix)
		return UnsupportedType("type", serverType, jsonPath, "Custom server type '"+serverType+"' is not registered in customSchemas. Add the custom type to the customSchemas field or use a standard type ('stdio' or 'http')")
	}

	// Convert schema value to string if possible
	schemaURL, ok := schemaValue.(string)
	if !ok {
		logValidation.Printf("Custom schema value is not a string: name=%s, type=%s", name, serverType)
		schemaURL = ""
	}

	logValidation.Printf("Custom type found in customSchemas: name=%s, type=%s, schemaURL=%s", name, serverType, schemaURL)

	// If schema URL is empty, skip validation
	if schemaURL == "" {
		logValidation.Printf("Custom schema URL is empty, skipping validation: name=%s, type=%s", name, serverType)
		return nil
	}

	// Fetch and validate against custom schema
	return validateAgainstCustomSchema(name, server, schemaURL, jsonPath)
}

// validateAgainstCustomSchema fetches and validates a server config against its custom schema
func validateAgainstCustomSchema(name string, server *StdinServerConfig, schemaURL string, jsonPath string) error {
	if cachedSchema, ok := customSchemaCache.Load(schemaURL); ok {
		if schema, ok := cachedSchema.(*jsonschema.Schema); ok {
			logValidation.Printf("Using cached custom schema: name=%s, url=%s", name, schemaURL)
			return validateServerAgainstSchema(name, server, schema, schemaURL, jsonPath)
		}
		logValidation.Printf("Ignoring cached custom schema with unexpected type: name=%s, url=%s", name, schemaURL)
	}

	logValidation.Printf("Fetching custom schema for validation: name=%s, url=%s", name, schemaURL)

	// Fetch the custom schema using the existing helper
	schemaJSON, err := fetchAndFixSchema(schemaURL)
	if err != nil {
		logValidation.Printf("Failed to fetch custom schema: name=%s, url=%s, error=%v", name, schemaURL, err)
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to fetch custom schema: %v", err),
			jsonPath,
			fmt.Sprintf("Ensure the schema URL '%s' is accessible and returns a valid JSON Schema", schemaURL))
	}

	logValidation.Printf("Custom schema fetched successfully: name=%s, size=%d bytes", name, len(schemaJSON))

	// Parse the schema to extract its $id
	var schemaObj map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schemaObj); err != nil {
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to parse custom schema: %v", err),
			jsonPath,
			fmt.Sprintf("The schema at '%s' must be valid JSON", schemaURL))
	}

	schemaID, ok := schemaObj["$id"].(string)
	if !ok || schemaID == "" {
		schemaID = schemaURL
	}

	// Compile the custom schema
	compiler := newDraft7Compiler()

	// Add the schema with both URLs (the fetch URL and the $id URL)
	if err := compiler.AddResource(schemaURL, strings.NewReader(string(schemaJSON))); err != nil {
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to compile custom schema: %v", err),
			jsonPath,
			fmt.Sprintf("The schema at '%s' must be a valid JSON Schema Draft 7 document", schemaURL))
	}
	if schemaID != schemaURL {
		if err := compiler.AddResource(schemaID, strings.NewReader(string(schemaJSON))); err != nil {
			return SchemaValidationError(server.Type,
				fmt.Sprintf("failed to compile custom schema with $id: %v", err),
				jsonPath,
				fmt.Sprintf("Check the $id field in the schema at '%s'", schemaURL))
		}
	}

	schema, err := compiler.Compile(schemaID)
	if err != nil {
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to compile custom schema: %v", err),
			jsonPath,
			fmt.Sprintf("The schema at '%s' must be a valid JSON Schema Draft 7 document", schemaURL))
	}

	logValidation.Printf("Custom schema compiled successfully: name=%s", name)
	customSchemaCache.Store(schemaURL, schema)

	return validateServerAgainstSchema(name, server, schema, schemaURL, jsonPath)
}

// validateServerAgainstSchema validates a server config map (including additional
// properties) against a compiled custom schema.
func validateServerAgainstSchema(name string, server *StdinServerConfig, schema *jsonschema.Schema, schemaURL string, jsonPath string) error {

	// Convert server config to a map that includes both struct fields and additional properties
	// This ensures custom fields are validated against the custom schema
	serverMap := make(map[string]interface{})

	// Marshal the struct to JSON first
	serverJSON, err := json.Marshal(server)
	if err != nil {
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to marshal server config for validation: %v", err),
			jsonPath, "Internal error - please report this issue")
	}

	// Unmarshal to map to get struct fields
	if err := json.Unmarshal(serverJSON, &serverMap); err != nil {
		return SchemaValidationError(server.Type,
			fmt.Sprintf("failed to unmarshal server config for validation: %v", err),
			jsonPath, "Internal error - please report this issue")
	}

	// Merge additional properties (custom fields) into the map
	for key, value := range server.AdditionalProperties {
		serverMap[key] = value
	}

	// Validate the merged map against the custom schema
	if err := schema.Validate(serverMap); err != nil {
		logValidation.Printf("Custom schema validation failed: name=%s, error=%v", name, err)
		return SchemaValidationError(server.Type,
			fmt.Sprintf("server configuration does not match custom schema: %v", err),
			jsonPath,
			fmt.Sprintf("Update the server configuration to match the schema requirements at '%s'", schemaURL))
	}

	logValidation.Printf("Custom schema validation passed: name=%s, type=%s", name, server.Type)
	return nil
}

// validateCustomSchemas validates the customSchemas field
func validateCustomSchemas(customSchemas map[string]interface{}) error {
	if customSchemas == nil {
		return nil
	}

	logValidation.Printf("Validating customSchemas: count=%d", len(customSchemas))

	for typeName, schemaValue := range customSchemas {
		// Check for reserved type names
		if typeName == "stdio" || typeName == "http" {
			logValidation.Printf("Reserved type name in customSchemas: %s", typeName)
			return UnsupportedType("customSchemas", typeName, fmt.Sprintf("customSchemas.%s", typeName), "Custom type name '"+typeName+"' conflicts with reserved type. Use a different name for your custom type (reserved types: stdio, http)")
		}
		// Enforce HTTPS-only for non-empty schema URLs (spec section 4.1.4)
		if schemaURL, ok := schemaValue.(string); ok && schemaURL != "" {
			if !strings.HasPrefix(schemaURL, "https://") {
				logValidation.Printf("Non-HTTPS schema URL in customSchemas: typeName=%s, url=%s", typeName, schemaURL)
				return InvalidValue("customSchemas."+typeName,
					fmt.Sprintf("custom schema URL must use HTTPS, got '%s'", schemaURL),
					"customSchemas."+typeName,
					"Use an HTTPS URL for the custom schema (e.g., 'https://example.com/schema.json')")
			}
		}
	}

	logValidation.Printf("customSchemas validation passed")
	return nil
}

// validateGatewayConfig validates gateway configuration
func validateGatewayConfig(gateway *StdinGatewayConfig) error {
	if gateway == nil {
		logValidation.Print("No gateway config to validate")
		return nil
	}

	logValidation.Print("Validating gateway configuration")

	// Validate port range using centralized rules
	if gateway.Port != nil {
		logValidation.Printf("Validating gateway port: %d", *gateway.Port)
		if err := PortRange(*gateway.Port, "gateway.port"); err != nil {
			return err
		}
	}

	// Validate timeout values using centralized rules
	if gateway.StartupTimeout != nil {
		logValidation.Printf("Validating startup timeout: %d", *gateway.StartupTimeout)
		if err := TimeoutPositive(*gateway.StartupTimeout, "startupTimeout", "gateway.startupTimeout"); err != nil {
			return err
		}
	}

	if gateway.ToolTimeout != nil {
		logValidation.Printf("Validating tool timeout: %d", *gateway.ToolTimeout)
		if err := TimeoutMinimum(*gateway.ToolTimeout, ToolTimeoutMin, "toolTimeout", "gateway.toolTimeout"); err != nil {
			return err
		}
	}

	// Validate payloadDir if provided (per schema: must be absolute path)
	if gateway.PayloadDir != "" {
		logValidation.Printf("Validating payload directory: %s", gateway.PayloadDir)
		if err := AbsolutePath(gateway.PayloadDir, "payloadDir", "gateway.payloadDir"); err != nil {
			return err
		}
	}

	// Validate payloadSizeThreshold per spec §4.1.3.3: must be a positive integer when present.
	if gateway.PayloadSizeThreshold != nil {
		if err := validateGatewayPayloadSizeThreshold(*gateway.PayloadSizeThreshold, "payloadSizeThreshold", "gateway.payloadSizeThreshold"); err != nil {
			return err
		}
	}

	// Validate trustedBots per spec §4.1.3.4: must be non-empty array when present
	if err := validateTrustedBots(gateway.TrustedBots); err != nil {
		return err
	}

	// Validate OpenTelemetry config per spec §4.1.3.6 when present
	if gateway.OpenTelemetry != nil {
		tracingCfg := &TracingConfig{
			Endpoint:    gateway.OpenTelemetry.Endpoint,
			Headers:     gateway.OpenTelemetry.Headers,
			TraceID:     gateway.OpenTelemetry.TraceID,
			SpanID:      gateway.OpenTelemetry.SpanID,
			ServiceName: gateway.OpenTelemetry.ServiceName,
		}
		if err := validateOpenTelemetryConfig(tracingCfg, true); err != nil {
			return err
		}
	}

	logValidation.Print("Gateway config validation passed")
	return nil
}

func validateGatewayPayloadSizeThreshold(value int, fieldName, jsonPath string) error {
	if ve := PositiveInteger(value, fieldName, jsonPath); ve != nil {
		return ve
	}
	return nil
}

// validateTrustedBots checks that the trusted_bots/trustedBots list conforms to spec §4.1.3.4:
// when present, it must be a non-empty array of non-empty strings.
func validateTrustedBots(bots []string) error {
	if bots == nil {
		return nil
	}
	if len(bots) == 0 {
		return fmt.Errorf("trusted_bots must be a non-empty array when present (spec §4.1.3.4)")
	}
	for i, bot := range bots {
		if strings.TrimSpace(bot) == "" {
			return fmt.Errorf("trusted_bots[%d] must be a non-empty string", i)
		}
	}
	return nil
}

// validateTOMLStdioContainerization validates that TOML stdio servers use Docker for containerization.
// This enforces MCP Gateway Specification Section 3.2.1: "Stdio-based MCP servers MUST be containerized."
func validateTOMLStdioContainerization(servers map[string]*ServerConfig) error {
	logValidation.Print("Validating TOML stdio server containerization requirement")

	for name, cfg := range servers {
		// Only validate stdio servers (or empty type which defaults to stdio)
		if cfg.Type == "" || cfg.Type == "stdio" || cfg.Type == "local" {
			logValidation.Printf("Checking stdio server: name=%s, command=%s", name, cfg.Command)

			// Check if command is Docker
			if cfg.Command != "docker" {
				logValidation.Printf("Validation failed: %s, name=%s, type=%s", fmt.Sprintf("stdio server using non-Docker command, command=%s", cfg.Command), name, "stdio")
				return fmt.Errorf(
					"server '%s': stdio servers must use containerized execution (command must be 'docker', got '%s'). "+
						"This is required by MCP Gateway Specification Section 3.2.1 (Containerization Requirement). "+
						"See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/mcp-gateway.md#321-containerization-requirement",
					name, cfg.Command)
			}
		}
	}

	logValidation.Print("TOML stdio containerization validation passed")
	return nil
}

// validateGuardPolicies validates all per-server guard policies in the config.
// It iterates over cfg.Guards and calls ValidateGuardPolicy for each non-nil policy.
func validateGuardPolicies(cfg *Config) error {
	logValidation.Printf("Validating guard policies: count=%d", len(cfg.Guards))
	for name, guardCfg := range cfg.Guards {
		if guardCfg != nil && guardCfg.Policy != nil {
			if err := ValidateGuardPolicy(guardCfg.Policy); err != nil {
				return fmt.Errorf("invalid policy for guard '%s': %w", name, err)
			}
		}
	}
	return nil
}
