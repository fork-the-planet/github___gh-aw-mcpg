package mcp

import (
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callSDKMethod calls the appropriate SDK method based on the method name.
// This centralizes the method dispatch logic used by both HTTP SDK transports and stdio.
func (c *Connection) callSDKMethod(method string, params interface{}) (*Response, error) {
	logConn.Printf("Dispatching SDK method: %s, serverID=%s", method, c.serverID)
	switch method {
	case "tools/list":
		return c.listTools()
	case "tools/call":
		return c.callTool(params)
	case "resources/list":
		return c.listResources()
	case "resources/read":
		return c.readResource(params)
	case "prompts/list":
		return c.listPrompts()
	case "prompts/get":
		return c.getPrompt(params)
	default:
		logConn.Printf("Unsupported method: %s", method)
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (c *Connection) listTools() (*Response, error) {
	logConn.Printf("listTools: listing tools from serverID=%s", c.serverID)
	return listMCPItems(c, "tools",
		func(cursor string) (paginatedPage[*sdk.Tool], error) {
			result, err := c.getSDKSession().ListTools(c.ctx, &sdk.ListToolsParams{Cursor: cursor})
			if err != nil {
				return paginatedPage[*sdk.Tool]{}, err
			}
			return paginatedPage[*sdk.Tool]{Items: result.Tools, NextCursor: result.NextCursor}, nil
		},
		func(items []*sdk.Tool) *sdk.ListToolsResult {
			return &sdk.ListToolsResult{Tools: items}
		},
	)
}

func (c *Connection) callTool(params interface{}) (*Response, error) {
	return callParamMethod(c, params, func(p CallToolParams) (interface{}, error) {
		// Ensure arguments is never nil - default to empty map
		// This is required by the MCP protocol which expects arguments to always be present
		if p.Arguments == nil {
			p.Arguments = make(map[string]interface{})
		}
		logConn.Printf("callTool: parsed name=%s, argumentCount=%d", p.Name, len(p.Arguments))
		return c.getSDKSession().CallTool(c.ctx, &sdk.CallToolParams{
			Name:      p.Name,
			Arguments: p.Arguments,
		})
	})
}

func (c *Connection) listResources() (*Response, error) {
	logConn.Printf("listResources: listing resources from serverID=%s", c.serverID)
	return listMCPItems(c, "resources",
		func(cursor string) (paginatedPage[*sdk.Resource], error) {
			result, err := c.getSDKSession().ListResources(c.ctx, &sdk.ListResourcesParams{Cursor: cursor})
			if err != nil {
				return paginatedPage[*sdk.Resource]{}, err
			}
			return paginatedPage[*sdk.Resource]{Items: result.Resources, NextCursor: result.NextCursor}, nil
		},
		func(items []*sdk.Resource) *sdk.ListResourcesResult {
			return &sdk.ListResourcesResult{Resources: items}
		},
	)
}

func (c *Connection) readResource(params interface{}) (*Response, error) {
	type readResourceParams struct {
		URI string `json:"uri"`
	}
	return callParamMethod(c, params, func(p readResourceParams) (interface{}, error) {
		logConn.Printf("readResource: reading resource uri=%s from serverID=%s", p.URI, c.serverID)
		return c.getSDKSession().ReadResource(c.ctx, &sdk.ReadResourceParams{
			URI: p.URI,
		})
	})
}

func (c *Connection) listPrompts() (*Response, error) {
	logConn.Printf("listPrompts: listing prompts from serverID=%s", c.serverID)
	return listMCPItems(c, "prompts",
		func(cursor string) (paginatedPage[*sdk.Prompt], error) {
			result, err := c.getSDKSession().ListPrompts(c.ctx, &sdk.ListPromptsParams{Cursor: cursor})
			if err != nil {
				return paginatedPage[*sdk.Prompt]{}, err
			}
			return paginatedPage[*sdk.Prompt]{Items: result.Prompts, NextCursor: result.NextCursor}, nil
		},
		func(items []*sdk.Prompt) *sdk.ListPromptsResult {
			return &sdk.ListPromptsResult{Prompts: items}
		},
	)
}

func (c *Connection) getPrompt(params interface{}) (*Response, error) {
	type getPromptParams struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	return callParamMethod(c, params, func(p getPromptParams) (interface{}, error) {
		logConn.Printf("getPrompt: getting prompt name=%s from serverID=%s", p.Name, c.serverID)
		return c.getSDKSession().GetPrompt(c.ctx, &sdk.GetPromptParams{
			Name:      p.Name,
			Arguments: p.Arguments,
		})
	})
}
