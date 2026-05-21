package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// parseLabelAgentResponse validates and decodes the raw JSON returned by the
// WASM label_agent function into a LabelAgentResult.
func parseLabelAgentResponse(resultJSON []byte) (*LabelAgentResult, error) {
	var raw map[string]any
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

// parsePathLabeledResponse parses the path-based labeling format.
// This is more efficient as guards don't need to copy data, just return paths and labels.
func parsePathLabeledResponse(responseJSON []byte, originalData any) (difc.LabeledData, error) {
	logWasm.Printf("parsePathLabeledResponse: responseSize=%d", len(responseJSON))

	pathLabels, err := difc.ParsePathLabels(responseJSON)
	if err != nil {
		logWasm.Printf("parsePathLabeledResponse: failed to parse path labels: %v", err)
		return nil, fmt.Errorf("failed to parse path labels: %w", err)
	}
	logWasm.Printf("parsePathLabeledResponse: parsed %d path labels", len(pathLabels.LabeledPaths))

	pld, err := difc.NewPathLabeledData(originalData, pathLabels)
	if err != nil {
		logWasm.Printf("parsePathLabeledResponse: failed to apply path labels: %v", err)
		return nil, fmt.Errorf("failed to apply path labels: %w", err)
	}

	// Convert to CollectionLabeledData for compatibility with existing filtering
	result := pld.ToCollectionLabeledData()
	logWasm.Printf("parsePathLabeledResponse: converted to CollectionLabeledData successfully")
	return result, nil
}

// isWasmTrap reports whether err represents a WASM execution trap that should
// permanently poison the guard. Normal process exits (exit code 0, e.g. TinyGo
// init) are NOT considered traps. A non-zero exit code is treated as a trap.
// As a fallback for wazero execution faults (e.g. Rust panic → unreachable),
// the function also matches on wazero's "wasm error:" message prefix
// (as of wazero v1.x; re-verify on wazero upgrades).
func isWasmTrap(err error) bool {
	if err == nil {
		return false
	}
	// A normal WASI process exit (exit code 0) is not a trap — don't poison the guard.
	var exitErr *sys.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() != 0
	}
	// Fallback for wazero execution traps (e.g. Rust panic → unreachable).
	return strings.Contains(err.Error(), "wasm error:")
}

// callWasmFunction calls an exported function in the WASM module.
// Precondition: g.mu must be held by the caller. All public methods
// (LabelAgent, LabelResource, LabelResponse) hold g.mu for their entire
// duration, satisfying this requirement.
func (g *WasmGuard) callWasmFunction(ctx context.Context, funcName string, inputJSON []byte) ([]byte, error) {
	// If the module has already trapped, refuse further calls immediately.
	// A WASM trap may corrupt the module's internal state (e.g. the global
	// policy context stored by label_agent), so all subsequent calls are
	// unsafe until the guard is reloaded.
	if g.failed {
		return nil, fmt.Errorf("WASM guard '%s' is unavailable after a previous trap: %w", g.name, g.failedErr)
	}

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

	// Adaptive output buffer strategy:
	//
	// WASM guards communicate buffer-too-small via a return code convention:
	//   -2  → buffer too small; first 4 bytes of the output buffer MAY contain the
	//          required size as a little-endian uint32. If present and > 0, we use
	//          that size for the next attempt; otherwise we double the buffer.
	//   < 0 → other error (returned as-is to the caller).
	//   >= 0 → success; value is the number of bytes written to the output buffer.
	//
	// We retry up to maxRetries times, growing from 4MB toward the 16MB ceiling.
	// A WASM trap (e.g. "wasm error: unreachable" from a Rust panic) permanently
	// marks the guard as failed because the module's internal state may be corrupt.
	outputSize := initialOutputSize
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		result, requiredSize, err := g.tryCallWasmFunction(ctx, fn, mem, inputJSON, outputSize)
		if err != nil {
			if isWasmTrap(err) {
				// A WASM trap (e.g. unreachable from a Rust panic) leaves the
				// module in an undefined state. Log it prominently and mark the
				// guard as permanently failed so callers get a clear error.
				logger.LogError("backend", "WASM guard trap: guard=%s, func=%s, error=%v", g.name, funcName, err)
				g.failed = true
				g.failedErr = err
			}
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

// tryCallWasmFunction attempts to call the WASM function with the given buffer size.
// Returns (result, 0, nil) on success.
// Returns (nil, requiredSize, nil) if buffer was too small.
// Returns (nil, 0, error) on actual error.
func (g *WasmGuard) tryCallWasmFunction(ctx context.Context, fn api.Function, mem api.Memory, inputJSON []byte, outputSize uint32) ([]byte, uint32, error) {
	inputSize := uint32(len(inputJSON))
	functionName := "<unknown>"
	if def := fn.Definition(); def != nil && def.Name() != "" {
		functionName = def.Name()
	}
	logWasm.Printf("tryCallWasmFunction: guard=%s, func=%s, inputSize=%d, outputSize=%d", g.name, functionName, inputSize, outputSize)

	// Preferred path: use guard allocator only when both allocator exports are
	// available, to avoid overlapping host-managed buffers with guard heap
	// allocations and to ensure allocated memory can be freed.
	allocFn := g.module.ExportedFunction("alloc")
	deallocFn := g.module.ExportedFunction("dealloc")
	if allocFn != nil && deallocFn != nil {
		logWasm.Printf("Using guard allocator path: guard=%s", g.name)
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
			if requiredSize, ok := mem.ReadUint32Le(outputPtr); ok && requiredSize > 0 {
				return nil, requiredSize, nil
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

	if !g.warnedDirectMemoryPath {
		logger.LogWarn("guard", "WASM guard '%s' is using the direct memory fallback for %s without alloc/dealloc exports; export alloc/dealloc to avoid linear-memory overlap risks", g.name, functionName)
		g.warnedDirectMemoryPath = true
	}
	logWasm.Printf("Using direct memory path: guard=%s, inputSize=%d, outputSize=%d", g.name, inputSize, outputSize)

	// Ensure memory is large enough for our buffers
	// Layout: [...guard memory...][input buffer][output buffer]
	// wazero enforces only the module's declared linear-memory maximum, so guard
	// authors should set an explicit max page count in the WASM binary when they
	// need a hard cap. The gateway does not impose an additional host-side limit,
	// so a guard that declares an excessively large maximum can still consume
	// correspondingly large host memory if it grows toward that maximum.
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
		if requiredSize, ok := mem.ReadUint32Le(outputPtr); ok && requiredSize > 0 {
			return nil, requiredSize, nil
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

// wasmAlloc allocates a buffer in WASM linear memory using the guard's exported alloc function.
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
	logWasm.Printf("wasmAlloc: guard=%s, size=%d, ptr=%d", g.name, size, ptr)
	return ptr, nil
}

// wasmDealloc frees a WASM linear-memory allocation via the guard's exported dealloc function.
func (g *WasmGuard) wasmDealloc(ctx context.Context, deallocFn api.Function, ptr, size uint32) {
	if deallocFn == nil || ptr == 0 || size == 0 {
		return
	}
	if _, err := deallocFn.Call(ctx, uint64(ptr), uint64(size)); err != nil {
		logWasm.Printf("WASM dealloc failed: ptr=%d size=%d err=%v", ptr, size, err)
	}
}

// parseResourceResponse converts the guard label_resource response to a LabeledResource.
func parseResourceResponse(response map[string]any) (*difc.LabeledResource, difc.OperationType, error) {
	resourceData, ok := response["resource"].(map[string]any)
	if !ok {
		return nil, difc.OperationWrite, fmt.Errorf("invalid resource format in guard response")
	}

	resource := &difc.LabeledResource{}

	if desc, ok := resourceData["description"].(string); ok {
		resource.Description = desc
	}

	// Parse secrecy tags
	if secrecy, ok := resourceData["secrecy"].([]any); ok {
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
	if integrity, ok := resourceData["integrity"].([]any); ok {
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

	logWasm.Printf("Parsed resource response: description=%q, operation=%v", resource.Description, operation)
	return resource, operation, nil
}

// parseCollectionLabeledData converts an array of items to CollectionLabeledData.
func parseCollectionLabeledData(items []any) (*difc.CollectionLabeledData, error) {
	logWasm.Printf("parseCollectionLabeledData: itemCount=%d", len(items))
	collection := &difc.CollectionLabeledData{
		Items: make([]difc.LabeledItem, 0, len(items)),
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		labeledItem := difc.LabeledItem{
			Data: itemMap["data"],
		}

		// Parse labels
		if labelsData, ok := itemMap["labels"].(map[string]any); ok {
			labels := &difc.LabeledResource{}

			if desc, ok := labelsData["description"].(string); ok {
				labels.Description = desc
			}

			// Parse secrecy tags
			if secrecy, ok := labelsData["secrecy"].([]any); ok {
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
			if integrity, ok := labelsData["integrity"].([]any); ok {
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

	logWasm.Printf("parseCollectionLabeledData: parsed %d labeled items from %d input items", len(collection.Items), len(items))
	return collection, nil
}
