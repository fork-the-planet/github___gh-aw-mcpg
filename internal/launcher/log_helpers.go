package launcher

import (
	"log"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
)

// logSecurityWarning logs container security warnings
func (l *Launcher) logSecurityWarning(serverID string, serverCfg *config.ServerConfig) {
	logger.LogWarnToServer(serverID, "backend", "Server '%s' uses direct command execution inside a container (command: %s)", serverID, serverCfg.Command)
	log.Printf("[LAUNCHER] ⚠️  Security Notice: Command '%s' will execute with the same privileges as the gateway", serverCfg.Command)
	log.Printf("[LAUNCHER] ⚠️  Consider using 'container' field instead for better isolation")
}

// logLaunchStart logs server launch initiation
func (l *Launcher) logLaunchStart(serverID, sessionID string, serverCfg *config.ServerConfig, isDirectCommand bool) {
	suffix := logger.SessionSuffix(sessionID)
	logger.LogInfoToServer(serverID, "backend", "Launching MCP backend server%s: server=%s%s, command=%s, args=%v",
		suffix, serverID, suffix, serverCfg.Command, sanitize.SanitizeArgs(serverCfg.Args))
	if sessionID != "" {
		logLauncher.Printf("Launching new session server: serverID=%s, sessionID=%s, command=%s", serverID, sessionID, serverCfg.Command)
	} else {
		logLauncher.Printf("Launching new server: serverID=%s, command=%s, inContainer=%v, isDirectCommand=%v",
			serverID, serverCfg.Command, l.runningInContainer, isDirectCommand)
	}
	log.Printf("[LAUNCHER] Command: %s", serverCfg.Command)
	log.Printf("[LAUNCHER] Args: %v", sanitize.SanitizeArgs(serverCfg.Args))
	if isDirectCommand {
		log.Printf("[LAUNCHER] isDirectCommand=true")
	}
}

// logEnvPassthrough checks and logs environment variable passthrough status
func (l *Launcher) logEnvPassthrough(args []string) {
	envutil.WalkDockerEnvArgs(args, func(_ int, varName, value string, found bool) {
		if !found {
			log.Printf("[LAUNCHER] ✗ WARNING: Env passthrough for %s requested but NOT FOUND in MCPG process", varName)
			return
		}
		if value != "" {
			log.Printf("[LAUNCHER] ✓ Env passthrough: %s=%s (from MCPG process)", varName, sanitize.TruncateSecret(value))
			return
		}
		log.Printf("[LAUNCHER] ⚠️  Env passthrough for %s is empty in MCPG process", varName)
	})
}

// logLaunchError logs detailed launch failure diagnostics
func (l *Launcher) logLaunchError(serverID, sessionID string, err error, serverCfg *config.ServerConfig, isDirectCommand bool) {
	LogConnectionError(ConnectionErrorContext{
		ServerID:           serverID,
		SessionID:          sessionID,
		Command:            serverCfg.Command,
		Args:               serverCfg.Args,
		Env:                serverCfg.Env,
		RunningInContainer: l.runningInContainer,
		IsDirectCommand:    isDirectCommand,
		StartupTimeout:     l.startupTimeout,
	}, err)
}

// logTimeoutError logs startup timeout diagnostics
func (l *Launcher) logTimeoutError(serverID, sessionID string) {
	suffix := logger.SessionSuffix(sessionID)
	logger.LogErrorToServer(serverID, "backend", "MCP backend server startup timeout%s: server=%s%s, timeout=%v",
		suffix, serverID, suffix, l.startupTimeout)
	log.Printf("[LAUNCHER] ⚠️  The server may be hanging or taking too long to initialize")
	log.Printf("[LAUNCHER] ⚠️  Consider increasing 'startupTimeout' in gateway config if server needs more time")
	logLauncher.Printf("Startup timeout occurred: serverID=%s%s, timeout=%v", serverID, suffix, l.startupTimeout)
}

// logLaunchSuccess logs successful server launch
func (l *Launcher) logLaunchSuccess(serverID, sessionID string) {
	suffix := logger.SessionSuffix(sessionID)
	logger.LogInfoToServer(serverID, "backend", "Successfully launched MCP backend server%s: server=%s%s", suffix, serverID, suffix)
	logLauncher.Printf("Connection established: serverID=%s%s", serverID, suffix)
}
