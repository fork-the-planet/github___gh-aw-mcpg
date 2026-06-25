package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/middleware"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// launchResult stores the result of a backend server launch
type launchResult struct {
	serverID string
	err      error
	duration time.Duration
}

// registerToolWithoutValidation registers a tool with the SDK server using the Server.AddTool
// method (not the sdk.AddTool function) to bypass JSON Schema validation. This allows including
// InputSchema from backends that use different JSON Schema versions (e.g., draft-07) without
// validation errors, which is critical for clients to understand tool parameters.
//
// # Three-argument handler convention
//
// Throughout this package, tool handlers use a three-argument form:
//
//	func(ctx context.Context, req *sdk.CallToolRequest, state interface{}) (*sdk.CallToolResult, interface{}, error)
//
// This differs from the SDK's native two-argument form. The extra parameters serve
// two internal purposes:
//   - state interface{}: reserved for the jq middleware pipeline (currently always nil at
//     the call site; middleware may propagate state between pre- and post-processing steps).
//   - second return value interface{}: carries intermediate data for the DIFC write-sink
//     logger so it can record the raw backend result alongside the final tool result.
//
// The wrapper in this function adapts the three-argument form back to the SDK's two-argument
// form when registering with the SDK server.
//
// NOTE: The Server.AddTool method (used here) does not validate tool arguments against the
// input schema at call time, whereas the package-level sdk.AddTool function does. The method
// does require that InputSchema is non-nil and has type "object" (enforced since v1.5.0), but
// it does not validate the argument values — that responsibility belongs to the caller.
// This distinction relies on internal SDK behaviour and must be re-verified on every SDK upgrade.
// Verified correct for go-sdk v1.6.1 (see server.go:Server.AddTool vs AddTool[In,Out]).
func registerToolWithoutValidation(server *sdk.Server, tool *sdk.Tool, handler func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error)) {
	server.AddTool(tool, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		result, _, err := handler(ctx, req, nil)
		return result, err
	})
}

func getToolResponseFilter(cfg *config.Config, serverID, toolName string) string {
	if cfg == nil {
		return ""
	}
	serverCfg, ok := cfg.Servers[serverID]
	if !ok || serverCfg == nil {
		return ""
	}
	return strings.TrimSpace(serverCfg.ToolResponseFilters[toolName])
}

// registerAllTools fetches and registers tools from all backend servers
func (us *UnifiedServer) registerAllTools() error {
	logger.LogInfo("backend", "Starting tool registration for %d backends", len(us.launcher.ServerIDs()))

	// Only register sys tools if DIFC is enabled
	// When DIFC is disabled (default), sys tools are not needed
	if us.enableDIFC {
		logger.LogInfo("backend", "DIFC enabled: registering sys tools...")
		if err := us.registerSysTools(); err != nil {
			logger.LogWarn("backend", "Failed to register sys tools: %v", err)
		}
	} else {
		logger.LogInfo("backend", "DIFC disabled: skipping sys tools registration")
	}

	serverIDs := us.launcher.ServerIDs()

	if us.sequentialLaunch {
		// Launch servers sequentially
		return us.registerAllToolsSequential(serverIDs)
	} else {
		// Launch servers in parallel (default behavior)
		return us.registerAllToolsParallel(serverIDs)
	}
}

// registrationErrors tracks backend servers that failed tool registration and
// logs a summary when finish is called. Both the sequential and parallel
// registration strategies use this type so failure-tracking semantics are
// defined in one place.
type registrationErrors struct {
	failed []string
	total  int
}

func (e *registrationErrors) record(serverID string) {
	e.failed = append(e.failed, serverID)
}

func (e *registrationErrors) finish() {
	if len(e.failed) > 0 {
		logger.LogError("backend", "Tool registration incomplete: %d of %d backends failed: %v — agents will not see tools from these servers",
			len(e.failed), e.total, e.failed)
	}
}

// registerAllToolsSequential registers tools from backend servers sequentially
func (us *UnifiedServer) registerAllToolsSequential(serverIDs []string) error {
	logUnified.Printf("Registering tools sequentially from %d backends", len(serverIDs))

	errs := &registrationErrors{total: len(serverIDs)}
	for _, serverID := range serverIDs {
		logUnified.Printf("Registering tools from backend: %s", serverID)
		if err := us.registerToolsFromBackend(serverID); err != nil {
			logger.LogError("backend", "Failed to register tools from %s: %v", serverID, err)
			errs.record(serverID)
		}
	}

	errs.finish()
	logUnified.Printf("Tool registration complete: total tools=%d", len(us.tools))
	return nil
}

// registerAllToolsParallel registers tools from backend servers in parallel
func (us *UnifiedServer) registerAllToolsParallel(serverIDs []string) error {
	logUnified.Printf("Registering tools in parallel from %d backends", len(serverIDs))

	var wg sync.WaitGroup
	results := make(chan launchResult, len(serverIDs))

	// Launch each server in its own goroutine
	for _, serverID := range serverIDs {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()

			startTime := time.Now()
			err := us.registerToolsFromBackend(sid)
			duration := time.Since(startTime)

			results <- launchResult{
				serverID: sid,
				err:      err,
				duration: duration,
			}
		}(serverID)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(results)

	// Collect and log results
	successCount := 0
	errs := &registrationErrors{total: len(serverIDs)}
	for result := range results {
		if result.err != nil {
			logger.LogErrorToServer(result.serverID, "backend", "Failed to register tools from %s (took %v): %v", result.serverID, result.duration, result.err)
			errs.record(result.serverID)
		} else {
			logUnified.Printf("Successfully registered tools from %s (took %v)", result.serverID, result.duration)
			logger.LogInfoToServer(result.serverID, "backend", "Successfully registered tools from %s (took %v)", result.serverID, result.duration)
			successCount++
		}
	}

	errs.finish()

	logger.LogInfo("backend", "Tool registration complete: %d succeeded, %d failed, total tools=%d", successCount, len(errs.failed), len(us.tools))
	return nil
}

// registerToolsFromBackend registers tools from a specific backend with <server>___<tool> naming
func (us *UnifiedServer) registerToolsFromBackend(serverID string) error {
	logUnified.Printf("Registering tools from backend: %s", serverID)

	// Get connection to backend
	conn, err := launcher.GetOrLaunch(us.launcher, serverID)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Surface backend server info from the MCP initialize handshake for diagnostics.
	// This helps debug compatibility issues between the gateway and specific backends.
	if name, version := conn.ServerInfo(); name != "" {
		logger.LogInfoToServer(serverID, "backend", "Backend server info: name=%s, version=%s", name, version)
	} else {
		logger.LogInfoToServer(serverID, "backend", "Backend server info unavailable (no SDK session or server omitted serverInfo)")
	}

	// List tools from backend
	result, err := conn.SendRequestWithServerID(context.Background(), "tools/list", nil, serverID)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Check if the backend returned an error
	if result.Error != nil {
		return fmt.Errorf("backend error listing tools: code=%d, message=%s", result.Error.Code, result.Error.Message)
	}

	// Parse the result
	var listResult struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
			Annotations *sdk.ToolAnnotations   `json:"annotations,omitempty"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result.Result, &listResult); err != nil {
		return fmt.Errorf("failed to parse tools: %w", err)
	}

	// Filter tools by the server's allowed-tools list (if configured).
	// This prevents non-allowed tools from appearing in tools/list responses
	// and is defense-in-depth alongside the callBackendTool enforcement.
	if allowedSet, ok := us.allowedToolSets[serverID]; ok && len(allowedSet) > 0 {
		n := 0
		for _, tool := range listResult.Tools {
			if allowedSet[tool.Name] {
				listResult.Tools[n] = tool
				n++
			}
		}
		if n < len(listResult.Tools) {
			logger.LogInfo("backend", "[allowed-tools] Filtered %d tools from %s: keeping %d of %d",
				len(listResult.Tools)-n, serverID, n, len(listResult.Tools))
		}
		listResult.Tools = listResult.Tools[:n]
	}

	// Collect tools for logging
	toolsForLogging := make([]logger.ToolInfo, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		toolsForLogging = append(toolsForLogging, logger.ToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}

	// Log tools to tools.json
	logger.LogToolsForServer(serverID, toolsForLogging)

	// Register each tool with prefixed name
	toolNames := []string{}
	for _, tool := range listResult.Tools {
		prefixedName := fmt.Sprintf("%s___%s", serverID, tool.Name)
		toolDesc := fmt.Sprintf("[%s] %s", serverID, tool.Description)
		logName := fmt.Sprintf("%s-%s", serverID, tool.Name)
		toolNames = append(toolNames, logName)

		// Normalize the input schema to fix common validation issues
		normalizedSchema := mcp.NormalizeInputSchema(tool.InputSchema, prefixedName)

		// Store tool info for routed mode
		us.toolsMu.Lock()
		us.tools[prefixedName] = &ToolInfo{
			Name:        prefixedName,
			Description: toolDesc,
			InputSchema: normalizedSchema,
			Annotations: tool.Annotations,
			BackendID:   serverID,
		}
		us.toolsMu.Unlock()

		// Create a closure to capture serverID and toolName
		serverIDCopy := serverID
		toolNameCopy := tool.Name

		// Create the handler function
		handler := func(ctx context.Context, req *sdk.CallToolRequest, args interface{}) (*sdk.CallToolResult, interface{}, error) {
			// Extract arguments from the request params (not the args parameter which is SDK internal state)
			toolArgs, err := mcp.ParseToolArguments(req)
			if err != nil {
				logger.LogError("client", "Failed to unmarshal tool arguments, tool=%s, error=%v", toolNameCopy, err)
				return mcp.NewErrorCallToolResult(err)
			}

			// Log the MCP tool call request
			sessionID := us.getSessionID(ctx)
			argsJSON, _ := json.Marshal(toolArgs)
			sanitizedArgs := sanitize.SanitizeString(string(argsJSON))
			logger.LogInfo("client", "MCP tool call request, session=%s, tool=%s, args=%s", sessionID, toolNameCopy, sanitizedArgs)

			// Check session is initialized
			if err := us.requireSession(ctx); err != nil {
				logger.LogError("client", "MCP tool call failed: session not initialized, session=%s, tool=%s", sessionID, toolNameCopy)
				return mcp.NewErrorCallToolResult(err)
			}

			result, data, err := us.callBackendTool(ctx, serverIDCopy, toolNameCopy, toolArgs)

			// Log the MCP tool call response
			if err != nil {
				logger.LogError("client", "MCP tool call error, session=%s, tool=%s, error=%v", sessionID, toolNameCopy, err)
			} else {
				logger.LogInfo("client", "MCP tool call response, session=%s, tool=%s, result=%s", sessionID, toolNameCopy, sanitize.MarshalAndSanitize(data))
			}

			return result, data, err
		}

		// Wrap handler with jqschema middleware if applicable
		finalHandler := handler
		if middleware.ShouldApplyMiddleware(prefixedName) {
			if filter := getToolResponseFilter(us.cfg, serverIDCopy, toolNameCopy); filter != "" {
				finalHandler = middleware.WrapToolHandlerWithFilter(handler, prefixedName, us.payloadDir, us.payloadPathPrefix, us.payloadSizeThreshold, us.getSessionID, filter)
			} else {
				finalHandler = middleware.WrapToolHandler(handler, prefixedName, us.payloadDir, us.payloadPathPrefix, us.payloadSizeThreshold, us.getSessionID)
			}
		}

		// Store handler for routed mode to reuse
		us.toolsMu.Lock()
		us.tools[prefixedName].Handler = finalHandler
		us.toolsMu.Unlock()

		registerToolWithoutValidation(us.server, &sdk.Tool{
			Name:        prefixedName,
			Description: toolDesc,
			InputSchema: normalizedSchema, // Include the schema for clients to understand parameters
			Annotations: tool.Annotations,
		}, finalHandler)

		logUnified.Printf("Registered tool: %s", logName)
	}

	logUnified.Printf("Registered %d tools from %s: %s", len(listResult.Tools), serverID, strings.Join(toolNames, ", "))

	// Register prompts from this backend. Prompt support is optional; failures are
	// logged but do not cause tool registration to fail.
	if err := us.registerPromptsFromBackend(context.Background(), serverID, conn); err != nil {
		logger.LogWarn("backend", "Failed to register prompts from %s (non-fatal): %v", serverID, err)
	}

	return nil
}

// registerPromptsFromBackend registers prompts from a specific backend with <server>___<prompt>
// naming, mirroring the tool registration convention. Prompt capability is optional in the MCP
// spec; backends that do not support prompts/list will return an error that is treated as a
// graceful skip rather than a hard failure.
func (us *UnifiedServer) registerPromptsFromBackend(ctx context.Context, serverID string, conn *mcp.Connection) error {
	// Only call prompts/list on backends that explicitly declared prompt support in
	// their initialize response. For SDK-based connections (streamable, SSE, stdio),
	// an unsupported prompts/list request can return EOF which the SDK interprets as
	// a session close, breaking subsequent tool calls on the same connection.
	// Plain JSON-RPC connections return false here too; their initialize response is
	// not parsed into typed capabilities, so we cannot safely detect support.
	if !conn.BackendHasPromptsCapability() {
		logUnified.Printf("Backend %s does not declare prompts capability (skipping)", serverID)
		return nil
	}

	// List prompts from backend
	result, err := conn.SendRequestWithServerID(ctx, "prompts/list", nil, serverID)
	if err != nil {
		// Many backends do not implement prompts — treat as a graceful skip.
		logUnified.Printf("Backend %s does not support prompts/list (skipping): %v", serverID, err)
		return nil
	}

	// A JSON-RPC error from the backend also means prompts are unavailable.
	if result.Error != nil {
		logUnified.Printf("Backend %s returned error for prompts/list (skipping): code=%d, message=%s",
			serverID, result.Error.Code, result.Error.Message)
		return nil
	}

	// Parse the prompt list.
	var listResult struct {
		Prompts []*sdk.Prompt `json:"prompts"`
	}
	if err := json.Unmarshal(result.Result, &listResult); err != nil {
		return fmt.Errorf("failed to parse prompts from %s: %w", serverID, err)
	}

	if len(listResult.Prompts) == 0 {
		logUnified.Printf("Backend %s has no prompts to register", serverID)
		return nil
	}

	// Register each prompt with a prefixed name so front-end clients can
	// distinguish prompts from different backends.
	promptNames := make([]string, 0, len(listResult.Prompts))
	for _, prompt := range listResult.Prompts {
		prefixedName := fmt.Sprintf("%s___%s", serverID, prompt.Name)
		promptDesc := fmt.Sprintf("[%s] %s", serverID, prompt.Description)
		promptNames = append(promptNames, prompt.Name)

		serverIDCopy := serverID
		promptNameCopy := prompt.Name

		sdkPrompt := &sdk.Prompt{
			Name:        prefixedName,
			Description: promptDesc,
			Arguments:   prompt.Arguments,
		}

		us.server.AddPrompt(sdkPrompt, func(ctx context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			sessionID := us.getSessionID(ctx)
			backendConn, connErr := launcher.GetOrLaunchForSession(us.launcher, serverIDCopy, sessionID)
			if connErr != nil {
				return nil, fmt.Errorf("failed to connect to backend %s: %w", serverIDCopy, connErr)
			}

			params := map[string]interface{}{
				"name":      promptNameCopy,
				"arguments": req.Params.Arguments,
			}

			response, reqErr := backendConn.SendRequestWithServerID(ctx, "prompts/get", params, serverIDCopy)
			if reqErr != nil {
				return nil, reqErr
			}
			if response.Error != nil {
				return nil, fmt.Errorf("backend error for prompts/get %s/%s: code=%d, message=%s",
					serverIDCopy, promptNameCopy, response.Error.Code, response.Error.Message)
			}

			var promptResult sdk.GetPromptResult
			if unmarshErr := json.Unmarshal(response.Result, &promptResult); unmarshErr != nil {
				return nil, fmt.Errorf("failed to parse prompt result: %w", unmarshErr)
			}
			return &promptResult, nil
		})

		logUnified.Printf("Registered prompt: %s___%s", serverID, promptNameCopy)
	}

	logUnified.Printf("Registered %d prompts from %s: %s", len(listResult.Prompts), serverID, strings.Join(promptNames, ", "))
	return nil
}
func (us *UnifiedServer) registerSysTool(name, description string, inputSchema map[string]interface{}, handler func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error)) {
	// Store tool info internally only -- sys tools are intentionally NOT registered
	// with the MCP SDK server and therefore never appear in tools/list.
	us.toolsMu.Lock()
	us.tools[name] = &ToolInfo{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		BackendID:   "sys",
		Handler:     handler,
	}
	us.toolsMu.Unlock()
}

// callSysServer is a helper that directly dispatches sys tools to SysServer.
func (us *UnifiedServer) callSysServer(toolName string) (interface{}, error) {
	switch toolName {
	case "sys_init":
		return us.sysServer.SysInit()
	case "sys_list_servers":
		return us.sysServer.ListServers()
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (us *UnifiedServer) callAndLogSysTool(sessionID, operationName, sysToolName string) (*sdk.CallToolResult, interface{}, error) {
	result, err := us.callSysServer(sysToolName)
	if err != nil {
		logger.LogError("client", "MCP %s call failed, session=%s, error=%v", operationName, sessionID, err)
		return mcp.NewErrorCallToolResult(err)
	}

	logger.LogInfo("client", "MCP %s response, session=%s, result=%s", operationName, sessionID, sanitize.MarshalAndSanitize(result))
	return nil, result, nil
}

// registerSysTools registers built-in sys tools
func (us *UnifiedServer) registerSysTools() error {
	// Register sys_init tool using helper
	us.registerSysTool(
		"sys___init",
		"Initialize the MCPG system and get available MCP servers",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"token": map[string]interface{}{
					"type":        "string",
					"description": "Authentication token for session initialization (can be empty for first call)",
				},
			},
		},
		us.sysInitHandler,
	)

	// Register sys_list_servers tool using helper
	us.registerSysTool(
		"sys___list_servers",
		"List all configured MCP backend servers",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		us.sysListServersHandler,
	)

	logUnified.Printf("Registered 2 sys tools")
	return nil
}

// buildAllowedToolSets builds a per-server map[string]bool set for O(1) lookup.
// Servers with no Tools list are not added to the map, which signals that all
// tools are permitted. If the Tools list contains a "*" entry anywhere, the
// server is treated the same as having no list (all tools allowed).
func buildAllowedToolSets(cfg *config.Config) map[string]map[string]bool {
	sets := make(map[string]map[string]bool)
	if cfg == nil {
		return sets
	}
	for serverID, serverCfg := range cfg.Servers {
		if len(serverCfg.Tools) > 0 {
			// Treat "*" anywhere in the list as "allow all" — skip adding to the filter map
			if hasWildcard(serverCfg.Tools) {
				logger.LogInfo("backend", "[allowed-tools] Wildcard \"*\" configured for %s: allowing all tools", serverID)
				continue
			}
			set := make(map[string]bool, len(serverCfg.Tools))
			for _, t := range serverCfg.Tools {
				set[t] = true
			}
			sets[serverID] = set
			logUnified.Printf("Built allowed tool set for server %s: %d tool(s) permitted", serverID, len(set))
		}
	}
	logUnified.Printf("Built allowed tool sets: %d server(s) with tool restrictions", len(sets))
	return sets
}

// hasWildcard reports whether the tools list contains a "*" entry.
func hasWildcard(tools []string) bool {
	for _, t := range tools {
		if t == "*" {
			return true
		}
	}
	return false
}
