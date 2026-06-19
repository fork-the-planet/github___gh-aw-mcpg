package server

import (
	"context"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logSys = logger.New("server:system_tools")

// SysServer implements the MCPG system tools
type SysServer struct {
	serverIDs []string
}

// NewSysServer creates a new system server
func NewSysServer(serverIDs []string) *SysServer {
	logSys.Printf("Creating new SysServer with %d servers: %v", len(serverIDs), serverIDs)
	return &SysServer{
		serverIDs: serverIDs,
	}
}

// SysInit returns the system initialization response used by sys___init.
func (s *SysServer) SysInit() (interface{}, error) {
	logSys.Printf("Initializing MCPG system with %d servers", len(s.serverIDs))
	response := mcp.BuildMCPTextResponse(fmt.Sprintf("MCPG initialized. Available servers: %v", s.serverIDs))
	logSys.Printf("MCPG system initialized: availableServers=%v", s.serverIDs)
	return response, nil
}

// ListServers returns the configured backend server listing used by sys___list_servers.
func (s *SysServer) ListServers() (interface{}, error) {
	logSys.Printf("Listing %d configured servers", len(s.serverIDs))
	serverList := ""
	for i, id := range s.serverIDs {
		serverList += fmt.Sprintf("%d. %s\n", i+1, id)
	}

	return mcp.BuildMCPTextResponse(fmt.Sprintf("Configured MCP Servers:\n%s", serverList)), nil
}

// sysListServersHandler handles sys___list_servers tool calls.
// It validates that a session exists and delegates to callAndLogSysTool.
func (us *UnifiedServer) sysListServersHandler(ctx context.Context, _ *sdk.CallToolRequest, _ interface{}) (*sdk.CallToolResult, interface{}, error) {
	sessionID := us.getSessionID(ctx)
	logger.LogInfo("client", "MCP sys_list_servers request, session=%s", truncateSessionID(sessionID))

	if err := us.requireSession(ctx); err != nil {
		logger.LogError("client", "MCP sys_list_servers failed: session not initialized, session=%s", sessionID)
		return mcp.NewErrorCallToolResult(err)
	}

	return us.callAndLogSysTool(truncateSessionID(sessionID), "sys_list_servers", "sys_list_servers")
}
