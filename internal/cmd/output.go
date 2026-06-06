package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// writeGatewayConfigToStdout writes the rewritten gateway configuration to stdout
// per MCP Gateway Specification Section 5.4
func writeGatewayConfigToStdout(cfg *config.Config, listenAddr, mode string, tlsEnabled bool) error {
	return writeGatewayConfig(cfg, listenAddr, mode, tlsEnabled, os.Stdout)
}

func writeGatewayConfig(cfg *config.Config, listenAddr, mode string, tlsEnabled bool, w io.Writer) error {
	debugLog.Printf("Writing gateway config: listenAddr=%s, mode=%s, serverCount=%d", listenAddr, mode, len(cfg.Servers))

	// Parse listen address to extract host and port
	// Use net.SplitHostPort which properly handles both IPv4 and IPv6 addresses
	host, port := DefaultListenIPv4, DefaultListenPort
	if h, p, err := net.SplitHostPort(listenAddr); err == nil {
		if h != "" {
			host = h
		}
		if p != "" {
			port = p
		}
	}
	debugLog.Printf("Parsed listen address: host=%s, port=%s", host, port)

	// Determine domain for gateway output URLs.
	// Use the listen host, but map wildcard bind addresses (0.0.0.0, ::) to
	// 127.0.0.1 since clients cannot connect to wildcard addresses.
	// Note: cfg.Gateway.Domain is NOT used here because the gateway output is
	// consumed by host-side tools (health checks, connectivity checks) that
	// need localhost-reachable URLs. The domain field (e.g., "host.docker.internal")
	// is applied by the downstream converter when generating agent configs for
	// container-side access.
	domain := host
	if domain == "0.0.0.0" || domain == "::" || domain == "[::]" {
		domain = "127.0.0.1"
	}

	debugLog.Printf("Resolved gateway address: host=%s, port=%s", host, port)

	// Extract agent ID from gateway config (per spec section 7.1)
	agentID := cfg.GetAgentID()
	debugLog.Printf("Gateway config: auth_enabled=%v", agentID != "")

	debugLog.Printf("Gateway auth: agentIDConfigured=%v", agentID != "")

	// Build output configuration
	outputConfig := map[string]interface{}{
		"mcpServers": make(map[string]interface{}),
	}

	servers := outputConfig["mcpServers"].(map[string]interface{})

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	for name, server := range cfg.Servers {
		serverConfig := map[string]interface{}{
			"type": "http",
		}

		var serverURL string
		if mode == "routed" {
			serverURL = fmt.Sprintf("%s://%s:%s/mcp/%s", scheme, domain, port, name)
		} else {
			// Unified mode - all servers use /mcp endpoint
			serverURL = fmt.Sprintf("%s://%s:%s/mcp", scheme, domain, port)
		}
		serverConfig["url"] = serverURL
		debugLog.Printf("Writing server config: name=%s, url=%s, toolCount=%d", name, serverURL, len(server.Tools))

		// Add auth headers per MCP Gateway Specification Section 5.4
		// Authorization header contains API key directly (not Bearer scheme per spec 7.1)
		if agentID != "" {
			serverConfig["headers"] = map[string]string{
				"Authorization": agentID,
			}
		}

		// Include tools field from original configuration per MCP Gateway Specification v1.5.0 Section 5.4
		// This preserves tool filtering from the input configuration
		if len(server.Tools) > 0 {
			serverConfig["tools"] = server.Tools
		}

		debugLog.Printf("Wrote server config entry: name=%s, url=%v, toolCount=%d", name, serverConfig["url"], len(server.Tools))

		servers[name] = serverConfig
	}

	// Write to output as single JSON document
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(outputConfig); err != nil {
		return fmt.Errorf("failed to encode configuration: %w", err)
	}
	debugLog.Printf("Gateway config written successfully: serverCount=%d", len(servers))

	// Flush stdout buffer if it's a regular file
	// Note: Sync() fails on pipes and character devices like /dev/stdout,
	// which is expected behavior. We only sync regular files.
	if f, ok := w.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode().IsRegular() {
			if err := f.Sync(); err != nil {
				// Log warning but don't fail - sync is best-effort
				debugLog.Printf("Warning: failed to sync file: %v", err)
			}
		}
	}

	return nil
}
