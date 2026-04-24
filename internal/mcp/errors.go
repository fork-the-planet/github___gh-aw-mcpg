package mcp

import (
	"log"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
	"github.com/github/gh-aw-mcpg/internal/strutil"
)

// ConnectionErrorContext holds all context needed to produce a detailed connection
// failure diagnostic. Fields left at their zero values are omitted from the output.
type ConnectionErrorContext struct {
	ServerID           string
	SessionID          string
	Command            string
	Args               []string
	Env                map[string]string
	RunningInContainer bool
	IsDirectCommand    bool
	StartupTimeout     time.Duration
	StderrOutput       string
}

// LogConnectionError logs detailed diagnostics for a connection failure, including
// command context, captured stderr, and actionable hints based on the error type
// and execution environment. All callers (launcher and mcp connection) use this
// single function so that hint analysis and output format remain consistent.
func LogConnectionError(errCtx ConnectionErrorContext, err error) {
	suffix := strutil.SessionSuffix(errCtx.SessionID)

	// Structured log via file logger.
	if errCtx.ServerID != "" {
		logger.LogErrorWithServer(errCtx.ServerID, "backend",
			"MCP backend connection failed%s: server=%s, command=%s, args=%v, error=%v",
			suffix, errCtx.ServerID, errCtx.Command, sanitize.SanitizeArgs(errCtx.Args), err)
	} else {
		logger.LogErrorMd("backend",
			"MCP backend connection failed, command=%s, args=%v, error=%v",
			errCtx.Command, sanitize.SanitizeArgs(errCtx.Args), err)
	}

	// Human-readable console output.
	if errCtx.ServerID != "" {
		log.Printf("[LAUNCHER] ❌ FAILED to connect to server '%s'%s", errCtx.ServerID, suffix)
	} else {
		log.Printf("[LAUNCHER] ❌ MCP Connection Failed")
	}
	log.Printf("[LAUNCHER] Error: %v", err)
	log.Printf("[LAUNCHER] Debug Information:")
	log.Printf("[LAUNCHER]   - Command: %s", errCtx.Command)
	log.Printf("[LAUNCHER]   - Args: %v", sanitize.SanitizeArgs(errCtx.Args))
	if len(errCtx.Env) > 0 {
		log.Printf("[LAUNCHER]   - Env vars: %v", sanitize.TruncateSecretMap(errCtx.Env))
	}
	if errCtx.RunningInContainer || errCtx.IsDirectCommand {
		log.Printf("[LAUNCHER]   - Running in container: %v", errCtx.RunningInContainer)
		log.Printf("[LAUNCHER]   - Is direct command: %v", errCtx.IsDirectCommand)
	}
	if errCtx.StartupTimeout > 0 {
		log.Printf("[LAUNCHER]   - Startup timeout: %v", errCtx.StartupTimeout)
	}

	// Log captured stderr output from the container/process.
	if errCtx.StderrOutput != "" {
		sanitizedStderr := sanitize.SanitizeString(errCtx.StderrOutput)
		logger.LogErrorMd("backend", "MCP backend stderr output:\n%s", sanitizedStderr)
		log.Printf("[LAUNCHER]   📋 Process stderr output:")
		for _, line := range strings.Split(sanitizedStderr, "\n") {
			log.Printf("[LAUNCHER]      %s", line)
		}
	}

	// Hints based on execution context (launcher-specific).
	if errCtx.IsDirectCommand && errCtx.RunningInContainer {
		log.Printf("[LAUNCHER] ⚠️  Possible causes:")
		log.Printf("[LAUNCHER]   - Command '%s' may not be installed in the gateway container", errCtx.Command)
		log.Printf("[LAUNCHER]   - Consider using 'container' config instead of 'command'")
		log.Printf("[LAUNCHER]   - Or add '%s' to the gateway's Dockerfile", errCtx.Command)
	} else if errCtx.IsDirectCommand {
		log.Printf("[LAUNCHER] ⚠️  Possible causes:")
		log.Printf("[LAUNCHER]   - Command '%s' may not be in PATH", errCtx.Command)
		log.Printf("[LAUNCHER]   - Check if '%s' is installed: which %s", errCtx.Command, errCtx.Command)
		log.Printf("[LAUNCHER]   - Verify file permissions and execute bit")
	}

	// Hints based on error message content.
	errStr := err.Error()
	if strings.Contains(errStr, "executable file not found") || strings.Contains(errStr, "no such file or directory") {
		logger.LogErrorMd("backend", "MCP backend command not found, command=%s", errCtx.Command)
		log.Printf("[LAUNCHER] ⚠️  Command '%s' not found in PATH", errCtx.Command)
		log.Printf("[LAUNCHER] ⚠️  Verify the command is installed and executable")
	}

	if strings.Contains(errStr, "EOF") || strings.Contains(errStr, "broken pipe") {
		logger.LogErrorMd("backend", "MCP backend connection/protocol error, command=%s", errCtx.Command)
		log.Printf("[LAUNCHER] ⚠️  Process started but terminated unexpectedly")
		log.Printf("[LAUNCHER] ⚠️  Check if the command supports MCP protocol over stdio")
	}
}
