package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/envutil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/oidc"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logConn = logger.New("mcp:connection")

// defaultConnectTimeout is the fallback connect timeout used when the configured timeout
// is non-positive or otherwise invalid.
// Kept in sync with config.DefaultConnectTimeout (30 s) to avoid importing the config package.
const defaultConnectTimeout = 30 * time.Second

// normalizeConnectTimeout returns defaultConnectTimeout when the input timeout is
// non-positive, otherwise it returns the input timeout unchanged.
func normalizeConnectTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultConnectTimeout
	}
	return timeout
}

// ContextKey for session ID
type ContextKey string

// SessionIDContextKey is used to store MCP session ID in context
// This is the same key used in the server package to avoid circular dependencies
const SessionIDContextKey ContextKey = "awmg-session-id"

// AgentTagsSnapshotContextKey stores a per-request snapshot of agent DIFC tags for enriched logging.
const AgentTagsSnapshotContextKey ContextKey = "awmg-agent-tags-snapshot"

// AgentTagsSnapshot contains agent secrecy/integrity tag snapshots for log enrichment.
type AgentTagsSnapshot struct {
	Secrecy   []string
	Integrity []string
}

// GetAgentTagsSnapshotFromContext extracts the agent DIFC tag snapshot from the request context.
// Used by guards (e.g., write-sink) that need the agent's current labels to mirror onto resources.
func GetAgentTagsSnapshotFromContext(ctx context.Context) (*AgentTagsSnapshot, bool) {
	if ctx == nil {
		return nil, false
	}

	raw := ctx.Value(AgentTagsSnapshotContextKey)
	snapshot, ok := raw.(*AgentTagsSnapshot)
	if !ok || snapshot == nil {
		return nil, false
	}

	return snapshot, true
}

// Connection represents a connection to an MCP server using the official SDK
type Connection struct {
	client   *sdk.Client
	session  *sdk.ClientSession
	ctx      context.Context
	cancel   context.CancelFunc
	serverID string // Server ID from config for logging
	// HTTP-specific fields
	isHTTP            bool
	httpURL           string
	headers           map[string]string
	httpClient        *http.Client
	httpSessionID     string            // Session ID returned by the HTTP backend
	httpTransportType HTTPTransportType // Type of HTTP transport in use
	keepAliveInterval time.Duration     // Keepalive interval for SDK transports (0 = disabled)
	connectTimeout    time.Duration     // Per-transport connect timeout for SDK transports
	// sessionMu protects the mutable session fields: httpSessionID, session, and client.
	// Always use getHTTPSessionID() or getSDKSession() to read these fields; the
	// reconnect functions (reconnectPlainJSON, reconnectSDKTransport) hold the full Lock.
	sessionMu sync.RWMutex
}

// getSDKSession returns a snapshot of the current SDK session under a read lock.
// Returns nil if no session is available (e.g. plain JSON-RPC transport).
func (c *Connection) getSDKSession() *sdk.ClientSession {
	c.sessionMu.RLock()
	s := c.session
	c.sessionMu.RUnlock()
	return s
}

// getHTTPSessionID returns a snapshot of the current HTTP session ID under a read lock.
func (c *Connection) getHTTPSessionID() string {
	c.sessionMu.RLock()
	id := c.httpSessionID
	c.sessionMu.RUnlock()
	return id
}

// NewConnection creates a new MCP connection using the official SDK
func NewConnection(ctx context.Context, serverID, command string, args []string, env map[string]string) (*Connection, error) {
	logger.LogInfo("backend", "Creating new MCP backend connection, command=%s, args=%v", command, sanitize.SanitizeArgs(args))
	ctx, cancel := context.WithCancel(ctx)

	// Create MCP client with logger (no keepalive for stdio – the process lifespan manages the session)
	client := newMCPClient(logConn, 0)

	// Expand Docker -e flags that reference environment variables
	// Docker's `-e VAR_NAME` expects VAR_NAME to be in the environment
	expandedArgs := envutil.ExpandEnvArgs(args)
	logConn.Printf("Expanded args for Docker env: %v", sanitize.SanitizeArgs(expandedArgs))

	// Create command transport
	cmd := exec.CommandContext(ctx, command, expandedArgs...)

	// Start with parent's environment to inherit shell variables
	cmd.Env = append([]string{}, cmd.Environ()...)

	// Add/override with config-specified environment variables
	if len(env) > 0 {
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Capture and stream stderr to help diagnose container issues
	// The SDK's CommandTransport only uses stdin/stdout for MCP protocol,
	// so we can capture stderr separately for debugging
	// Use a TeeReader-style approach: write to both a buffer (for error reporting)
	// and to a pipe that streams to logs in real-time
	var stderrBuf bytes.Buffer
	stderrPipeReader, stderrPipeWriter := io.Pipe()
	cmd.Stderr = io.MultiWriter(&stderrBuf, stderrPipeWriter)

	// Stream stderr to logs in a goroutine
	go func() {
		defer stderrPipeReader.Close()
		scanner := bufio.NewScanner(stderrPipeReader)
		for scanner.Scan() {
			line := scanner.Text()
			sanitizedLine := sanitize.SanitizeString(line)
			logger.LogInfoToServer(serverID, "backend", "[stderr] %s", sanitizedLine)
		}
	}()

	logger.LogInfo("backend", "Starting MCP backend server, command=%s, args=%v", command, sanitize.SanitizeArgs(expandedArgs))
	transport := &sdk.CommandTransport{Command: cmd}

	// Connect to the server (this handles the initialization handshake automatically)
	logConn.Print("Initiating MCP server connection and handshake")
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cancel()
		stderrPipeWriter.Close() // Close pipe to stop the stderr streaming goroutine

		stderrOutput := strings.TrimSpace(stderrBuf.String())
		logger.LogErrorToServer(serverID, "backend",
			"MCP connection failed: server=%s, command=%s, args=%v, error=%v",
			serverID, command, sanitize.SanitizeArgs(expandedArgs), err)
		if stderrOutput != "" {
			logger.LogErrorToMarkdown("backend", "MCP backend stderr output:\n%s", sanitize.SanitizeString(stderrOutput))
		}

		logConn.Printf("Connection failed: command=%s, error=%v", command, err)
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	logger.LogInfoToMarkdown("backend", "Successfully connected to MCP backend server, command=%s", command)

	conn := &Connection{
		client:   client,
		session:  session,
		ctx:      ctx,
		cancel:   cancel,
		serverID: serverID,
		isHTTP:   false,
	}

	logger.LogInfo("backend", "Started MCP server: %s %v", command, sanitize.SanitizeArgs(expandedArgs))
	return conn, nil
}

// NewHTTPConnection creates a new HTTP-based MCP connection with transport fallback
// For HTTP servers that are already running, we connect and initialize a session
//
// This function implements a fallback strategy for HTTP transports:
//  1. Try standard transports in order:
//     a. Streamable HTTP (2025-03-26 spec) using SDK's StreamableClientTransport
//     b. SSE (2024-11-05 spec) using SDK's SSEClientTransport
//     c. Plain JSON-RPC 2.0 over HTTP POST as final fallback
//
// Custom headers (e.g. Authorization) are injected into every outgoing request via a
// custom http.RoundTripper, so the SDK transports are used even when authentication
// headers are configured.
//
// When oidcProvider is non-nil, a GitHub Actions OIDC token is dynamically acquired
// and injected as Authorization: Bearer on every request, overriding any static
// Authorization header from the headers map.
//
// This ensures compatibility with all types of HTTP MCP servers.
func NewHTTPConnection(ctx context.Context, serverID, url string, headers map[string]string, oidcProvider *oidc.Provider, oidcAudience string, keepAlive time.Duration, connectTimeout time.Duration) (*Connection, error) {
	// Apply default connect timeout when not specified
	connectTimeout = normalizeConnectTimeout(connectTimeout)
	logger.LogInfo("backend", "Creating HTTP MCP connection with transport fallback, url=%s, connectTimeout=%v", url, connectTimeout)
	ctx, cancel := context.WithCancel(ctx)

	// Create an HTTP client with connect/setup timeouts only.
	// Do not set http.Client.Timeout or ResponseHeaderTimeout: request execution
	// should be controlled by per-request context deadlines (for example the gateway
	// tool timeout budget). connectTimeout bounds transport establishment/fallback
	// (TCP dial, TLS handshake) but must not cap how long we wait for response
	// headers during a tools/call — that is governed by toolTimeout via context.
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectTimeout,
			}).DialContext,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	// Build the transport layer in the correct order so that OIDC takes precedence
	// over any static Authorization header:
	//
	//   headerInjectingRoundTripper (outer — sets static headers first)
	//     └─ oidcRoundTripper        (inner — overrides Authorization with OIDC token)
	//          └─ http.DefaultTransport
	//
	// By placing the OIDC layer inside, it runs last and its Authorization: Bearer
	// header is the one that reaches the server, overwriting any static Authorization
	// from the headers map. Other static headers (e.g. X-Custom-Header) pass through.
	baseClient := httpClient
	if oidcProvider != nil {
		baseClient = buildHTTPClientWithOIDC(httpClient, oidcProvider, oidcAudience)
		logger.LogInfo("backend", "OIDC authentication enabled for HTTP MCP connection: url=%s, audience=%s", url, oidcAudience)
	}

	// Wrap with static header injection on top. When no headers are configured the
	// original client is returned unchanged.
	headerClient := buildHTTPClientWithHeaders(baseClient, headers)

	// Try standard transports in order: streamable HTTP → SSE → plain JSON-RPC

	// Try 1: Streamable HTTP (2025-03-26 spec)
	conn, err := tryStreamableHTTPTransport(ctx, cancel, serverID, url, headers, headerClient, keepAlive, connectTimeout)
	if err == nil {
		logger.LogInfo("backend", "Successfully connected using streamable HTTP transport, url=%s", url)
		return conn, nil
	}
	logConn.Printf("Streamable HTTP failed: %v", err)

	// Try 2: SSE (2024-11-05 spec)
	conn, err = trySSETransport(ctx, cancel, serverID, url, headers, headerClient, keepAlive, connectTimeout)
	if err == nil {
		logger.LogWarn("backend", "⚠️  MCP over SSE (2024-11-05 spec) is DEPRECATED for url=%s. Please migrate to streamable HTTP transport (2025-03-26 spec).", url)
		logger.LogInfo("backend", "Configured HTTP MCP server with SSE transport: %s", url)
		return conn, nil
	}
	logConn.Printf("SSE transport failed: %v", err)

	// Try 3: Plain JSON-RPC over HTTP (non-standard, for fallback)
	conn, err = tryPlainJSONTransport(ctx, cancel, serverID, url, headers, headerClient)
	if err == nil {
		logger.LogInfo("backend", "Successfully connected using plain JSON-RPC transport, url=%s", url)
		return conn, nil
	}
	logConn.Printf("Plain JSON-RPC transport failed: %v", err)

	// All transports failed
	cancel()
	logger.LogError("backend", "All HTTP transports failed for url=%s", url)
	return nil, fmt.Errorf("failed to connect using any HTTP transport (tried streamable, SSE, and plain JSON-RPC): last error: %w", err)
}

// IsHTTP returns true if this is an HTTP connection
func (c *Connection) IsHTTP() bool {
	return c.isHTTP
}

// GetHTTPURL returns the HTTP URL for this connection
func (c *Connection) GetHTTPURL() string {
	return c.httpURL
}

// GetHTTPHeaders returns the HTTP headers for this connection
func (c *Connection) GetHTTPHeaders() map[string]string {
	return c.headers
}

// ServerInfo returns the backend's name and version from the MCP initialize handshake.
// Returns ("", "") when no SDK session is available (plain JSON-RPC transport).
func (c *Connection) ServerInfo() (name, version string) {
	sess := c.getSDKSession()
	if sess == nil {
		return "", ""
	}
	initResult := sess.InitializeResult()
	if initResult == nil || initResult.ServerInfo == nil {
		return "", ""
	}
	return initResult.ServerInfo.Name, initResult.ServerInfo.Version
}

// withReconnectLock acquires the session write lock, logs the reconnect attempt, runs
// reconnect, logs the result, and wraps any error with a consistent message.
// transportName is included in the debug log to identify which transport is reconnecting
// (e.g. "plain JSON-RPC" or "SDK transport (type=streamable)").
func (c *Connection) withReconnectLock(transportName string, reconnect func() error) error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	logConn.Printf("Session expired, reconnecting %s for serverID=%s", transportName, c.serverID)
	c.logReconnectStart()
	err := reconnect()
	c.logReconnectResult(err)
	if err != nil {
		return fmt.Errorf("session reconnect failed: %w", err)
	}
	return nil
}

// reconnectPlainJSON re-initialises the plain JSON-RPC session with the HTTP backend.
// It is safe for concurrent callers: only one reconnect runs at a time, and the updated
// session ID is available to all callers once the lock is released.
func (c *Connection) reconnectPlainJSON() error {
	return c.withReconnectLock("plain JSON-RPC", func() error {
		sessionID, err := c.initializeHTTPSession()
		if err != nil {
			return err
		}
		c.httpSessionID = sessionID
		logConn.Printf("Reconnected plain JSON-RPC session for serverID=%s, new sessionID=%s", c.serverID, sessionID)
		return nil
	})
}

// reconnectSDKTransport re-establishes the SDK session for streamable or SSE transports.
// It is safe for concurrent callers: only one reconnect runs at a time.
func (c *Connection) reconnectSDKTransport() error {
	return c.withReconnectLock(fmt.Sprintf("SDK transport (type=%s)", c.httpTransportType), func() error {
		// Close the existing session gracefully (ignore error – it's already dead).
		if c.session != nil {
			_ = c.session.Close()
		}

		// Rebuild the header-injecting client so custom auth headers are preserved on reconnect.
		headerClient := buildHTTPClientWithHeaders(c.httpClient, c.headers)

		// Build the appropriate transport.
		// Re-use the same keepAliveInterval so the reconnected session also sends periodic pings.
		client := newMCPClient(logConn, c.keepAliveInterval)
		var transport sdk.Transport
		switch c.httpTransportType {
		case HTTPTransportStreamable:
			transport = &sdk.StreamableClientTransport{
				Endpoint:   c.httpURL,
				HTTPClient: headerClient,
				// See streamableMaxRetries for rationale. The gateway handles reconnection
				// itself to prevent double-retry behaviour.
				MaxRetries:           streamableMaxRetries,
				DisableStandaloneSSE: true,
			}
		case HTTPTransportSSE:
			transport = &sdk.SSEClientTransport{
				Endpoint:   c.httpURL,
				HTTPClient: headerClient,
			}
		default:
			return fmt.Errorf("cannot reconnect: unsupported transport type %s", c.httpTransportType)
		}

		timeout := normalizeConnectTimeout(c.connectTimeout)
		connectCtx, cancel := context.WithTimeout(c.ctx, timeout)
		defer cancel()

		session, err := client.Connect(connectCtx, transport, nil)
		if err != nil {
			return err
		}

		c.client = client
		c.session = session

		logConn.Printf("Reconnected SDK session for serverID=%s", c.serverID)
		return nil
	})
}

// callSDKMethodWithReconnect calls the SDK method and, if the session has expired,
// reconnects and retries exactly once before propagating the error.
func (c *Connection) callSDKMethodWithReconnect(method string, params interface{}) (*Response, error) {
	result, err := c.callSDKMethod(method, params)
	if err != nil && isSessionNotFoundError(err) {
		logConn.Printf("Session not found error from SDK (serverID=%s), attempting reconnect", c.serverID)
		if reconnErr := c.reconnectSDKTransport(); reconnErr != nil {
			logConn.Printf("SDK session reconnect failed for serverID=%s: %v; returning original error", c.serverID, reconnErr)
			logger.LogError("backend", "SDK session reconnect failed for %s: %v", c.serverID, reconnErr)
			// Return the original session-not-found error so the caller sees a meaningful message.
			return result, err
		}
		result, err = c.callSDKMethod(method, params)
	}
	return result, err
}

// SendRequest sends a JSON-RPC request and waits for the response
// The serverID parameter is used for logging to associate the request with a backend server
func (c *Connection) SendRequest(method string, params interface{}) (*Response, error) {
	return c.SendRequestWithServerID(context.Background(), method, params, "unknown")
}

// SendRequestWithServerID sends a JSON-RPC request with server ID for logging
// The ctx parameter is used to extract session ID for HTTP MCP servers
func (c *Connection) SendRequestWithServerID(ctx context.Context, method string, params interface{}, serverID string) (*Response, error) {
	snapshot, hasSnapshot := GetAgentTagsSnapshotFromContext(ctx)
	shouldAttachAgentTags := hasSnapshot && difc.IsSinkServerID(serverID)
	var loggingSnapshot *AgentTagsSnapshot
	if shouldAttachAgentTags {
		loggingSnapshot = snapshot
	}

	// Log the outbound request to backend server
	requestPayload, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	logOutboundRPCRequest(serverID, method, requestPayload, loggingSnapshot)

	var result *Response
	var err error

	// Handle HTTP connections
	if c.isHTTP {
		// For plain JSON-RPC transport, use manual HTTP requests
		if c.httpTransportType == HTTPTransportPlainJSON {
			result, err = c.sendHTTPRequest(ctx, method, params)
		} else {
			// For streamable and SSE transports, use SDK session methods
			result, err = c.callSDKMethodWithReconnect(method, params)
		}
	} else {
		// Handle stdio connections using SDK client
		result, err = c.callSDKMethod(method, params)
	}

	return logInboundRPCResponseFromResult(serverID, result, err, loggingSnapshot)
}

// requireSDKSession validates that a session is available for SDK operations.
// This helper centralizes session validation logic across all MCP method wrappers.
// Returns an error if the session is nil (e.g., for plain JSON-RPC transport).
func (c *Connection) requireSDKSession() error {
	if c.getSDKSession() == nil {
		return fmt.Errorf("SDK session not available for plain JSON-RPC transport")
	}
	return nil
}

// Close closes the connection
func (c *Connection) Close() error {
	logConn.Printf("Closing connection: serverID=%s, isHTTP=%v", c.serverID, c.isHTTP)
	if c.cancel != nil {
		c.cancel()
	}
	if session := c.getSDKSession(); session != nil {
		return session.Close()
	}
	return nil
}
