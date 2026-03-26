package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var logWasm = logger.New("guard:wasm")

// WasmGuardOptions configures optional settings for WASM guard creation
type WasmGuardOptions struct {
	// Stdout is the writer for WASM stdout output. Defaults to os.Stdout if nil.
	Stdout io.Writer
	// Stderr is the writer for WASM stderr output. Defaults to os.Stderr if nil.
	Stderr io.Writer
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

	// mu serializes all calls to the WASM module
	// WASM modules are single-threaded and cannot handle concurrent calls
	mu sync.Mutex
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
// Options can be nil to use defaults (stdout/stderr go to os.Stdout/os.Stderr)
func NewWasmGuardWithOptions(ctx context.Context, name string, wasmBytes []byte, backend BackendCaller, opts *WasmGuardOptions) (*WasmGuard, error) {
	logWasm.Printf("Creating WASM guard from bytes: name=%s, size=%d", name, len(wasmBytes))

	// Create WASM runtime with explicit compiler config and context-based cancellation
	// WithCloseOnContextDone enables request-scoped timeouts to propagate into guard execution
	runtimeConfig := wazero.NewRuntimeConfigCompiler().WithCloseOnContextDone(true)
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

	// Configure module options with stdout/stderr and stdin isolation
	// WithStdin prevents WASM from accidentally reading gateway's MCP protocol stdin
	moduleConfig := wazero.NewModuleConfig().
		WithName("guard").
		WithStartFunctions().
		WithStdin(strings.NewReader("")) // Isolate stdin
	if opts != nil {
		if opts.Stdout != nil {
			moduleConfig = moduleConfig.WithStdout(opts.Stdout)
		}
		if opts.Stderr != nil {
			moduleConfig = moduleConfig.WithStderr(opts.Stderr)
		}
	}

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

	return err
}

// hostCallBackend is called by the WASM module to make backend MCP calls
func (g *WasmGuard) hostCallBackend(ctx context.Context, m api.Module, stack []uint64) {
	toolNamePtr := uint32(stack[0])
	toolNameLen := uint32(stack[1])
	argsPtr := uint32(stack[2])
	argsLen := uint32(stack[3])
	resultPtr := uint32(stack[4])
	resultSize := uint32(stack[5])

	// Helper to set error return value
	setError := func() {
		stack[0] = uint64(^uint32(0)) // Max uint32 represents error
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
	var args interface{}
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
		setError()
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
	case logLevelError:
		logWasm.Printf("%sERROR: %s", prefix, msg)
	default:
		logWasm.Printf("%s%s", prefix, msg)
	}
}

// Name returns the identifier for this guard
func (g *WasmGuard) Name() string {
	return g.name
}

func normalizePolicyPayload(policy interface{}) (interface{}, error) {
	if policy == nil {
		return nil, fmt.Errorf("policy is required")
	}

	if policyString, ok := policy.(string); ok {
		trimmed := strings.TrimSpace(policyString)
		if trimmed == "" {
			return nil, fmt.Errorf("policy string is empty")
		}

		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, fmt.Errorf("policy string is not valid JSON object: %w", err)
		}

		switch parsed.(type) {
		case map[string]interface{}:
			return parsed, nil
		default:
			return nil, fmt.Errorf("policy JSON must decode to an object")
		}
	}

	return policy, nil
}

func buildStrictLabelAgentPayload(policy interface{}) (map[string]interface{}, error) {
	if policy == nil {
		return nil, fmt.Errorf("invalid guard policy transport shape: expected {\"allow-only\":{\"repos\":...,\"min-integrity\":...}}")
	}

	if policyMap, ok := policy.(map[string]interface{}); ok {
		if nested, hasPolicy := policyMap["policy"]; hasPolicy {
			if nestedMap, nestedOK := nested.(map[string]interface{}); nestedOK {
				if _, hasAllowOnly := nestedMap["allow-only"]; hasAllowOnly {
					return nil, fmt.Errorf("gateway policy adapter is outdated: remove legacy envelope key policy before calling label_agent")
				}
			}
		}
	}

	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize label_agent policy: %w", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(policyJSON, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode label_agent policy payload: %w", err)
	}

	if _, hasPolicyEnvelope := payload["policy"]; hasPolicyEnvelope {
		return nil, fmt.Errorf("gateway policy adapter is outdated: remove legacy envelope key policy before calling label_agent")
	}

	allowOnlyRaw, ok := payload["allow-only"]
	if !ok {
		// Accept legacy "allowonly" form for backward compatibility
		allowOnlyRaw, ok = payload["allowonly"]
	}
	if !ok {
		return nil, fmt.Errorf("label_agent policy must use top-level allow-only object (received policy.allow-only)")
	}

	// Validate that the only allowed top-level keys are "allow-only" (or legacy "allowonly")
	// and the optional "trusted-bots" key.
	for k := range payload {
		switch k {
		case "allow-only", "allowonly", "trusted-bots":
			// valid top-level keys
		default:
			return nil, fmt.Errorf("invalid guard policy transport shape: unexpected key %q", k)
		}
	}

	allowOnly, ok := allowOnlyRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid guard policy transport shape: expected {\"allow-only\":{\"repos\":...,\"min-integrity\":...}}")
	}

	reposRaw, hasRepos := allowOnly["repos"]
	integrityRaw, hasIntegrity := allowOnly["min-integrity"]
	if !hasIntegrity {
		integrityRaw, hasIntegrity = allowOnly["integrity"]
	}
	if !hasRepos || !hasIntegrity {
		return nil, fmt.Errorf("invalid guard policy transport shape: missing required fields repos and/or min-integrity in allow-only object")
	}

	// Validate that the allow-only object contains only known keys.
	for k := range allowOnly {
		switch k {
		case "repos", "min-integrity", "integrity", "blocked-users", "approval-labels", "trusted-users":
			// valid allow-only keys
		default:
			return nil, fmt.Errorf("invalid guard policy transport shape: unexpected allow-only key %q", k)
		}
	}

	if !isValidAllowOnlyRepos(reposRaw) {
		return nil, fmt.Errorf("invalid repos value: expected all, public, or non-empty array of scoped strings")
	}

	integrity, ok := integrityRaw.(string)
	if !ok {
		return nil, fmt.Errorf("invalid integrity value: expected one of none|unapproved|approved|merged")
	}

	switch strings.ToLower(strings.TrimSpace(integrity)) {
	case "none", "unapproved", "approved", "merged":
	default:
		return nil, fmt.Errorf("invalid integrity value: expected one of none|unapproved|approved|merged")
	}

	// Validate blocked-users if present: must be a non-empty array of non-empty strings.
	if blockedUsersRaw, ok := allowOnly["blocked-users"]; ok {
		arr, ok := blockedUsersRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid blocked-users value: expected array of strings")
		}
		for _, entry := range arr {
			if s, ok := entry.(string); !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("invalid blocked-users value: each entry must be a non-empty string")
			}
		}
	}

	// Validate approval-labels if present: must be a non-empty array of non-empty strings.
	if approvalLabelsRaw, ok := allowOnly["approval-labels"]; ok {
		arr, ok := approvalLabelsRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid approval-labels value: expected array of strings")
		}
		for _, entry := range arr {
			if s, ok := entry.(string); !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("invalid approval-labels value: each entry must be a non-empty string")
			}
		}
	}

	// Validate trusted-bots if present.
	// Per spec §4.1.3.4: trustedBots MUST be a non-empty array of strings when present.
	if trustedBotsRaw, hasTrustedBots := payload["trusted-bots"]; hasTrustedBots {
		trustedBots, ok := trustedBotsRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid trusted-bots value: expected non-empty array of strings")
		}
		if len(trustedBots) == 0 {
			return nil, fmt.Errorf("invalid trusted-bots value: must be a non-empty array when present")
		}
		for _, entry := range trustedBots {
			if s, ok := entry.(string); !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("invalid trusted-bots value: each entry must be a non-empty string")
			}
		}
	}

	// Validate trusted-users if present inside allow-only.
	// Must be a non-empty array of non-empty strings when present.
	if trustedUsersRaw, ok := allowOnly["trusted-users"]; ok {
		arr, ok := trustedUsersRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid trusted-users value: expected array of strings")
		}
		for _, entry := range arr {
			if s, ok := entry.(string); !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("invalid trusted-users value: each entry must be a non-empty string")
			}
		}
	}

	return payload, nil
}

// BuildLabelAgentPayload constructs the label_agent input payload from the given guard policy
// and optional lists of additional trusted bot usernames and trusted user logins. The trusted
// bots are merged with the guard's built-in list and cannot remove any built-in entries. If
// both trustedBots and trustedUsers are nil or empty, the returned payload contains only the
// allow-only policy.
func BuildLabelAgentPayload(policy interface{}, trustedBots []string, trustedUsers []string) interface{} {
	if len(trustedBots) == 0 && len(trustedUsers) == 0 {
		return policy
	}

	// Marshal the policy to a generic map so we can inject the trusted-bots and trusted-users
	// keys alongside the allow-only policy without altering the policy itself.
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		// If we can't marshal the policy, return it as-is; buildStrictLabelAgentPayload
		// will surface the error later.
		return policy
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(policyJSON, &payload); err != nil {
		return policy
	}

	if len(trustedBots) > 0 {
		// trusted-bots is a top-level key in the label_agent payload.
		// Convert []string to []interface{} for JSON compatibility.
		bots := make([]interface{}, len(trustedBots))
		for i, b := range trustedBots {
			bots[i] = b
		}
		payload["trusted-bots"] = bots
	}

	if len(trustedUsers) > 0 {
		// trusted-users is injected inside the allow-only object.
		// Convert []string to []interface{} for JSON compatibility.
		// If allow-only is absent, the injection is skipped and buildStrictLabelAgentPayload
		// will reject the payload when called with the missing allow-only key.
		users := make([]interface{}, len(trustedUsers))
		for i, u := range trustedUsers {
			users[i] = u
		}
		// Inject into allow-only object if present
		if allowOnly, ok := payload["allow-only"].(map[string]interface{}); ok {
			allowOnly["trusted-users"] = users
		}
	}

	return payload
}

func isValidAllowOnlyRepos(repos interface{}) bool {
	switch value := repos.(type) {
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(value))
		return trimmed == "all" || trimmed == "public"
	case []interface{}:
		if len(value) == 0 {
			return false
		}
		for _, entry := range value {
			if _, ok := entry.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// checkBoolFailure returns a non-nil error if the given raw response map
// contains field key set to false, extracting the "error" message if present.
func checkBoolFailure(raw map[string]interface{}, resultJSON []byte, key string) error {
	val, ok := raw[key].(bool)
	if !ok || val {
		return nil // field absent or true — not a failure
	}
	if message, msgOK := raw["error"].(string); msgOK && strings.TrimSpace(message) != "" {
		logWasm.Printf("label_agent response indicated failure: error=%s, response=%s", message, string(resultJSON))
		return fmt.Errorf("label_agent rejected policy: %s", message)
	}
	logWasm.Printf("label_agent response indicated non-success status: response=%s", string(resultJSON))
	return fmt.Errorf("label_agent returned non-success status")
}

func parseLabelAgentResponse(resultJSON []byte) (*LabelAgentResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(resultJSON, &raw); err != nil {
		logWasm.Printf("label_agent response parse error (invalid JSON): error=%v, raw=%s", err, string(resultJSON))
		return nil, fmt.Errorf("failed to unmarshal label_agent response: %w", err)
	}

	if err := checkBoolFailure(raw, resultJSON, "success"); err != nil {
		return nil, err
	}
	if err := checkBoolFailure(raw, resultJSON, "ok"); err != nil {
		return nil, err
	}
	if message, ok := raw["error"].(string); ok && strings.TrimSpace(message) != "" {
		logWasm.Printf("label_agent response contained error field: error=%s, response=%s", message, string(resultJSON))
		return nil, fmt.Errorf("label_agent returned error: %s", message)
	}

	var result LabelAgentResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		logWasm.Printf("label_agent response decode error: error=%v, response=%s", err, string(resultJSON))
		return nil, fmt.Errorf("failed to decode label_agent response: %w", err)
	}

	if strings.TrimSpace(result.DIFCMode) == "" {
		logWasm.Printf("label_agent response missing difc_mode: response=%s", string(resultJSON))
		return nil, fmt.Errorf("label_agent response missing difc_mode")
	}

	if _, err := difc.ParseEnforcementMode(result.DIFCMode); err != nil {
		logWasm.Printf("label_agent response invalid difc_mode=%q: error=%v, response=%s", result.DIFCMode, err, string(resultJSON))
		return nil, fmt.Errorf("invalid difc_mode from label_agent: %w", err)
	}

	return &result, nil
}

// LabelAgent calls the WASM module's label_agent function.
func (g *WasmGuard) LabelAgent(ctx context.Context, policy interface{}, backend BackendCaller, caps *difc.Capabilities) (*LabelAgentResult, error) {
	logWasm.Printf("LabelAgent called: guard=%s", g.name)

	if g.module.ExportedFunction("label_agent") == nil {
		return nil, fmt.Errorf("WASM guard does not export label_agent")
	}

	// Serialize access to the WASM module
	g.mu.Lock()
	defer g.mu.Unlock()

	// Update backend caller for this request
	g.backend = backend

	normalizedPolicy, err := normalizePolicyPayload(policy)
	if err != nil {
		logWasm.Printf("LabelAgent normalizePolicyPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}
	normalizedPolicyJSON, normalizeMarshalErr := json.Marshal(normalizedPolicy)
	if normalizeMarshalErr != nil {
		logWasm.Printf("LabelAgent normalized policy (marshal failed): guard=%s, error=%v", g.name, normalizeMarshalErr)
	} else {
		logWasm.Printf("LabelAgent normalized policy: guard=%s, policy=%s", g.name, string(normalizedPolicyJSON))
	}
	_ = caps

	input, err := buildStrictLabelAgentPayload(normalizedPolicy)
	if err != nil {
		logWasm.Printf("LabelAgent buildStrictLabelAgentPayload failed: guard=%s, error=%v", g.name, err)
		return nil, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal label_agent input: %w", err)
	}

	logWasm.Printf("LabelAgent input JSON (%d bytes): %s", len(inputJSON), string(inputJSON))

	resultJSON, err := g.callWasmFunction(ctx, "label_agent", inputJSON)
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

	resultLogJSON, resultMarshalErr := json.Marshal(result)
	if resultMarshalErr != nil {
		logWasm.Printf("LabelAgent parsed response (marshal failed): guard=%s, error=%v", g.name, resultMarshalErr)
	} else {
		logWasm.Printf("LabelAgent parsed response: guard=%s, response=%s", g.name, string(resultLogJSON))
	}

	return result, nil
}

// LabelResource calls the WASM module's label_resource function
func (g *WasmGuard) LabelResource(ctx context.Context, toolName string, args interface{}, backend BackendCaller, caps *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	logWasm.Printf("LabelResource called: toolName=%s, args=%+v", toolName, args)

	// Serialize access to the WASM module
	g.mu.Lock()
	defer g.mu.Unlock()

	// Update backend caller for this request
	g.backend = backend

	// Prepare input
	input := map[string]interface{}{
		"tool_name": toolName,
		"tool_args": args,
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, difc.OperationWrite, fmt.Errorf("failed to marshal input: %w", err)
	}

	logWasm.Printf("LabelResource input JSON (%d bytes): %s", len(inputJSON), string(inputJSON))

	// Call WASM function
	resultJSON, err := g.callWasmFunction(ctx, "label_resource", inputJSON)
	if err != nil {
		return nil, difc.OperationWrite, err
	}

	// Parse result
	var response map[string]interface{}
	if err := json.Unmarshal(resultJSON, &response); err != nil {
		return nil, difc.OperationWrite, fmt.Errorf("failed to unmarshal WASM response: %w", err)
	}

	return parseResourceResponse(response)
}

// LabelResponse calls the WASM module's label_response function
func (g *WasmGuard) LabelResponse(ctx context.Context, toolName string, result interface{}, backend BackendCaller, caps *difc.Capabilities) (difc.LabeledData, error) {
	logWasm.Printf("LabelResponse called: toolName=%s", toolName)

	// Serialize access to the WASM module
	g.mu.Lock()
	defer g.mu.Unlock()

	// Update backend caller for this request
	g.backend = backend

	// Prepare input
	input := map[string]interface{}{
		"tool_name":   toolName,
		"tool_result": result,
	}
	if state := GetRequestStateFromContext(ctx); state != nil {
		if stateMap, ok := state.(map[string]interface{}); ok {
			if toolArgs, hasToolArgs := stateMap["tool_args"]; hasToolArgs {
				input["tool_args"] = toolArgs
			}
		}
	}
	if caps != nil {
		input["capabilities"] = caps
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	// Call WASM function
	resultJSON, err := g.callWasmFunction(ctx, "label_response", inputJSON)
	if err != nil {
		return nil, err
	}

	// If empty result, return nil (no fine-grained labeling)
	if len(resultJSON) == 0 {
		return nil, nil
	}

	// Parse result - check for new path-based format first
	var responseMap map[string]interface{}
	if err := json.Unmarshal(resultJSON, &responseMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal WASM response: %w", err)
	}

	// Check for path-based labeling format (preferred, more efficient)
	if _, hasLabeledPaths := responseMap["labeled_paths"]; hasLabeledPaths {
		return parsePathLabeledResponse(resultJSON, result)
	}

	// Legacy format: check if it's a collection with "items"
	if items, ok := responseMap["items"].([]interface{}); ok && len(items) > 0 {
		return parseCollectionLabeledData(items)
	}

	// No fine-grained labeling
	return nil, nil
}

// parsePathLabeledResponse parses the new path-based labeling format
// This is more efficient as guards don't need to copy data, just return paths and labels
func parsePathLabeledResponse(responseJSON []byte, originalData interface{}) (difc.LabeledData, error) {
	pathLabels, err := difc.ParsePathLabels(responseJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse path labels: %w", err)
	}

	pld, err := difc.NewPathLabeledData(originalData, pathLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to apply path labels: %w", err)
	}

	// Convert to CollectionLabeledData for compatibility with existing filtering
	return pld.ToCollectionLabeledData(), nil
}

// callWasmFunction calls an exported function in the WASM module
func (g *WasmGuard) callWasmFunction(ctx context.Context, funcName string, inputJSON []byte) ([]byte, error) {
	fn := g.module.ExportedFunction(funcName)
	if fn == nil {
		return nil, fmt.Errorf("function %s not exported from WASM module", funcName)
	}

	mem := g.module.Memory()
	if mem == nil {
		return nil, fmt.Errorf("WASM module has no memory")
	}

	// Start with 4MB output buffer, can grow up to 16MB if needed
	initialOutputSize := uint32(4 * 1024 * 1024) // 4MB initial
	maxOutputSize := uint32(16 * 1024 * 1024)    // 16MB maximum
	maxInputSize := uint32(8 * 1024 * 1024)      // 8MB max input

	if uint32(len(inputJSON)) > maxInputSize {
		return nil, fmt.Errorf("input too large: %d bytes (max %d)", len(inputJSON), maxInputSize)
	}

	// Try with initial buffer size, retry with larger buffer if needed
	outputSize := initialOutputSize
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		result, requiredSize, err := g.tryCallWasmFunction(ctx, fn, mem, inputJSON, outputSize)
		if err != nil {
			return nil, err
		}

		// If we got a result, return it
		if result != nil {
			return result, nil
		}

		// Buffer was too small, check if we can grow
		if requiredSize == 0 {
			// Guard didn't tell us the required size, double the buffer
			requiredSize = outputSize * 2
		}

		if requiredSize > maxOutputSize {
			return nil, fmt.Errorf("guard requires buffer of %d bytes which exceeds maximum of %d bytes", requiredSize, maxOutputSize)
		}

		logWasm.Printf("Buffer too small (%d bytes), retrying with %d bytes", outputSize, requiredSize)
		outputSize = requiredSize
	}

	return nil, fmt.Errorf("failed after %d attempts, buffer size %d still insufficient", maxRetries, outputSize)
}

// tryCallWasmFunction attempts to call the WASM function with the given buffer size
// Returns (result, 0, nil) on success
// Returns (nil, requiredSize, nil) if buffer was too small
// Returns (nil, 0, error) on actual error
func (g *WasmGuard) tryCallWasmFunction(ctx context.Context, fn api.Function, mem api.Memory, inputJSON []byte, outputSize uint32) ([]byte, uint32, error) {
	inputSize := uint32(len(inputJSON))

	// Preferred path: use guard allocator if exported to avoid overlapping
	// host-managed buffers with guard heap allocations.
	allocFn := g.module.ExportedFunction("alloc")
	deallocFn := g.module.ExportedFunction("dealloc")
	if allocFn != nil {
		// Use a non-cancelable context for cleanup to avoid leaking WASM heap
		// allocations if the request context is canceled or times out.
		cleanupCtx := context.WithoutCancel(ctx)

		inputPtr, err := g.wasmAlloc(ctx, allocFn, inputSize)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to allocate WASM input buffer: %w", err)
		}
		defer g.wasmDealloc(cleanupCtx, deallocFn, inputPtr, inputSize)

		outputPtr, err := g.wasmAlloc(ctx, allocFn, outputSize)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to allocate WASM output buffer: %w", err)
		}
		defer g.wasmDealloc(cleanupCtx, deallocFn, outputPtr, outputSize)

		if !mem.Write(inputPtr, inputJSON) {
			return nil, 0, fmt.Errorf("failed to write input to WASM memory")
		}

		results, err := fn.Call(ctx,
			uint64(inputPtr),
			uint64(inputSize),
			uint64(outputPtr),
			uint64(outputSize))
		if err != nil {
			return nil, 0, fmt.Errorf("WASM function call failed: %w", err)
		}

		resultLen := int32(results[0])
		if resultLen == -2 {
			if sizeBytes, ok := mem.Read(outputPtr, 4); ok && len(sizeBytes) == 4 {
				requiredSize := uint32(sizeBytes[0]) | uint32(sizeBytes[1])<<8 | uint32(sizeBytes[2])<<16 | uint32(sizeBytes[3])<<24
				if requiredSize > 0 {
					return nil, requiredSize, nil
				}
			}
			return nil, 0, nil
		}

		if resultLen < 0 {
			return nil, 0, fmt.Errorf("WASM function returned error code: %d", resultLen)
		}

		if resultLen == 0 {
			return []byte{}, 0, nil
		}

		outputJSON, ok := mem.Read(outputPtr, uint32(resultLen))
		if !ok {
			return nil, 0, fmt.Errorf("failed to read output from WASM memory (len=%d)", resultLen)
		}

		// Copy out of WASM linear memory before deferred dealloc runs.
		resultCopy := append([]byte(nil), outputJSON...)
		return resultCopy, 0, nil
	}

	// Ensure memory is large enough for our buffers
	// Layout: [...guard memory...][input buffer][output buffer]
	requiredMemory := inputSize + outputSize + uint32(64*1024) // Extra 64KB for safety margin

	memSize := mem.Size()
	if memSize < requiredMemory {
		pages := (requiredMemory - memSize + 65535) / 65536 // Round up to pages
		_, success := mem.Grow(pages)
		if !success {
			return nil, 0, fmt.Errorf("failed to grow WASM memory from %d to %d bytes", memSize, requiredMemory)
		}
		memSize = mem.Size()
	}

	// Place buffers at end of memory
	outputPtr := memSize - outputSize
	inputPtr := outputPtr - inputSize

	// Write input to WASM memory
	if !mem.Write(inputPtr, inputJSON) {
		return nil, 0, fmt.Errorf("failed to write input to WASM memory")
	}

	// Call the WASM function
	results, err := fn.Call(ctx,
		uint64(inputPtr),
		uint64(inputSize),
		uint64(outputPtr),
		uint64(outputSize))
	if err != nil {
		return nil, 0, fmt.Errorf("WASM function call failed: %w", err)
	}

	// Check result
	resultLen := int32(results[0])

	// Error code -2 means "buffer too small"
	// The guard can optionally return the required size in the output buffer as a uint32
	if resultLen == -2 {
		// Try to read the required size from the output buffer (first 4 bytes as uint32)
		if sizeBytes, ok := mem.Read(outputPtr, 4); ok && len(sizeBytes) == 4 {
			requiredSize := uint32(sizeBytes[0]) | uint32(sizeBytes[1])<<8 | uint32(sizeBytes[2])<<16 | uint32(sizeBytes[3])<<24
			if requiredSize > 0 {
				return nil, requiredSize, nil
			}
		}
		// Guard didn't specify size, return 0 to trigger doubling
		return nil, 0, nil
	}

	// Other negative values are errors
	if resultLen < 0 {
		return nil, 0, fmt.Errorf("WASM function returned error code: %d", resultLen)
	}

	if resultLen == 0 {
		return []byte{}, 0, nil
	}

	// Read output from WASM memory
	outputJSON, ok := mem.Read(outputPtr, uint32(resultLen))
	if !ok {
		return nil, 0, fmt.Errorf("failed to read output from WASM memory (len=%d)", resultLen)
	}

	// Copy out of WASM linear memory to avoid aliasing with future calls.
	resultCopy := append([]byte(nil), outputJSON...)
	return resultCopy, 0, nil
}

func (g *WasmGuard) wasmAlloc(ctx context.Context, allocFn api.Function, size uint32) (uint32, error) {
	results, err := allocFn.Call(ctx, uint64(size))
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("alloc returned no result")
	}
	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, fmt.Errorf("alloc returned null pointer")
	}
	return ptr, nil
}

func (g *WasmGuard) wasmDealloc(ctx context.Context, deallocFn api.Function, ptr, size uint32) {
	if deallocFn == nil || ptr == 0 || size == 0 {
		return
	}
	if _, err := deallocFn.Call(ctx, uint64(ptr), uint64(size)); err != nil {
		logWasm.Printf("WASM dealloc failed: ptr=%d size=%d err=%v", ptr, size, err)
	}
}

// parseResourceResponse converts guard response to LabeledResource
func parseResourceResponse(response map[string]interface{}) (*difc.LabeledResource, difc.OperationType, error) {
	resourceData, ok := response["resource"].(map[string]interface{})
	if !ok {
		return nil, difc.OperationWrite, fmt.Errorf("invalid resource format in guard response")
	}

	resource := &difc.LabeledResource{}

	if desc, ok := resourceData["description"].(string); ok {
		resource.Description = desc
	}

	// Parse secrecy tags
	if secrecy, ok := resourceData["secrecy"].([]interface{}); ok {
		tags := make([]difc.Tag, 0, len(secrecy))
		for _, t := range secrecy {
			if tagStr, ok := t.(string); ok {
				tags = append(tags, difc.Tag(tagStr))
			}
		}
		resource.Secrecy = *difc.NewSecrecyLabelWithTags(tags)
	} else {
		resource.Secrecy = *difc.NewSecrecyLabel()
	}

	// Parse integrity tags
	if integrity, ok := resourceData["integrity"].([]interface{}); ok {
		tags := make([]difc.Tag, 0, len(integrity))
		for _, t := range integrity {
			if tagStr, ok := t.(string); ok {
				tags = append(tags, difc.Tag(tagStr))
			}
		}
		resource.Integrity = *difc.NewIntegrityLabelWithTags(tags)
	} else {
		resource.Integrity = *difc.NewIntegrityLabel()
	}

	// Parse operation type
	operation := difc.OperationWrite // default to most restrictive
	if opStr, ok := response["operation"].(string); ok {
		switch opStr {
		case "read":
			operation = difc.OperationRead
		case "write":
			operation = difc.OperationWrite
		case "read-write":
			operation = difc.OperationReadWrite
		}
	}

	return resource, operation, nil
}

// parseCollectionLabeledData converts an array of items to CollectionLabeledData
func parseCollectionLabeledData(items []interface{}) (*difc.CollectionLabeledData, error) {
	collection := &difc.CollectionLabeledData{
		Items: make([]difc.LabeledItem, 0, len(items)),
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		labeledItem := difc.LabeledItem{
			Data: itemMap["data"],
		}

		// Parse labels
		if labelsData, ok := itemMap["labels"].(map[string]interface{}); ok {
			labels := &difc.LabeledResource{}

			if desc, ok := labelsData["description"].(string); ok {
				labels.Description = desc
			}

			// Parse secrecy tags
			if secrecy, ok := labelsData["secrecy"].([]interface{}); ok {
				tags := make([]difc.Tag, 0, len(secrecy))
				for _, t := range secrecy {
					if tagStr, ok := t.(string); ok {
						tags = append(tags, difc.Tag(tagStr))
					}
				}
				labels.Secrecy = *difc.NewSecrecyLabelWithTags(tags)
			} else {
				labels.Secrecy = *difc.NewSecrecyLabel()
			}

			// Parse integrity tags
			if integrity, ok := labelsData["integrity"].([]interface{}); ok {
				tags := make([]difc.Tag, 0, len(integrity))
				for _, t := range integrity {
					if tagStr, ok := t.(string); ok {
						tags = append(tags, difc.Tag(tagStr))
					}
				}
				labels.Integrity = *difc.NewIntegrityLabelWithTags(tags)
			} else {
				labels.Integrity = *difc.NewIntegrityLabel()
			}

			labeledItem.Labels = labels
		}

		collection.Items = append(collection.Items, labeledItem)
	}

	return collection, nil
}

// Close releases WASM runtime resources
func (g *WasmGuard) Close(ctx context.Context) error {
	if g.module != nil {
		if err := g.module.Close(ctx); err != nil {
			logWasm.Printf("Error closing module: %v", err)
		}
	}
	if g.runtime != nil {
		return g.runtime.Close(ctx)
	}
	return nil
}
