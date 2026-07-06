// Package logger provides the gateway logging implementations.
//
// The package exposes three parallel global sink APIs:
//   - LogInfo / LogWarn / LogError / LogDebug for unified file/stdout logs
//   - LogInfoToMarkdown / LogWarnToMarkdown / LogErrorToMarkdown / LogDebugToMarkdown for markdown preview logs
//   - LogInfoToServer / LogWarnToServer / LogErrorToServer / LogDebugToServer
//     for per-server logs
//
// These APIs target different sinks and can be used together when a message should
// appear in multiple outputs.
//
// # Per-type setup and error-handler functions
//
// Each logger type has its own setup* and handleError* functions (e.g.
// setupFileLogger / handleFileLoggerError, setupMarkdownLogger /
// handleMarkdownLoggerError). These are intentionally not collapsed into a
// single generic helper because each type has unique initialization behaviour:
//
//   - JSONLLogger has no fallback path (a failure returns an error directly).
//   - ToolsLogger writes a one-time header and closes the file immediately after
//     opening, so its setup function owns that lifecycle step.
//   - FileLogger, MarkdownLogger, and RPCLogger each open a persistent file and
//     wire up different formatters.
//
// The per-type functions are bundled via the loggerFactory[T] generic defined in
// global_state.go, which handles the shared open-file / call-setup / call-onError flow.
package logger
