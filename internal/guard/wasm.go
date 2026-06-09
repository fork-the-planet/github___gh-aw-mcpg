package guard

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var logWasm = logger.New("guard:wasm")

var globalCompilationCacheMu sync.Mutex

const wasmGuardsDirEnvVar = "MCP_GATEWAY_WASM_GUARDS_DIR"

// globalCompilationCache is a process-level compilation cache shared across all
// WasmGuard instances. wazero's cache is goroutine-safe and eliminates redundant
// JIT compilation when multiple guards load the same WASM binary.
var globalCompilationCache = wazero.NewCompilationCache()

// GetWASMGuardsRootDir returns the trimmed value of MCP_GATEWAY_WASM_GUARDS_DIR.
func GetWASMGuardsRootDir() string {
	return strings.TrimSpace(os.Getenv(wasmGuardsDirEnvVar))
}

// FindServerWASMGuardFile discovers the first .wasm file for a server under
// $MCP_GATEWAY_WASM_GUARDS_DIR/<serverID>.
func FindServerWASMGuardFile(serverID string) (string, bool, error) {
	guardsRootDir := GetWASMGuardsRootDir()
	if guardsRootDir == "" {
		logWasm.Printf("Skipping WASM guard discovery: %s is not set", wasmGuardsDirEnvVar)
		return "", false, nil
	}

	serverGuardDir := filepath.Join(guardsRootDir, serverID)
	logWasm.Printf("Searching for WASM guard file: serverID=%s, dir=%s", serverID, serverGuardDir)
	entries, err := os.ReadDir(serverGuardDir)
	if err != nil {
		if os.IsNotExist(err) {
			logWasm.Printf("No WASM guard directory found for serverID=%s", serverID)
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read server guard directory %q: %w", serverGuardDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".wasm") {
			wasmPath := filepath.Join(serverGuardDir, entry.Name())
			logWasm.Printf("Found WASM guard file: serverID=%s, path=%s", serverID, wasmPath)
			return wasmPath, true, nil
		}
	}

	logWasm.Printf("No WASM guard file found in directory: serverID=%s, dir=%s", serverID, serverGuardDir)
	return "", false, nil
}

func newCompilationCache(dir string) (wazero.CompilationCache, error) {
	if dir == "" {
		logWasm.Print("Creating in-memory compilation cache")
		return wazero.NewCompilationCache(), nil
	}
	logWasm.Printf("Creating disk-backed compilation cache: dir=%s", dir)
	return wazero.NewCompilationCacheWithDir(dir)
}

// ConfigureGlobalCompilationCache replaces the process-level compilation cache.
// This should be called during process startup before any guards are created.
func ConfigureGlobalCompilationCache(ctx context.Context, dir string) error {
	logWasm.Printf("Configuring global compilation cache: dir=%q", dir)
	cache, err := newCompilationCache(dir)
	if err != nil {
		return err
	}

	globalCompilationCacheMu.Lock()
	oldCache := globalCompilationCache
	if oldCache == nil {
		globalCompilationCache = cache
		globalCompilationCacheMu.Unlock()
		logWasm.Print("Global compilation cache set (no previous cache)")
		return nil
	}

	if err := oldCache.Close(ctx); err != nil {
		globalCompilationCacheMu.Unlock()

		closeReplacementErr := cache.Close(ctx)
		if closeReplacementErr != nil {
			return errors.Join(
				fmt.Errorf("failed to close previous compilation cache: %w", err),
				fmt.Errorf("failed to close replacement compilation cache: %w", closeReplacementErr),
			)
		}
		return fmt.Errorf("failed to close previous compilation cache: %w", err)
	}
	globalCompilationCache = cache
	globalCompilationCacheMu.Unlock()

	logWasm.Print("Global compilation cache replaced successfully")
	return nil
}

// CloseGlobalCompilationCache releases JIT resources held by the shared
// compilation cache. It must be called once during graceful shutdown, after
// all WasmGuard runtimes have been closed (i.e., after Registry.Close()).
// Calling it while guards are still active or calling it more than once leads
// to undefined behavior. It is not safe to call concurrently.
func CloseGlobalCompilationCache(ctx context.Context) error {
	logWasm.Print("Closing global compilation cache")
	globalCompilationCacheMu.Lock()
	cache := globalCompilationCache
	globalCompilationCacheMu.Unlock()
	if cache == nil {
		logWasm.Print("Global compilation cache is nil, nothing to close")
		return nil
	}
	if err := cache.Close(ctx); err != nil {
		return err
	}
	logWasm.Print("Global compilation cache closed")
	return nil
}

// WasmGuardOptions configures optional settings for WASM guard creation
type WasmGuardOptions struct {
	// Stdout is the writer for WASM stdout output. Defaults to os.Stderr if nil.
	Stdout io.Writer
	// Stderr is the writer for WASM stderr output. Defaults to os.Stderr if nil.
	Stderr io.Writer
	// CompilationCache overrides the process-level compilation cache.
	// If nil and DisableCompilationCache is false, the shared globalCompilationCache is used.
	CompilationCache wazero.CompilationCache
	// DisableCompilationCache, when true, prevents any compilation cache from being used
	// even if a global or per-instance cache is available. This can be useful to avoid
	// unbounded memory growth when many distinct WASM binaries are loaded over the
	// lifetime of a long-lived process.
	DisableCompilationCache bool
}

// WasmGuard implements Guard interface by executing a WASM module in-process
// The WASM module runs sandboxed within the gateway using wazero runtime
// Guards cannot make direct network calls - they receive a BackendCaller interface via host functions
//
// Thread Safety: WASM modules are single-threaded, so all calls to a guard instance
// are serialized using a mutex. Concurrent requests will queue and execute one at a time.
type WasmGuard struct {
	name    string
	runtime wazero.Runtime
	module  api.Module

	// Backend caller provided to the guard via host functions
	backend BackendCaller

	// mu serializes all calls to the WASM module.
	// WASM modules are single-threaded and cannot handle concurrent calls.
	// All exported methods (LabelAgent, LabelResource, LabelResponse) hold mu
	// for their entire duration, including any nested calls to callWasmFunction.
	mu sync.Mutex

	// failed and failedErr are set when the WASM module encounters a trap
	// (e.g. unreachable instruction from a Rust panic). Once failed, all
	// subsequent calls return an error immediately because the module's
	// internal state may be corrupted.
	// Both fields are accessed only while mu is held.
	failed    bool
	failedErr error

	// warnedDirectMemoryPath ensures we emit the allocator-fallback warning at
	// most once per guard instance. It is accessed only while mu is held.
	warnedDirectMemoryPath bool
}

// NewWasmGuard creates a new WASM guard from a WASM binary file
func NewWasmGuard(ctx context.Context, name string, wasmPath string, backend BackendCaller) (*WasmGuard, error) {
	logWasm.Printf("Creating WASM guard: name=%s, path=%s", name, wasmPath)

	// Read WASM binary
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	return NewWasmGuardFromBytes(ctx, name, wasmBytes, backend)
}

// NewWasmGuardFromBytes creates a new WASM guard from WASM binary bytes
// This is useful when loading guards from URLs or other sources
func NewWasmGuardFromBytes(ctx context.Context, name string, wasmBytes []byte, backend BackendCaller) (*WasmGuard, error) {
	return NewWasmGuardWithOptions(ctx, name, wasmBytes, backend, nil)
}

// NewWasmGuardWithOptions creates a new WASM guard from WASM binary bytes with custom options
// Options can be nil to use defaults (stdout/stderr go to os.Stderr/os.Stderr)
func NewWasmGuardWithOptions(ctx context.Context, name string, wasmBytes []byte, backend BackendCaller, opts *WasmGuardOptions) (*WasmGuard, error) {
	logWasm.Printf("Creating WASM guard from bytes: name=%s, size=%d", name, len(wasmBytes))

	// Select compilation cache: explicit opt-out, injected cache, or shared global.
	runtimeConfig := wazero.NewRuntimeConfigCompiler().WithCloseOnContextDone(true)
	if opts != nil && opts.DisableCompilationCache {
		// Caller explicitly disabled caching
	} else if opts != nil && opts.CompilationCache != nil {
		runtimeConfig = runtimeConfig.WithCompilationCache(opts.CompilationCache)
	} else {
		runtimeConfig = runtimeConfig.WithCompilationCache(globalCompilationCache)
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Instantiate WASI
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		runtime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	guard := &WasmGuard{
		name:    name,
		runtime: runtime,
		backend: backend,
	}

	// Create host functions for the guard to call
	if err := guard.instantiateHostFunctions(ctx); err != nil {
		runtime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate host functions: %w", err)
	}

	// Configure module options with stdout/stderr and stdin isolation.
	stdoutWriter := io.Writer(os.Stderr)
	stderrWriter := io.Writer(os.Stderr)
	if opts != nil {
		if opts.Stdout != nil {
			stdoutWriter = opts.Stdout
		}
		if opts.Stderr != nil {
			stderrWriter = opts.Stderr
		}
	}

	// WithStdin prevents WASM from accidentally reading gateway's MCP protocol stdin
	moduleConfig := wazero.NewModuleConfig().
		WithName(func() string {
			if name == "" {
				return "guard"
			}
			return name
		}()).
		// WithStartFunctions with no args suppresses automatic _start execution
		// so guard loading cannot block on stdin or perform unexpected I/O.
		WithStartFunctions().
		WithStdin(strings.NewReader("")). // Isolate stdin
		WithStdout(stdoutWriter).         // Keep WASM stdout off gateway stdout (MCP stream)
		WithStderr(stderrWriter)

	// Compile and instantiate the WASM module
	module, err := runtime.InstantiateWithConfig(ctx, wasmBytes, moduleConfig)
	if err != nil {
		runtime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	guard.module = module

	// Verify required functions are exported
	labelResourceFn := module.ExportedFunction("label_resource")
	labelResponseFn := module.ExportedFunction("label_response")
	labelAgentFn := module.ExportedFunction("label_agent")

	if labelResourceFn == nil || labelResponseFn == nil {
		runtime.Close(ctx)

		// Check if this was compiled with standard Go (only _start is exported)
		if module.ExportedFunction("_start") != nil && labelResourceFn == nil {
			return nil, fmt.Errorf("WASM module does not export guard functions. " +
				"This usually means the guard was compiled with standard Go instead of TinyGo. " +
				"TinyGo is required for proper function exports. " +
				"Note: TinyGo 0.34 supports Go 1.19-1.23 (not yet compatible with Go 1.25). " +
				"See examples/guards/sample-guard/README.md for details")
		}

		return nil, fmt.Errorf("WASM module must export label_resource and label_response functions")
	}

	if labelAgentFn == nil {
		runtime.Close(ctx)
		return nil, fmt.Errorf("WASM module must export label_agent")
	}

	logWasm.Printf("WASM guard created successfully: name=%s", name)
	return guard, nil
}

// instantiateHostFunctions creates the host functions that the WASM module can call
func (g *WasmGuard) instantiateHostFunctions(ctx context.Context) error {
	logWasm.Printf("Instantiating WASM host functions: guard=%s", g.name)
	// Create a host module with functions the guard can call
	_, err := g.runtime.NewHostModuleBuilder("env").
		// call_backend: allows guards to call MCP tools on the backend
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(g.hostCallBackend), []api.ValueType{
			api.ValueTypeI32, // ptr to tool name
			api.ValueTypeI32, // tool name length
			api.ValueTypeI32, // ptr to args JSON
			api.ValueTypeI32, // args length
			api.ValueTypeI32, // ptr to result buffer
			api.ValueTypeI32, // result buffer size
		}, []api.ValueType{api.ValueTypeI32}). // returns result length or negative error
		Export("call_backend").
		// host_log: allows guards to send log messages to the gateway
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(g.hostLog), []api.ValueType{
			api.ValueTypeI32, // log level (0=debug, 1=info, 2=warn, 3=error)
			api.ValueTypeI32, // ptr to message
			api.ValueTypeI32, // message length
		}, []api.ValueType{}).
		Export("host_log").
		Instantiate(ctx)

	if err != nil {
		return err
	}
	logWasm.Printf("WASM host functions instantiated: guard=%s (call_backend, host_log)", g.name)
	return nil
}

// hostCallBackend is called by the WASM module to make backend MCP calls
func (g *WasmGuard) hostCallBackend(ctx context.Context, m api.Module, stack []uint64) {
	toolNamePtr := uint32(stack[0])
	toolNameLen := uint32(stack[1])
	argsPtr := uint32(stack[2])
	argsLen := uint32(stack[3])
	resultPtr := uint32(stack[4])
	resultSize := uint32(stack[5])

	// Helper to set return value
	setResult := func(code int32) {
		stack[0] = uint64(uint32(code))
	}

	// Helper to set generic error return value
	setError := func() {
		setResult(-1)
	}

	// Read tool name from WASM memory
	toolNameBytes, ok := m.Memory().Read(toolNamePtr, toolNameLen)
	if !ok {
		setError()
		return
	}
	toolName := string(toolNameBytes)

	// Read args JSON from WASM memory
	argsBytes, ok := m.Memory().Read(argsPtr, argsLen)
	if !ok {
		setError()
		return
	}

	// Parse args
	var args any
	if len(argsBytes) > 0 {
		if err := json.Unmarshal(argsBytes, &args); err != nil {
			logWasm.Printf("Failed to unmarshal backend call args: %v", err)
			setError()
			return
		}
	}

	logWasm.Printf("WASM guard calling backend: tool=%s", toolName)

	// Call backend
	result, err := g.backend.CallTool(ctx, toolName, args)
	if err != nil {
		logWasm.Printf("Backend call failed: %v", err)
		setError()
		return
	}

	// Marshal result to JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		logWasm.Printf("Failed to marshal backend result: %v", err)
		setError()
		return
	}

	// Check if result fits in buffer
	if uint32(len(resultJSON)) > resultSize {
		logWasm.Printf("Result too large: %d > %d", len(resultJSON), resultSize)
		// Signal buffer-too-small with required size protocol:
		// return -2 and (if possible) write required size (u32 LE) to resultPtr.
		if resultSize >= 4 {
			var required [4]byte
			binary.LittleEndian.PutUint32(required[:], uint32(len(resultJSON)))
			if !m.Memory().Write(resultPtr, required[:]) {
				logWasm.Printf("Failed to write required size to WASM memory")
			}
		}
		setResult(-2)
		return
	}

	// Write result to WASM memory
	if !m.Memory().Write(resultPtr, resultJSON) {
		logWasm.Printf("Failed to write result to WASM memory")
		setError()
		return
	}

	// Return result length
	stack[0] = uint64(uint32(len(resultJSON)))
}

// Log level constants for hostLog
const (
	logLevelDebug = 0
	logLevelInfo  = 1
	logLevelWarn  = 2
	logLevelError = 3
)

// hostLog is called by the WASM module to send log messages to the gateway
func (g *WasmGuard) hostLog(ctx context.Context, m api.Module, stack []uint64) {
	level := uint32(stack[0])
	msgPtr := uint32(stack[1])
	msgLen := uint32(stack[2])

	// Read message from WASM memory
	msgBytes, ok := m.Memory().Read(msgPtr, msgLen)
	if !ok {
		logWasm.Printf("hostLog: failed to read message from WASM memory")
		return
	}
	msg := string(msgBytes)

	// Log at the appropriate level
	prefix := fmt.Sprintf("[guard:%s] ", g.name)
	switch level {
	case logLevelDebug:
		logWasm.Printf("%sDEBUG: %s", prefix, msg)
	case logLevelInfo:
		logWasm.Printf("%sINFO: %s", prefix, msg)
	case logLevelWarn:
		logWasm.Printf("%sWARN: %s", prefix, msg)
		logger.LogWarn("guard", "[%s] %s", g.name, msg)
	case logLevelError:
		logWasm.Printf("%sERROR: %s", prefix, msg)
		logger.LogError("guard", "[%s] %s", g.name, msg)
	default:
		logWasm.Printf("%s%s", prefix, msg)
	}
}

// Name returns the identifier for this guard
func (g *WasmGuard) Name() string {
	return g.name
}

// IsHealthy reports whether the guard is still usable after previous WASM calls.
func (g *WasmGuard) IsHealthy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.failed
}

// callWasmGuardFunction serialises WASM access, sets the backend reference, marshals
// inputData, logs the input, calls the named WASM export, and returns the raw result.
// All three public dispatch methods (LabelAgent, LabelResource, LabelResponse) share
// this preamble; keeping it in one place ensures locking and backend-update logic
// cannot drift between them.
func (g *WasmGuard) callWasmGuardFunction(ctx context.Context, funcName string, backend BackendCaller, inputData map[string]any) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.backend = backend

	inputJSON, err := json.Marshal(inputData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s input: %w", funcName, err)
	}
	logWasm.Printf("%s input JSON (%d bytes): %s", funcName, len(inputJSON), string(inputJSON))

	return g.callWasmFunction(ctx, funcName, inputJSON)
}

// LabelAgent calls the WASM module's label_agent function.
func (g *WasmGuard) LabelAgent(ctx context.Context, policy any, backend BackendCaller, caps *difc.Capabilities) (*LabelAgentResult, error) {
	logWasm.Printf("LabelAgent called: guard=%s", g.name)

	// Normalisation and payload-build operate only on the caller-supplied `policy`
	// argument and do not access any g.* fields, so they are safe to run outside
	// the lock that callWasmGuardFunction acquires.
	normalizedPolicy, err := normalizePolicyPayload(policy)
	if err != nil {
		logWasm.Printf("LabelAgent normalizePolicyPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}
	logger.LogMarshaledForDebug(
		normalizedPolicy,
		func(policyJSON string) {
			logWasm.Printf("LabelAgent normalized policy: guard=%s, policy=%s", g.name, policyJSON)
		},
		func(marshalErr error) {
			logWasm.Printf("LabelAgent normalized policy (marshal failed): guard=%s, error=%v", g.name, marshalErr)
		},
	)
	_ = caps

	input, err := buildStrictLabelAgentPayload(normalizedPolicy)
	if err != nil {
		logWasm.Printf("LabelAgent buildStrictLabelAgentPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}

	resultJSON, err := g.callWasmGuardFunction(ctx, "label_agent", backend, input)
	if err != nil {
		logWasm.Printf("LabelAgent callWasmFunction failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}
	logWasm.Printf("LabelAgent raw response JSON (%d bytes): %s", len(resultJSON), string(resultJSON))

	if len(resultJSON) == 0 {
		logWasm.Printf("LabelAgent returned empty response: guard=%s", g.name)
		return nil, fmt.Errorf("label_agent returned empty response")
	}

	result, err := parseLabelAgentResponse(resultJSON)
	if err != nil {
		logWasm.Printf("LabelAgent response validation failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}

	logger.LogMarshaledForDebug(
		result,
		func(responseJSON string) {
			logWasm.Printf("LabelAgent parsed response: guard=%s, response=%s", g.name, responseJSON)
		},
		func(marshalErr error) {
			logWasm.Printf("LabelAgent parsed response (marshal failed): guard=%s, error=%v", g.name, marshalErr)
		},
	)

	return result, nil
}

// LabelResource calls the WASM module's label_resource function
func (g *WasmGuard) LabelResource(ctx context.Context, toolName string, args any, backend BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	logWasm.Printf("LabelResource called: toolName=%s, args=%+v", toolName, args)

	// Prepare input
	input := map[string]any{
		"tool_name": toolName,
		"tool_args": args,
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	const funcName = "label_resource"
	resultJSON, err := g.callWasmGuardFunction(ctx, funcName, backend, input)
	if err != nil {
		return nil, difc.OperationWrite, err
	}

	// Parse result
	response, err := unmarshalWasmResponse(funcName, resultJSON)
	if err != nil {
		return nil, difc.OperationWrite, err
	}

	return parseResourceResponse(response)
}

// LabelResponse calls the WASM module's label_response function
func (g *WasmGuard) LabelResponse(ctx context.Context, toolName string, result any, backend BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
	logWasm.Printf("LabelResponse called: toolName=%s", toolName)

	// Prepare input
	input := map[string]any{
		"tool_name":   toolName,
		"tool_result": result,
	}
	if state := GetRequestStateFromContext(ctx); state != nil {
		if stateMap, ok := state.(map[string]any); ok {
			if toolArgs, hasToolArgs := stateMap["tool_args"]; hasToolArgs {
				input["tool_args"] = toolArgs
			}
		}
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	const funcName = "label_response"
	resultJSON, err := g.callWasmGuardFunction(ctx, funcName, backend, input)
	if err != nil {
		return nil, err
	}

	// If empty result, return nil (no fine-grained labeling)
	if len(resultJSON) == 0 {
		return nil, nil
	}

	// Parse result - check for new path-based format first
	responseMap, err := unmarshalWasmResponse(funcName, resultJSON)
	if err != nil {
		return nil, err
	}

	// Check for path-based labeling format (preferred, more efficient)
	if _, hasLabeledPaths := responseMap["labeled_paths"]; hasLabeledPaths {
		return parsePathLabeledResponse(resultJSON, result)
	}

	// Legacy format: check if it's a collection with "items"
	if items, ok := responseMap["items"].([]any); ok && len(items) > 0 {
		return parseCollectionLabeledData(items)
	}

	// No fine-grained labeling
	return nil, nil
}

// unmarshalWasmResponse decodes a JSON byte slice returned by a WASM guard
// function into a generic map. funcName is included in the error message to
// make it easier to identify which call failed in log output.
func unmarshalWasmResponse(funcName string, data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s WASM response: %w", funcName, err)
	}
	return m, nil
}

// Close releases WASM runtime resources
func (g *WasmGuard) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx := context.WithoutCancel(ctx)
	var moduleErr, runtimeErr error
	if g.module != nil {
		moduleErr = g.module.Close(cleanupCtx)
	}
	if g.runtime != nil {
		runtimeErr = g.runtime.Close(cleanupCtx)
	}
	return errors.Join(moduleErr, runtimeErr)
}
