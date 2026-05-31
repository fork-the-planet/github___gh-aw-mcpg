package mcptest

import (
	"context"
	"fmt"

	"github.com/github/gh-aw-mcpg/internal/logger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var logValidator = logger.New("testutil:validator")

const validatorPaginationMaxPages = 1000

// ValidatorClient is a client for validating MCP servers
type ValidatorClient struct {
	client  *sdk.Client
	session *sdk.ClientSession
	ctx     context.Context
}

// NewValidatorClient creates a new validator client connected to the given transport
func NewValidatorClient(ctx context.Context, transport sdk.Transport) (*ValidatorClient, error) {
	client := sdk.NewClient(&sdk.Implementation{
		Name:    "mcp-validator",
		Version: "1.0.0",
	}, &sdk.ClientOptions{
		Logger: logger.NewSlogLoggerWithHandler(logValidator),
	})

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to server: %w", err)
	}

	return &ValidatorClient{
		client:  client,
		session: session,
		ctx:     ctx,
	}, nil
}

// paginate collects all pages from a paginated MCP list call.
// fetch is called with a cursor (empty string for the first page) and returns the items,
// the next cursor (empty when done), and any error.
// Keep loop-protection invariants aligned with internal/mcp/pagination.go:paginateAll.
func paginate[T any](ctx context.Context, fetch func(ctx context.Context, cursor string) ([]T, string, error)) ([]T, error) {
	var all []T
	var cursor string
	seenCursors := make(map[string]struct{})
	pages := 0
	for {
		pages++
		if pages > validatorPaginationMaxPages {
			return nil, fmt.Errorf("exceeded maximum pagination limit of %d pages", validatorPaginationMaxPages)
		}

		items, nextCursor, err := fetch(ctx, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if nextCursor == "" {
			break
		}
		if _, ok := seenCursors[nextCursor]; ok {
			return nil, fmt.Errorf("detected repeated pagination cursor %q", nextCursor)
		}
		seenCursors[nextCursor] = struct{}{}
		cursor = nextCursor
	}
	return all, nil
}

// ListTools retrieves the list of tools from the connected MCP server, including all paginated results.
func (v *ValidatorClient) ListTools() ([]*sdk.Tool, error) {
	tools, err := paginate(v.ctx, func(ctx context.Context, cursor string) ([]*sdk.Tool, string, error) {
		result, err := v.session.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, "", err
		}
		return result.Tools, result.NextCursor, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return tools, nil
}

// ListResources retrieves the list of resources from the connected MCP server, including all paginated results.
func (v *ValidatorClient) ListResources() ([]*sdk.Resource, error) {
	resources, err := paginate(v.ctx, func(ctx context.Context, cursor string) ([]*sdk.Resource, string, error) {
		result, err := v.session.ListResources(ctx, &sdk.ListResourcesParams{Cursor: cursor})
		if err != nil {
			return nil, "", err
		}
		return result.Resources, result.NextCursor, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	return resources, nil
}

// CallTool calls a tool on the MCP server
func (v *ValidatorClient) CallTool(name string, arguments map[string]interface{}) (*sdk.CallToolResult, error) {
	result, err := v.session.CallTool(v.ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("call tool %s: %w", name, err)
	}
	return result, nil
}

// ReadResource reads a resource from the MCP server
func (v *ValidatorClient) ReadResource(uri string) (*sdk.ReadResourceResult, error) {
	result, err := v.session.ReadResource(v.ctx, &sdk.ReadResourceParams{
		URI: uri,
	})
	if err != nil {
		return nil, fmt.Errorf("read resource %s: %w", uri, err)
	}
	return result, nil
}

// GetServerInfo returns the server information from the initialize handshake
func (v *ValidatorClient) GetServerInfo() *sdk.Implementation {
	initResult := v.session.InitializeResult()
	if initResult != nil {
		return initResult.ServerInfo
	}
	return nil
}

// Close closes the validator client connection
func (v *ValidatorClient) Close() error {
	if v.session != nil {
		return v.session.Close()
	}
	return nil
}
