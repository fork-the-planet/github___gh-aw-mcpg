package logger

type loggerInitEntry struct {
	name string
	init func(logDir string) error
}

type loggerCloseEntry struct {
	name  string
	close func() error
}

var gatewayLoggerInitializers = []loggerInitEntry{
	{
		name: "file logger",
		init: func(logDir string) error {
			return InitFileLogger(logDir, "mcp-gateway.log")
		},
	},
	{
		name: "server file logger",
		init: InitServerFileLogger,
	},
	{
		name: "markdown logger",
		init: func(logDir string) error {
			return InitMarkdownLogger(logDir, "gateway.md")
		},
	},
	{
		name: "JSONL logger",
		init: func(logDir string) error {
			return InitJSONLLogger(logDir, "rpc-messages.jsonl")
		},
	},
	{
		name: "tools logger",
		init: func(logDir string) error {
			return InitToolsLogger(logDir, "tools.json")
		},
	},
}

var proxyLoggerInitializers = []loggerInitEntry{
	{
		name: "file logger",
		init: func(logDir string) error {
			return InitFileLogger(logDir, "proxy.log")
		},
	},
	{
		name: "markdown logger",
		init: func(logDir string) error {
			return InitMarkdownLogger(logDir, "gateway.md")
		},
	},
	{
		name: "JSONL logger",
		init: func(logDir string) error {
			return InitJSONLLogger(logDir, "rpc-messages.jsonl")
		},
	},
}

var globalLoggerClosers = []loggerCloseEntry{
	{
		name:  "file logger",
		close: CloseGlobalLogger,
	},
	{
		name:  "JSONL logger",
		close: CloseJSONLLogger,
	},
	{
		name:  "markdown logger",
		close: CloseMarkdownLogger,
	},
	{
		name:  "tools logger",
		close: CloseToolsLogger,
	},
	{
		name:  "server file logger",
		close: CloseServerFileLogger,
	},
}

func initLoggerSet(logDir string, entries []loggerInitEntry) {
	for _, entry := range entries {
		initWithWarning(entry.init(logDir), entry.name)
	}
}

func closeLoggerSet(entries []loggerCloseEntry) error {
	var firstErr error
	for _, entry := range entries {
		if err := entry.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
