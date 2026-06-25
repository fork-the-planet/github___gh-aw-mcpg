package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callSDKMethod calls the appropriate SDK method based on the method name.
// This centralizes the method dispatch logic used by both HTTP SDK transports and stdio.
//
// ctx is the per-request context (e.g. carrying a tool-timeout deadline) and is
// forwarded directly to every SDK session call so that cancellations and deadlines
// set by the caller are respected. c.ctx (the long-lived connection context) is NOT
// used here; that context only controls the lifetime of the connection itself.
//
// The tools/list, resources/list, and prompts/list cases share the same structure
// (paginated SDK list call via listSDKItems). They cannot be merged into a single
// method because Go does not support method-level type parameters; the pagination
// loop itself is already unified in listSDKItems (see pagination.go).
func (c *Connection) callSDKMethod(ctx context.Context, method string, params interface{}) (*Response, error) {
	logConn.Printf("Dispatching SDK method: %s, serverID=%s", method, c.serverID)
	switch method {
	case "tools/list":
		return listSDKItems(c, "tools",
			func(cursor string) (*sdk.ListToolsResult, error) {
				return c.getSDKSession().ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
			},
			func(result *sdk.ListToolsResult) paginatedPage[*sdk.Tool] {
				return paginatedPage[*sdk.Tool]{Items: result.Tools, NextCursor: result.NextCursor}
			},
			func(items []*sdk.Tool) *sdk.ListToolsResult {
				return &sdk.ListToolsResult{Tools: items}
			},
		)
	case "tools/call":
		return c.callTool(ctx, params)
	case "resources/list":
		return listSDKItems(c, "resources",
			func(cursor string) (*sdk.ListResourcesResult, error) {
				return c.getSDKSession().ListResources(ctx, &sdk.ListResourcesParams{Cursor: cursor})
			},
			func(result *sdk.ListResourcesResult) paginatedPage[*sdk.Resource] {
				return paginatedPage[*sdk.Resource]{Items: result.Resources, NextCursor: result.NextCursor}
			},
			func(items []*sdk.Resource) *sdk.ListResourcesResult {
				return &sdk.ListResourcesResult{Resources: items}
			},
		)
	case "resources/read":
		return c.readResource(ctx, params)
	case "prompts/list":
		return listSDKItems(c, "prompts",
			func(cursor string) (*sdk.ListPromptsResult, error) {
				return c.getSDKSession().ListPrompts(ctx, &sdk.ListPromptsParams{Cursor: cursor})
			},
			func(result *sdk.ListPromptsResult) paginatedPage[*sdk.Prompt] {
				return paginatedPage[*sdk.Prompt]{Items: result.Prompts, NextCursor: result.NextCursor}
			},
			func(items []*sdk.Prompt) *sdk.ListPromptsResult {
				return &sdk.ListPromptsResult{Prompts: items}
			},
		)
	case "prompts/get":
		return c.getPrompt(ctx, params)
	default:
		logConn.Printf("Unsupported method: %s", method)
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (c *Connection) callTool(ctx context.Context, params interface{}) (*Response, error) {
	return callParamMethod(c, params, func(p CallToolParams) (interface{}, error) {
		// Ensure arguments is never nil - default to empty map
		// This is required by the MCP protocol which expects arguments to always be present
		if p.Arguments == nil {
			p.Arguments = make(map[string]interface{})
		}
		logConn.Printf("callTool: parsed name=%s, argumentCount=%d", p.Name, len(p.Arguments))
		return c.getSDKSession().CallTool(ctx, &sdk.CallToolParams{
			Name:      p.Name,
			Arguments: p.Arguments,
		})
	})
}

func (c *Connection) readResource(ctx context.Context, params interface{}) (*Response, error) {
	type readResourceParams struct {
		URI string `json:"uri"`
	}
	return callParamMethod(c, params, func(p readResourceParams) (interface{}, error) {
		logConn.Printf("readResource: reading resource uri=%s from serverID=%s", p.URI, c.serverID)
		return c.getSDKSession().ReadResource(ctx, &sdk.ReadResourceParams{
			URI: p.URI,
		})
	})
}

func (c *Connection) getPrompt(ctx context.Context, params interface{}) (*Response, error) {
	type getPromptParams struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	return callParamMethod(c, params, func(p getPromptParams) (interface{}, error) {
		logConn.Printf("getPrompt: getting prompt name=%s from serverID=%s", p.Name, c.serverID)
		return c.getSDKSession().GetPrompt(ctx, &sdk.GetPromptParams{
			Name:      p.Name,
			Arguments: p.Arguments,
		})
	})
}
