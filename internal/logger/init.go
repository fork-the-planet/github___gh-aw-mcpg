package logger

// InitGatewayLoggers initializes the standard set of gateway loggers for the
// given log directory. Failures are printed as warnings but do not abort startup.
func InitGatewayLoggers(logDir string) {
	initLoggerSet(logDir, gatewayLoggerInitializers)
}

// InitProxyLoggers initializes the subset of loggers used by the proxy command.
// Failures are printed as warnings but do not abort startup.
func InitProxyLoggers(logDir string) {
	initLoggerSet(logDir, proxyLoggerInitializers)
}
