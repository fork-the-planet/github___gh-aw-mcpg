// Package logger provides the gateway logging implementations.
//
// The package exposes three parallel global sink APIs:
//   - LogInfo / LogWarn / LogError / LogDebug for unified file/stdout logs
//   - LogInfoMd / LogWarnMd / LogErrorMd / LogDebugMd for markdown preview logs
//   - LogInfoWithServer / LogWarnWithServer / LogErrorWithServer / LogDebugWithServer
//     for per-server logs
//
// These APIs target different sinks and can be used together when a message should
// appear in multiple outputs.
package logger
