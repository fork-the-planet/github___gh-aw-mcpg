package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/launcher"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	"github.com/github/gh-aw-mcpg/internal/version"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logUnified = logger.New("server:unified")

const wasmGuardsDirEnvVar = "MCP_GATEWAY_WASM_GUARDS_DIR"

// MCPProtocolVersion is the MCP protocol version supported by this gateway
const MCPProtocolVersion = mcp.MCPProtocolVersion

// MCPGatewaySpecVersion is the MCP Gateway Specification version this implementation conforms to
const MCPGatewaySpecVersion = "1.8.0"

// Session represents a MCPG session
type Session struct {
	Token     string
	SessionID string
	StartTime time.Time
	GuardInit map[string]*GuardSessionState
}

// GuardSessionState stores label_agent initialization state for a guard within a session.
type GuardSessionState struct {
	Initialized      bool
	PolicyHash       string
	PolicySource     string
	DIFCMode         difc.EnforcementMode
	NormalizedPolicy map[string]interface{}
}

// ServerStatus represents the health status of a backend server
type ServerStatus struct {
	Status string `json:"status"` // "running" | "stopped" | "error"
	Uptime int    `json:"uptime"` // seconds since server was launched
}

// SessionIDContextKey is used to store MCP session ID in context
// This is re-exported from mcp package for backward compatibility
const SessionIDContextKey = mcp.SessionIDContextKey

// ToolInfo stores metadata about a registered tool
type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Annotations *sdk.ToolAnnotations
	BackendID   string // Which backend this tool belongs to
	Handler     func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error)
}

// UnifiedServer implements a unified MCP server that aggregates multiple backend servers
type UnifiedServer struct {
	launcher             *launcher.Launcher
	sysServer            *SysServer
	ctx                  context.Context
	server               *sdk.Server
	sessions             map[string]*Session // mcp-session-id -> Session
	sessionMu            sync.RWMutex
	tools                map[string]*ToolInfo // prefixed tool name -> tool info
	toolsMu              sync.RWMutex
	sequentialLaunch     bool   // When true, launches MCP servers sequentially during startup. Default is false (parallel launch).
	payloadDir           string // Base directory for storing large payload files (segmented by session ID)
	payloadPathPrefix    string // Path prefix to use when returning payloadPath to clients (allows remapping host paths to client/agent container paths)
	payloadSizeThreshold int    // Size threshold (in bytes) for storing payloads to disk. Payloads larger than this are stored to disk, smaller ones are returned inline.

	// DIFC components
	guardRegistry *guard.Registry
	agentRegistry *difc.AgentRegistry
	capabilities  *difc.Capabilities
	evaluator     *difc.Evaluator
	enableDIFC    bool // When true, DIFC enforcement and session requirement are enabled

	// Configuration reference for guard loading
	cfg *config.Config

	// Shutdown state tracking
	isShutdown     bool
	shutdownMu     sync.RWMutex
	shutdownOnce   sync.Once
	httpShutdownFn func(context.Context) error // Called during /close to drain in-flight HTTP requests

	// Testing support - when true, skips os.Exit() call
	testMode bool
}

// NewUnified creates a new unified MCP server
func NewUnified(ctx context.Context, cfg *config.Config) (*UnifiedServer, error) {
	logUnified.Printf("Creating new unified server: sequentialLaunch=%v, servers=%d", cfg.SequentialLaunch, len(cfg.Servers))
	l := launcher.New(ctx, cfg)

	// Get payload directory from config, with fallback to default
	payloadDir := config.DefaultPayloadDir
	if cfg.Gateway != nil && cfg.Gateway.PayloadDir != "" {
		payloadDir = cfg.Gateway.PayloadDir
	}

	// Get payload path prefix from config (empty by default)
	payloadPathPrefix := ""
	if cfg.Gateway != nil && cfg.Gateway.PayloadPathPrefix != "" {
		payloadPathPrefix = cfg.Gateway.PayloadPathPrefix
	}

	// Get payload size threshold from config, with fallback to default
	payloadSizeThreshold := config.DefaultPayloadSizeThreshold
	if cfg.Gateway != nil && cfg.Gateway.PayloadSizeThreshold > 0 {
		payloadSizeThreshold = cfg.Gateway.PayloadSizeThreshold
	}
	logUnified.Printf("Payload configuration: dir=%s, pathPrefix=%s, sizeThreshold=%d bytes (%.2f KB)",
		payloadDir, payloadPathPrefix, payloadSizeThreshold, float64(payloadSizeThreshold)/1024)

	// Parse DIFC enforcement mode
	difcMode, err := difc.ParseEnforcementMode(cfg.DIFCMode)
	if err != nil {
		// Default to strict mode if not specified or invalid
		difcMode = difc.EnforcementStrict
	}

	us := &UnifiedServer{
		launcher:             l,
		sysServer:            NewSysServer(l.ServerIDs()),
		ctx:                  ctx,
		sessions:             make(map[string]*Session),
		tools:                make(map[string]*ToolInfo),
		sequentialLaunch:     cfg.SequentialLaunch,
		payloadDir:           payloadDir,
		payloadPathPrefix:    payloadPathPrefix,
		payloadSizeThreshold: payloadSizeThreshold,

		// Initialize DIFC components
		guardRegistry: guard.NewRegistry(),
		agentRegistry: difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:  difc.NewCapabilities(),
		evaluator:     difc.NewEvaluatorWithMode(difcMode),
		cfg:           cfg, // Store config for guard loading
	}

	// Create MCP server with logger
	server := sdk.NewServer(&sdk.Implementation{
		Name:    "awmg-unified",
		Version: version.Get(),
	}, &sdk.ServerOptions{
		Logger: logger.NewSlogLoggerWithHandler(logUnified),
	})

	us.server = server
	us.logWASMGuardsDirConfiguration()

	// Register guards for all backends
	for _, serverID := range l.ServerIDs() {
		if err := us.registerGuard(serverID); err != nil {
			return nil, fmt.Errorf("failed to register guard for server %q: %w", serverID, err)
		}
	}

	// Auto-enable DIFC if any non-noop guard was registered, a global policy override
	// exists, or any server has per-server guard policies configured.
	if !us.enableDIFC && (us.guardRegistry.HasNonNoopGuard() || cfg.GuardPolicy != nil || hasServerGuardPolicies(cfg)) {
		us.enableDIFC = true
		logUnified.Printf("Auto-enabled DIFC: non-noop guard, global policy, or per-server guard policies detected")
	}

	// Log guards status early (before backend launch which may take time)
	if us.enableDIFC {
		log.Printf("Guards enforcement enabled with mode: %s", cfg.DIFCMode)
	} else {
		log.Println("Guards enforcement disabled (sessions auto-created for standard MCP client compatibility)")
	}

	// Register aggregated tools from all backends
	if err := us.registerAllTools(); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}

	logUnified.Printf("Unified server created successfully with %d tools", len(us.tools))
	return us, nil
}

// executeBackendToolCall executes a backend MCP tool call and returns the raw result.
// This helper consolidates the common pattern of:
// 1. Get or launch backend connection
// 2. Send tools/call request
// 3. Check for backend error
// 4. Unmarshal and return result
//
// Callers are responsible for adapting the result to their specific return types
// and wrapping errors as needed.
func executeBackendToolCall(ctx context.Context, l *launcher.Launcher, serverID, sessionID, toolName string, args interface{}) (interface{}, error) {
	conn, err := launcher.GetOrLaunchForSession(l, serverID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	response, err := conn.SendRequestWithServerID(ctx, "tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	}, serverID)
	if err != nil {
		return nil, err
	}

	// Check if the backend returned an error
	if response.Error != nil {
		return nil, fmt.Errorf("backend error: code=%d, message=%s", response.Error.Code, response.Error.Message)
	}

	// Parse the result
	var result interface{}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return result, nil
}

// guardBackendCaller implements guard.BackendCaller for guards to query backend metadata
type guardBackendCaller struct {
	server   *UnifiedServer
	serverID string
	ctx      context.Context
}

func (g *guardBackendCaller) CallTool(ctx context.Context, toolName string, args interface{}) (interface{}, error) {
	// Make a read-only call to the backend for metadata
	// This bypasses DIFC checks since it's internal to the guard
	log.Printf("[DIFC] Guard calling backend %s tool %s for metadata", g.serverID, toolName)

	// Get or launch backend connection (use session-aware connection for stateful backends)
	sessionID := g.ctx.Value(SessionIDContextKey)
	if sessionID == nil {
		sessionID = "default"
	}

	return executeBackendToolCall(g.ctx, g.server.launcher, g.serverID, sessionID.(string), toolName, args)
}

// newErrorCallToolResult creates a standard error CallToolResult with the error message
// included as text content, so MCP clients can understand what went wrong.
func newErrorCallToolResult(err error) (*sdk.CallToolResult, interface{}, error) {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{
			&sdk.TextContent{Text: err.Error()},
		},
	}, nil, err
}

// callBackendTool calls a tool on a backend server with DIFC enforcement
func (us *UnifiedServer) callBackendTool(ctx context.Context, serverID, toolName string, args interface{}) (*sdk.CallToolResult, interface{}, error) {
	// Note: Session validation happens at the tool registration level via closures
	// The closure captures the request and validates before calling this method
	log.Printf("Calling tool on %s: %s with DIFC enforcement", serverID, toolName)
	logUnified.Printf("callBackendTool: serverID=%s, toolName=%s, args=%+v", serverID, toolName, args)

	// Get guard for this backend
	g := us.guardRegistry.Get(serverID)
	sessionID := us.getSessionID(ctx)

	// Create backend caller for the guard
	backendCaller := &guardBackendCaller{
		server:   us,
		serverID: serverID,
		ctx:      ctx,
	}

	// Initialize policy-driven guard session state (label_agent) before first guarded call.
	enforcementMode, err := us.ensureGuardInitialized(ctx, sessionID, serverID, g, backendCaller)
	if err != nil {
		return newErrorCallToolResult(fmt.Errorf("guard session initialization failed: %w", err))
	}

	requestEvaluator := difc.NewEvaluatorWithMode(enforcementMode)

	// **Phase 0: Extract agent ID and get/create agent labels**
	agentID := guard.GetAgentIDFromContext(ctx)
	agentLabels := us.agentRegistry.GetOrCreate(agentID)
	log.Printf("[DIFC] Agent %s | Secrecy: %v | Integrity: %v",
		agentID, agentLabels.GetSecrecyTags(), agentLabels.GetIntegrityTags())

	ctx = context.WithValue(ctx, mcp.AgentTagsSnapshotContextKey, &mcp.AgentTagsSnapshot{
		Secrecy:   difc.TagsToStrings(agentLabels.GetSecrecyTags()),
		Integrity: difc.TagsToStrings(agentLabels.GetIntegrityTags()),
	})

	// Store request state for guards that need request context during response labeling.
	// This allows LabelResponse() to access the original tool arguments.
	ctx = guard.SetRequestStateInContext(ctx, map[string]interface{}{
		"tool_args": args,
	})

	// **Phase 1: Guard labels the resource**
	resource, operation, err := g.LabelResource(ctx, toolName, args, backendCaller, us.capabilities)
	if err != nil {
		log.Printf("[DIFC] Guard labeling failed: %v", err)
		return newErrorCallToolResult(fmt.Errorf("guard labeling failed: %w", err))
	}

	log.Printf("[DIFC] Resource: %s | Operation: %s | Secrecy: %v | Integrity: %v",
		resource.Description, operation, resource.Secrecy.Label.GetTags(), resource.Integrity.Label.GetTags())

	// **Phase 2: Reference Monitor performs coarse-grained access check**
	// For read operations in any mode, we skip the coarse-grained block
	// and let the request proceed. Fine-grained filtering at Phase 5 will filter
	// individual items from the response based on their actual labels from LabelResponse().
	isReadOperation := (operation == difc.OperationRead)
	result := requestEvaluator.Evaluate(agentLabels.Secrecy, agentLabels.Integrity, resource, operation)

	if !result.IsAllowed() {
		if isReadOperation {
			// Read operation in any mode - skip coarse-grained block
			// The guard will label response items and Phase 5 will enforce per-item policy
			log.Printf("[DIFC] Coarse-grained check failed for read in %s mode - proceeding to backend for response labeling", enforcementMode)
			log.Printf("[DIFC] Response items will be evaluated at Phase 5 based on per-item labels from LabelResponse()")
		} else {
			// Non-read operation - block the request
			log.Printf("[DIFC] Access DENIED for agent %s to %s: %s", agentID, resource.Description, result.Reason)
			detailedErr := difc.FormatViolationError(result, agentLabels.Secrecy, agentLabels.Integrity, resource)
			return &sdk.CallToolResult{
				Content: []sdk.Content{
					&sdk.TextContent{
						Text: detailedErr.Error(),
					},
				},
				IsError: true,
			}, nil, detailedErr
		}
	} else {
		log.Printf("[DIFC] Access ALLOWED for agent %s to %s", agentID, resource.Description)
	}

	// **Phase 3: Execute the backend call**
	backendResult, err := executeBackendToolCall(ctx, us.launcher, serverID, sessionID, toolName, args)
	if err != nil {
		return newErrorCallToolResult(err)
	}

	// **Phase 4: Guard labels the response data (for fine-grained filtering)**
	// Per spec: LabelResponse() is only called for read operations in all modes,
	// and for read-write operations in filter/propagate modes.
	// For write operations and read-write in strict mode, skip LabelResponse().
	isPureWrite := (operation == difc.OperationWrite)
	shouldCallLabelResponse := !isPureWrite && (operation != difc.OperationReadWrite || enforcementMode != difc.EnforcementStrict)

	var labeledData difc.LabeledData
	if shouldCallLabelResponse {
		labeledData, err = g.LabelResponse(ctx, toolName, backendResult, backendCaller, us.capabilities)
		if err != nil {
			log.Printf("[DIFC] Response labeling failed: %v", err)
			return newErrorCallToolResult(fmt.Errorf("response labeling failed: %w", err))
		}
	} else {
		log.Printf("[DIFC] Skipping LabelResponse() for %s operation in %s mode", operation, enforcementMode)
	}

	// **Phase 5: Reference Monitor performs fine-grained filtering (if applicable)**
	var finalResult interface{}
	var difcFiltered *difc.FilteredCollectionLabeledData // tracks items removed in filter/propagate mode
	if labeledData != nil {
		// Guard provided fine-grained labels - check if it's a collection
		if collection, ok := labeledData.(*difc.CollectionLabeledData); ok {
			// Filter collection based on agent labels
			filtered := requestEvaluator.FilterCollection(agentLabels.Secrecy, agentLabels.Integrity, collection, operation)

			log.Printf("[DIFC] Filtered collection: %d/%d items accessible",
				filtered.GetAccessibleCount(), filtered.TotalCount)

			// **Strict mode: block entire response if ANY item is filtered**
			if enforcementMode == difc.EnforcementStrict && filtered.GetFilteredCount() > 0 {
				log.Printf("[DIFC] STRICT MODE: Blocking entire response - %d/%d items violate DIFC policy",
					filtered.GetFilteredCount(), filtered.TotalCount)
				blockErr := fmt.Errorf("DIFC policy violation: %d of %d items in response are not accessible to agent %s",
					filtered.GetFilteredCount(), filtered.TotalCount, agentID)
				return &sdk.CallToolResult{
					Content: []sdk.Content{
						&sdk.TextContent{
							Text: blockErr.Error(),
						},
					},
					IsError: true,
				}, nil, blockErr
			}

			if filtered.GetFilteredCount() > 0 {
				log.Printf("[DIFC] Filtered out %d items due to DIFC policy", filtered.GetFilteredCount())
				logFilteredItems(serverID, toolName, filtered)
				difcFiltered = filtered
			}

			// Convert filtered data to result
			finalResult, err = filtered.ToResult()
			if err != nil {
				return newErrorCallToolResult(fmt.Errorf("failed to convert filtered data: %w", err))
			}
		} else {
			// Simple labeled data - already passed coarse-grained check
			finalResult, err = labeledData.ToResult()
			if err != nil {
				return newErrorCallToolResult(fmt.Errorf("failed to convert labeled data: %w", err))
			}
		}

		// **Phase 6: Accumulate labels from this operation (for reads in PROPAGATE mode only)**
		// Label accumulation should only happen when mode is EnforcementPropagate
		// Filter mode does NOT accumulate - it just filters what the agent can see
		if !isPureWrite && enforcementMode == difc.EnforcementPropagate {
			overall := labeledData.Overall()
			agentLabels.AccumulateFromRead(overall)
			log.Printf("[DIFC] Agent %s accumulated labels (propagate mode) | Secrecy: %v | Integrity: %v",
				agentID, agentLabels.GetSecrecyTags(), agentLabels.GetIntegrityTags())
		}
	} else {
		// No fine-grained labeling - use original backend result
		finalResult = backendResult

		// **Phase 6: Accumulate labels from resource (for reads in PROPAGATE mode only)**
		if !isPureWrite && enforcementMode == difc.EnforcementPropagate {
			agentLabels.AccumulateFromRead(resource)
			log.Printf("[DIFC] Agent %s accumulated labels (propagate mode) | Secrecy: %v | Integrity: %v",
				agentID, agentLabels.GetSecrecyTags(), agentLabels.GetIntegrityTags())
		}
	}

	// Convert finalResult to SDK CallToolResult format
	callResult, err := mcp.ConvertToCallToolResult(finalResult)
	if err != nil {
		return newErrorCallToolResult(fmt.Errorf("failed to convert result: %w", err))
	}

	// If items were filtered by DIFC policy in filter/propagate mode, append a notice so
	// the agent knows items exist but were withheld.  Without this, an agent receiving an
	// empty (or partial) list has no way to distinguish "no items" from "items filtered",
	// which can cause targeted-dispatch workflows to silently fall back to scheduled mode.
	if difcFiltered != nil {
		if notice := buildDIFCFilteredNotice(difcFiltered); notice != "" {
			callResult.Content = append(callResult.Content, &sdk.TextContent{Text: notice})
		}
	}

	return callResult, finalResult, nil
}

// Run starts the unified MCP server on the specified transport
func (us *UnifiedServer) Run(transport sdk.Transport) error {
	log.Println("Starting unified MCP server...")
	return us.server.Run(us.ctx, transport)
}

// GetPayloadSizeThreshold returns the configured payload size threshold (in bytes).
// Payloads larger than this threshold are stored to disk, smaller ones are returned inline.
// This getter allows other modules to access the threshold configuration.
func (us *UnifiedServer) GetPayloadSizeThreshold() int {
	return us.payloadSizeThreshold
}

// GetServerIDs returns the list of backend server IDs
func (us *UnifiedServer) GetServerIDs() []string {
	return us.launcher.ServerIDs()
}

// GetServerStatus returns the status of all configured backend servers
func (us *UnifiedServer) GetServerStatus() map[string]ServerStatus {
	status := make(map[string]ServerStatus)

	// Get all configured servers
	serverIDs := us.launcher.ServerIDs()

	for _, serverID := range serverIDs {
		// Check if server has been launched by checking launcher connections
		// For now, we'll return "running" for all configured servers
		// and track uptime from when the gateway started
		// This is a simple implementation - a more sophisticated version
		// would track actual connection state per server
		status[serverID] = ServerStatus{
			Status: "running",
			Uptime: 0, // Will be properly tracked when servers are actually launched
		}
	}

	return status
}

// GetToolsForBackend returns tools for a specific backend with prefix stripped
func (us *UnifiedServer) GetToolsForBackend(backendID string) []ToolInfo {
	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	prefix := backendID + "___"
	filtered := make([]ToolInfo, 0)

	for _, tool := range us.tools {
		if tool.BackendID == backendID {
			// Create a copy with the prefix stripped from the name
			filteredTool := *tool
			filteredTool.Name = tool.Name[len(prefix):] // Strip prefix
			filtered = append(filtered, filteredTool)
		}
	}

	return filtered
}

// GetToolHandler returns the handler for a specific backend tool
// This allows routed mode to reuse the unified server's tool handlers
func (us *UnifiedServer) GetToolHandler(backendID string, toolName string) func(context.Context, *sdk.CallToolRequest, interface{}) (*sdk.CallToolResult, interface{}, error) {
	us.toolsMu.RLock()
	defer us.toolsMu.RUnlock()

	prefixedName := backendID + "___" + toolName
	if toolInfo, ok := us.tools[prefixedName]; ok {
		return toolInfo.Handler
	}
	return nil
}

// Close cleans up resources
func (us *UnifiedServer) Close() error {
	us.launcher.Close()
	return nil
}

// IsShutdown returns true if the gateway has been shut down
func (us *UnifiedServer) IsShutdown() bool {
	us.shutdownMu.RLock()
	defer us.shutdownMu.RUnlock()
	return us.isShutdown
}

// InitiateShutdown initiates graceful shutdown and returns the number of servers terminated
// This method is idempotent - subsequent calls will return 0 servers terminated
func (us *UnifiedServer) InitiateShutdown() int {
	serversTerminated := 0
	us.shutdownOnce.Do(func() {
		// Mark as shutdown
		us.shutdownMu.Lock()
		us.isShutdown = true
		us.shutdownMu.Unlock()

		log.Println("Initiating gateway shutdown...")
		logger.LogInfo("shutdown", "Gateway shutdown initiated")

		// Count servers before closing
		serversTerminated = len(us.launcher.ServerIDs())

		// Terminate all backend servers
		log.Printf("Terminating %d backend server(s)...", serversTerminated)
		logger.LogInfo("shutdown", "Terminating %d backend servers", serversTerminated)
		us.launcher.Close()

		log.Println("Backend servers terminated")
		logger.LogInfo("shutdown", "Backend servers terminated successfully")
	})
	return serversTerminated
}

// RegisterTestTool registers a tool for testing purposes
// This method is used by integration tests to inject mock tools into the gateway
func (us *UnifiedServer) RegisterTestTool(name string, tool *ToolInfo) {
	us.toolsMu.Lock()
	defer us.toolsMu.Unlock()
	us.tools[name] = tool
}

// SetTestMode enables test mode which prevents os.Exit() calls
// This should only be used in unit tests
func (us *UnifiedServer) SetTestMode(enabled bool) {
	us.testMode = enabled
}

// ShouldExit returns whether the gateway should exit after shutdown
// Returns false in test mode to prevent actual process exit
func (us *UnifiedServer) ShouldExit() bool {
	return !us.testMode
}

// SetHTTPShutdown sets the function to call when draining in-flight HTTP requests
// during /close endpoint handling (spec 5.1.3). Should be called after the HTTP server
// is created so that the close handler can perform graceful shutdown.
func (us *UnifiedServer) SetHTTPShutdown(fn func(context.Context) error) {
	us.httpShutdownFn = fn
}

// GetHTTPShutdown returns the HTTP shutdown function, or nil if not set
func (us *UnifiedServer) GetHTTPShutdown() func(context.Context) error {
	return us.httpShutdownFn
}

// IsDIFCEnabled returns whether DIFC is enabled
func (us *UnifiedServer) IsDIFCEnabled() bool {
	return us.enableDIFC
}
