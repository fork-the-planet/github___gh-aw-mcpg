package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGuard is a configurable test double for guard.Guard.
// It allows tests to exercise specific code paths in handleWithDIFC
// by controlling what LabelResource and LabelResponse return.
type stubGuard struct {
	labelResourceResult *difc.LabeledResource
	labelResourceOp     difc.OperationType
	labelResourceErr    error
	labelResponseData   difc.LabeledData
	labelResponseErr    error
}

func (g *stubGuard) Name() string { return "stub" }

func (g *stubGuard) LabelAgent(_ context.Context, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*guard.LabelAgentResult, error) {
	return &guard.LabelAgentResult{
		Agent:    guard.AgentLabelsPayload{Secrecy: []string{}, Integrity: []string{}},
		DIFCMode: difc.ModeFilter,
	}, nil
}

func (g *stubGuard) LabelResource(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (*difc.LabeledResource, difc.OperationType, error) {
	return g.labelResourceResult, g.labelResourceOp, g.labelResourceErr
}

func (g *stubGuard) LabelResponse(_ context.Context, _ string, _ interface{}, _ guard.BackendCaller, _ *difc.Capabilities) (difc.LabeledData, error) {
	return g.labelResponseData, g.labelResponseErr
}

// privateResource creates a LabeledResource with a private secrecy tag.
// The default agent (with no secrecy tags) will fail the coarse read check for this resource
// when the evaluator is in strict or filter mode.
func privateResource() *difc.LabeledResource {
	r := difc.NewLabeledResource("private-resource")
	r.Secrecy = *difc.NewSecrecyLabelWithTags([]difc.Tag{"private:test-org/test-repo"})
	return r
}

// publicResource creates a LabeledResource with no label restrictions.
func publicResource() *difc.LabeledResource {
	return difc.NewLabeledResource("public-resource")
}

// newTestServerWithStub builds a proxy.Server that uses the given stubGuard and enforcement mode.
func newTestServerWithStub(t *testing.T, upstreamURL string, g *stubGuard, mode difc.EnforcementMode) *Server {
	t.Helper()
	return &Server{
		guard:            g,
		evaluator:        difc.NewEvaluatorWithMode(mode),
		agentRegistry:    difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:     difc.NewCapabilities(),
		githubAPIURL:     upstreamURL,
		httpClient:       &http.Client{},
		guardInitialized: true,
		enforcementMode:  mode,
	}
}

// newTestServerWithPrivateAgent builds a proxy.Server whose "proxy" agent carries a private
// secrecy tag. This causes writes to resources without a matching secrecy tag to be blocked.
func newTestServerWithPrivateAgent(t *testing.T, upstreamURL string, g *stubGuard, mode difc.EnforcementMode) *Server {
	t.Helper()
	reg := difc.NewAgentRegistryWithDefaults([]difc.Tag{"private:test-org/test-repo"}, nil)
	return &Server{
		guard:            g,
		evaluator:        difc.NewEvaluatorWithMode(mode),
		agentRegistry:    reg,
		capabilities:     difc.NewCapabilities(),
		githubAPIURL:     upstreamURL,
		httpClient:       &http.Client{},
		guardInitialized: true,
		enforcementMode:  mode,
	}
}

// ─── Phase 1: LabelResource error → 502 ──────────────────────────────────────

func TestHandleWithDIFC_LabelResourceError(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{map[string]interface{}{"id": 1}})
	defer upstream.Close()

	g := &stubGuard{
		labelResourceErr: errors.New("guard unavailable"),
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "resource labeling failed")
}

// ─── Phase 2: write operation blocked by coarse check → 403 ──────────────────

func TestHandleWithDIFC_WriteOperationBlocked(t *testing.T) {
	// The agent carries a private secrecy tag; the resource (public) has no secrecy.
	// For a WRITE: agent secrecy must be a subset of resource secrecy.
	// Agent has "private:test-org/test-repo", resource is empty → write is denied.
	upstream := mockUpstream(t, http.StatusOK, map[string]interface{}{"id": 1})
	defer upstream.Close()

	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationWrite,
	}
	s := newTestServerWithPrivateAgent(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodPost, "/repos/org/repo/issues",
		bytes.NewBufferString(`{"title":"new"}`))
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "create_issue",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "DIFC policy violation")
}

// ─── Phase 4: LabelResponse error, coarse check allowed → original response ──

func TestHandleWithDIFC_LabelResponseError_CoarseAllowed(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1},
		map[string]interface{}{"id": 2},
	})
	defer upstream.Close()

	g := &stubGuard{
		labelResourceResult: publicResource(), // no restrictions → coarse allowed
		labelResourceOp:     difc.OperationRead,
		labelResponseErr:    errors.New("response labeling failed"),
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	// Coarse check passed → original upstream response is returned even though labeling failed
	assert.Equal(t, http.StatusOK, w.Code)
	var got []map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Len(t, got, 2)
}

// ─── Phase 4: LabelResponse error, coarse check denied → empty response ───────

func TestHandleWithDIFC_LabelResponseError_CoarseBlocked(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1},
	})
	defer upstream.Close()

	// Resource has private secrecy; agent has none → coarse read check is denied.
	// Phase 2 does not block reads — it stores the denial and continues.
	// Phase 4: LabelResponse fails → fallback to coarse result → writeEmptyResponse.
	g := &stubGuard{
		labelResourceResult: privateResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseErr:    errors.New("response labeling failed"),
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	// Status is preserved from upstream (200), but body is empty (coarse denied)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())
}

// ─── Phase 5: no fine-grained labels, coarse check denied → empty response ────

func TestHandleWithDIFC_NoFineGrainedLabels_CoarseBlocked(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1},
	})
	defer upstream.Close()

	// Resource has private secrecy → coarse read denied.
	// LabelResponse returns nil (no fine-grained labels) → use coarse result → empty.
	g := &stubGuard{
		labelResourceResult: privateResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   nil, // no fine-grained labels
		labelResponseErr:    nil,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())
}

// ─── Phase 5: strict mode blocks when any item is filtered ────────────────────

func TestHandleWithDIFC_StrictMode_FiltersBlock(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1},
		map[string]interface{}{"id": 2},
	})
	defer upstream.Close()

	// Item 2 has a private secrecy label; agent has none → item 2 is filtered out.
	// In strict mode, any filtering blocks the entire response.
	privateItem := difc.LabeledItem{
		Data:   map[string]interface{}{"id": 2},
		Labels: privateResource(),
	}
	collection := &difc.CollectionLabeledData{
		Items: []difc.LabeledItem{
			{Data: map[string]interface{}{"id": 1}, Labels: publicResource()},
			privateItem,
		},
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   collection,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementStrict)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "DIFC policy violation")
	assert.Contains(t, w.Body.String(), "not accessible")
}

// ─── Phase 5: collection in filter mode, some items filtered ──────────────────

func TestHandleWithDIFC_Collection_FilterMode_ItemsFiltered(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1},
		map[string]interface{}{"id": 2},
	})
	defer upstream.Close()

	// Item 2 has private labels → filtered out; item 1 is accessible.
	// In filter mode, accessible items are returned (not blocked).
	collection := &difc.CollectionLabeledData{
		Items: []difc.LabeledItem{
			{Data: map[string]interface{}{"id": 1}, Labels: publicResource()},
			{Data: map[string]interface{}{"id": 2}, Labels: privateResource()},
		},
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   collection,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	// Only the public item remains
	assert.Len(t, got, 1)
	assert.Equal(t, float64(1), got[0].(map[string]interface{})["id"])
}

// ─── Phase 5: GraphQL collection, no items filtered → original body ───────────

func TestHandleWithDIFC_GraphQL_Collection_NoItemsFiltered(t *testing.T) {
	gqlResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"issues": map[string]interface{}{
					"totalCount": float64(1),
					"nodes": []interface{}{
						map[string]interface{}{"id": "I_1", "title": "First issue"},
					},
				},
			},
		},
	}
	upstream := mockUpstream(t, http.StatusOK, gqlResponse)
	defer upstream.Close()

	// All items are public → nothing filtered → original body returned as-is.
	collection := &difc.CollectionLabeledData{
		Items: []difc.LabeledItem{
			{Data: map[string]interface{}{"id": "I_1", "title": "First issue"}, Labels: publicResource()},
		},
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   collection,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	gqlBody := []byte(`{"query":"{ repository(owner:\"org\",name:\"repo\") { issues { nodes { id } } } }"}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/graphql", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, gqlBody)

	assert.Equal(t, http.StatusOK, w.Code)
	// Original GraphQL response body is returned unchanged
	var got map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	data := got["data"].(map[string]interface{})
	repo := data["repository"].(map[string]interface{})
	issues := repo["issues"].(map[string]interface{})
	assert.Equal(t, float64(1), issues["totalCount"])
}

// ─── Phase 5: GraphQL collection, some items filtered → rebuilt response ──────

func TestHandleWithDIFC_GraphQL_Collection_ItemsFiltered(t *testing.T) {
	gqlResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"issues": map[string]interface{}{
					"totalCount": float64(2),
					"nodes": []interface{}{
						map[string]interface{}{"id": "I_1"},
						map[string]interface{}{"id": "I_2"},
					},
				},
			},
		},
	}
	upstream := mockUpstream(t, http.StatusOK, gqlResponse)
	defer upstream.Close()

	// Item I_2 is private → filtered out; rebuildGraphQLResponse is called.
	collection := &difc.CollectionLabeledData{
		Items: []difc.LabeledItem{
			{Data: map[string]interface{}{"id": "I_1"}, Labels: publicResource()},
			{Data: map[string]interface{}{"id": "I_2"}, Labels: privateResource()},
		},
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   collection,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	gqlBody := []byte(`{"query":"{ repository(owner:\"org\",name:\"repo\") { issues { nodes { id } } } }"}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/graphql", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, gqlBody)

	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	data := got["data"].(map[string]interface{})
	repo := data["repository"].(map[string]interface{})
	issues := repo["issues"].(map[string]interface{})
	// totalCount updated to reflect only accessible nodes
	assert.Equal(t, float64(1), issues["totalCount"])
	nodes := issues["nodes"].([]interface{})
	assert.Len(t, nodes, 1)
	assert.Equal(t, "I_1", nodes[0].(map[string]interface{})["id"])
}

// ─── Phase 5: simple labeled data (non-GraphQL) → ToResult ───────────────────

func TestHandleWithDIFC_SimpleLabeledData_NonGraphQL(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, map[string]interface{}{
		"name": "README.md",
		"path": "README.md",
	})
	defer upstream.Close()

	simple := &difc.SimpleLabeledData{
		Data:   map[string]interface{}{"name": "README.md", "path": "README.md"},
		Labels: publicResource(),
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   simple,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/contents/README.md", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/contents/README.md", "get_file_contents",
		map[string]interface{}{"owner": "org", "repo": "repo", "path": "README.md"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "README.md", got["name"])
}

// ─── Phase 5: simple labeled data (GraphQL) → original body ──────────────────

func TestHandleWithDIFC_SimpleLabeledData_GraphQL(t *testing.T) {
	gqlResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"viewer": map[string]interface{}{"login": "octocat"},
		},
	}
	upstream := mockUpstream(t, http.StatusOK, gqlResponse)
	defer upstream.Close()

	simple := &difc.SimpleLabeledData{
		Data:   gqlResponse,
		Labels: publicResource(),
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   simple,
	}
	s := newTestServerWithStub(t, upstream.URL, g, difc.EnforcementFilter)
	h := &proxyHandler{server: s}

	gqlBody := []byte(`{"query":"{ viewer { login } }"}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/graphql", "viewer",
		map[string]interface{}{}, gqlBody)

	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	// Original body returned as-is for GraphQL + SimpleLabeledData
	data := got["data"].(map[string]interface{})
	assert.Equal(t, "octocat", data["viewer"].(map[string]interface{})["login"])
}

// ─── Phase 6: propagate mode accumulates agent labels ─────────────────────────

func TestHandleWithDIFC_PropagateMode_AccumulatesLabels(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, map[string]interface{}{
		"id":    1,
		"title": "secret issue",
	})
	defer upstream.Close()

	// Simple data with private secrecy labels; propagate mode should add those
	// labels to the agent after the read.
	simple := &difc.SimpleLabeledData{
		Data:   map[string]interface{}{"id": 1, "title": "secret issue"},
		Labels: privateResource(),
	}
	g := &stubGuard{
		labelResourceResult: publicResource(),
		labelResourceOp:     difc.OperationRead,
		labelResponseData:   simple,
	}
	reg := difc.NewAgentRegistryWithDefaults(nil, nil)
	s := &Server{
		guard:            g,
		evaluator:        difc.NewEvaluatorWithMode(difc.EnforcementPropagate),
		agentRegistry:    reg,
		capabilities:     difc.NewCapabilities(),
		githubAPIURL:     upstream.URL,
		httpClient:       &http.Client{},
		guardInitialized: true,
		enforcementMode:  difc.EnforcementPropagate,
	}
	h := &proxyHandler{server: s}

	agentBefore := reg.GetOrCreate("proxy")
	assert.Empty(t, agentBefore.GetSecrecyTags(), "agent should start with no secrecy tags")

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues/1", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues/1", "get_issue",
		map[string]interface{}{"owner": "org", "repo": "repo", "issue_number": 1}, nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// After reading private data in propagate mode, agent should carry the private tag
	agentAfter := reg.GetOrCreate("proxy")
	tags := agentAfter.GetSecrecyTags()
	assert.NotEmpty(t, tags, "agent should have accumulated private secrecy tag after reading private data")
	assert.Contains(t, tagsAsStrings(tags), "private:test-org/test-repo")
}

// tagsAsStrings converts difc.Tag slice to string slice for assertion convenience.
func tagsAsStrings(tags []difc.Tag) []string {
	result := make([]string, len(tags))
	for i, t := range tags {
		result[i] = string(t)
	}
	return result
}
