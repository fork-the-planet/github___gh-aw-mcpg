package config

import (
	"fmt"
	"strings"
)

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
		if IsStdioServerType(cfg.Type) {
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

// validateRuleBasedPatterns validates additional rule-based string constraints that
// are not handled by schema validation alone.
func validateRuleBasedPatterns(stdinCfg *StdinConfig) error {
	logValidation.Printf("Validating string patterns: server_count=%d", len(stdinCfg.MCPServers))

	for name, server := range stdinCfg.MCPServers {
		jsonPath := fmt.Sprintf("mcpServers.%s", name)
		logValidation.Printf("Validating server: name=%s, type=%s", name, server.Type)

		if IsStdioServerType(server.Type) {
			if server.Container != "" && !containerPattern.MatchString(server.Container) {
				return InvalidPattern("container", server.Container,
					fmt.Sprintf("%s.container", jsonPath),
					"Use a valid container image format (e.g., 'ghcr.io/owner/image:tag', 'owner/image:latest', or 'ghcr.io/owner/image:tag@sha256:<digest>')")
			}

			if server.Entrypoint != "" && len(strings.TrimSpace(server.Entrypoint)) == 0 {
				return InvalidValue("entrypoint", "entrypoint cannot be empty or whitespace only",
					fmt.Sprintf("%s.entrypoint", jsonPath),
					"Provide a valid entrypoint path or remove the field")
			}
		}

		if server.Type == "http" {
			if server.URL != "" && !urlPattern.MatchString(server.URL) {
				return InvalidPattern("url", server.URL,
					fmt.Sprintf("%s.url", jsonPath),
					"Use a valid HTTP or HTTPS URL (e.g., 'https://api.example.com/mcp')")
			}
		}
	}

	if stdinCfg.Gateway != nil {
		if err := validateGatewayConfig(stdinCfg.Gateway); err != nil {
			return err
		}

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
