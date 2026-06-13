package config

import (
	"fmt"
	"os"
	"regexp"
)

// Variable expression pattern: ${VARIABLE_NAME}
var varExprPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandVariablesCore is the shared implementation for variable expansion.
// It works with byte slices and handles the core expansion logic, tracking undefined variables.
// This eliminates code duplication between expandVariables and ExpandRawJSONVariables.
// It returns the expanded bytes, a slice of undefined variable names, and an error
// (currently always nil).
func expandVariablesCore(data []byte, contextDesc string) ([]byte, []string, error) {
	logValidation.Printf("Expanding variables: context=%s", contextDesc)
	var undefinedVars []string

	result := varExprPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		// Extract variable name (remove ${ and })
		varName := string(match[2 : len(match)-1])

		if envValue, exists := os.LookupEnv(varName); exists {
			logValidation.Printf("Expanded variable: %s (found in environment)", varName)
			return []byte(envValue)
		}

		// Track undefined variable
		undefinedVars = append(undefinedVars, varName)
		logValidation.Printf("Undefined variable: %s", varName)
		return match // Keep original if undefined
	})

	logValidation.Printf("Variable expansion completed: context=%s, undefined_count=%d", contextDesc, len(undefinedVars))
	return result, undefinedVars, nil
}

// expandVariables expands variable expressions in a string.
// value is the source string and jsonPath identifies the config location for errors.
// It returns the expanded string and an error if any variable is undefined.
func expandVariables(value, jsonPath string) (string, error) {
	result, undefinedVars, _ := expandVariablesCore([]byte(value), fmt.Sprintf("jsonPath=%s", jsonPath))

	if len(undefinedVars) > 0 {
		logValidation.Printf("Variable expansion failed: undefined variables=%v", undefinedVars)
		return "", UndefinedVariable(undefinedVars[0], jsonPath)
	}

	return string(result), nil
}

// ExpandRawJSONVariables expands all ${VAR} expressions in JSON data before schema validation.
// This ensures the schema validates the expanded values, not the variable syntax.
// It collects undefined variables and reports the first undefined variable as an error.
func ExpandRawJSONVariables(data []byte) ([]byte, error) {
	result, undefinedVars, _ := expandVariablesCore(data, "raw JSON data")

	if len(undefinedVars) > 0 {
		logValidation.Printf("Variable expansion failed: undefined variables=%v", undefinedVars)
		return nil, UndefinedVariable(undefinedVars[0], "configuration")
	}

	return result, nil
}

// expandEnvVariables expands all variable expressions in an env map.
// env is the map to expand and serverName is used for config-path error context.
// It returns a new map with expanded values or an error if any variable is undefined.
func expandEnvVariables(env map[string]string, serverName string) (map[string]string, error) {
	logValidation.Printf("Expanding env variables for server: %s, count=%d", serverName, len(env))
	result := make(map[string]string, len(env))

	for key, value := range env {
		jsonPath := fmt.Sprintf("mcpServers.%s.env.%s", serverName, key)

		expanded, err := expandVariables(value, jsonPath)
		if err != nil {
			return nil, err
		}

		result[key] = expanded
	}

	logValidation.Printf("Env variable expansion completed for server: %s", serverName)
	return result, nil
}

func expandMapInPlace(m *map[string]string, serverName, fieldDesc string) error {
	if len(*m) == 0 {
		return nil
	}

	logStdin.Printf("Server %q: expanding %d %s", serverName, len(*m), fieldDesc)
	expanded, err := expandEnvVariables(*m, serverName)
	if err != nil {
		return fmt.Errorf("server %q: failed to expand %s: %w", serverName, fieldDesc, err)
	}
	*m = expanded
	return nil
}

// expandTracingVariables expands ${VAR} expressions in TracingConfig fields.
// This is called for TOML-loaded configs before validation, mirroring the
// stdin JSON path where ExpandRawJSONVariables handles expansion.
func expandTracingVariables(cfg *TracingConfig) error {
	if cfg == nil {
		return nil
	}

	logValidation.Printf("Expanding tracing config variables: hasEndpoint=%v, hasTraceID=%v, hasSpanID=%v, hasHeaders=%v",
		cfg.Endpoint != "", cfg.TraceID != "", cfg.SpanID != "", cfg.Headers != "")

	fields := []struct {
		name     string
		jsonPath string
		value    *string
	}{
		{name: "endpoint", jsonPath: "gateway.opentelemetry.endpoint", value: &cfg.Endpoint},
		{name: "traceId", jsonPath: "gateway.opentelemetry.traceId", value: &cfg.TraceID},
		{name: "spanId", jsonPath: "gateway.opentelemetry.spanId", value: &cfg.SpanID},
		{name: "headers", jsonPath: "gateway.opentelemetry.headers", value: &cfg.Headers},
	}

	for _, field := range fields {
		if *field.value == "" {
			continue
		}

		expanded, err := expandVariables(*field.value, field.jsonPath)
		if err != nil {
			return err
		}

		logValidation.Printf("Expanded tracing %s variable", field.name)
		*field.value = expanded
	}

	logValidation.Print("Tracing config variable expansion completed")
	return nil
}
