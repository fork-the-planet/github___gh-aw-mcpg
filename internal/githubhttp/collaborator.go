package githubhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/httputil"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/util"
)

var logCollab = logger.New("githubhttp:collaborator")

// ParseCollaboratorPermissionArgs extracts and validates the owner, repo, and
// username fields from an args map for a get_collaborator_permission call.
// It returns the (possibly partial) values even on error so that callers can
// include them in diagnostic log messages.
func ParseCollaboratorPermissionArgs(argsMap map[string]interface{}) (owner, repo, username string, err error) {
	owner = util.GetStringFromMap(argsMap, "owner")
	repo = util.GetStringFromMap(argsMap, "repo")
	username = util.GetStringFromMap(argsMap, "username")
	if owner == "" || repo == "" || username == "" {
		logCollab.Printf("ParseCollaboratorPermissionArgs: missing required fields: owner=%q, repo=%q, username=%q", owner, repo, username)
		err = fmt.Errorf("get_collaborator_permission: missing owner/repo/username")
	}
	return
}

// WrapCollaboratorPermission parses the raw GitHub API response body for a
// get_collaborator_permission request, logs the resolved permission level for
// observability, and returns the body wrapped in MCP text-response format.
//
// This helper is shared between the server and proxy packages to eliminate
// duplicated parse/log/wrap logic. Callers pass their own debug logger's Printf
// method so that log lines appear under the correct namespace.
func WrapCollaboratorPermission(
	body []byte,
	owner, repo, username string,
	statusCode int,
	logPrintf func(format string, args ...interface{}),
) interface{} {
	var permResp map[string]interface{}
	if jsonErr := json.Unmarshal(body, &permResp); jsonErr == nil {
		if perm, ok := permResp["permission"].(string); ok {
			logPrintf("get_collaborator_permission: %s/%s user %s → permission=%q (HTTP %d)", owner, repo, username, perm, statusCode)
		} else {
			logPrintf("get_collaborator_permission: %s/%s user %s → HTTP %d, permission field missing from response", owner, repo, username, statusCode)
		}
	} else {
		logPrintf("get_collaborator_permission: %s/%s user %s → HTTP %d, %d bytes (JSON parse failed: %v)", owner, repo, username, statusCode, len(body), jsonErr)
	}
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": string(body)},
		},
	}
}

// FetchCollaboratorPermission executes a get_collaborator_permission REST call
// using the provided fetch function and returns the wrapped MCP text response.
//
// The fetch callback should perform the authenticated HTTP request for the
// given API path and return the upstream response.
func FetchCollaboratorPermission(
	ctx context.Context,
	owner, repo, username string,
	fetch func(ctx context.Context, apiPath string) (*http.Response, error),
	logPrintf func(format string, args ...interface{}),
) (interface{}, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/collaborators/%s/permission", owner, repo, username)
	logCollab.Printf("FetchCollaboratorPermission: owner=%s, repo=%s, username=%s, apiPath=%s", owner, repo, username, apiPath)

	resp, err := fetch(ctx, apiPath)
	if err != nil {
		logCollab.Printf("FetchCollaboratorPermission: fetch error: %v", err)
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("failed to fetch response: nil response returned without error")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("failed to fetch response: response body is nil")
	}
	body, err := httputil.ReadResponseBody(resp, "GitHub API")
	if err != nil {
		logCollab.Printf("FetchCollaboratorPermission: GitHub API error: owner=%s, repo=%s, username=%s, err=%v", owner, repo, username, err)
		return nil, err
	}
	logCollab.Printf("FetchCollaboratorPermission: response received: status=%d, bodyLen=%d", resp.StatusCode, len(body))

	return WrapCollaboratorPermission(body, owner, repo, username, resp.StatusCode, logPrintf), nil
}
