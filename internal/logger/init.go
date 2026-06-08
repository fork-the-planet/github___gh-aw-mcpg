package logger

import "log"

// InitGatewayLoggers initializes the standard set of gateway loggers for the
// given log directory. Failures are printed as warnings but do not abort startup.
func InitGatewayLoggers(logDir string) {
	initWithWarning(InitFileLogger(logDir, "mcp-gateway.log"), "file logger")
	initWithWarning(InitServerFileLogger(logDir), "server file logger")
	initWithWarning(InitMarkdownLogger(logDir, "gateway.md"), "markdown logger")
	initWithWarning(InitJSONLLogger(logDir, "rpc-messages.jsonl"), "JSONL logger")
	initWithWarning(InitToolsLogger(logDir, "tools.json"), "tools logger")
}

// InitProxyLoggers initializes the subset of loggers used by the proxy command.
// Failures are printed as warnings but do not abort startup.
func InitProxyLoggers(logDir string) {
	initWithWarning(InitFileLogger(logDir, "proxy.log"), "file logger")
	initWithWarning(InitMarkdownLogger(logDir, "gateway.md"), "markdown logger")
	initWithWarning(InitJSONLLogger(logDir, "rpc-messages.jsonl"), "JSONL logger")
}

func initWithWarning(err error, name string) {
	if err != nil {
		log.Printf("Warning: Failed to initialize %s: %v", name, err)
	}
}

// logFallbackWarnings prints two WARNING lines for logger initialization failure with fallback:
// the first includes the underlying error, the second describes the fallback behavior.
func logFallbackWarnings(err error, errMsg, fallbackMsg string) {
	log.Printf("WARNING: %s: %v", errMsg, err)
	log.Printf("WARNING: %s", fallbackMsg)
}
