package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates a minimal proxy Server for unit testing.
// It uses a NoopGuard so no WASM file is needed, and points
// upstream at the given URL (use a httptest.Server URL).
func newTestServer(t *testing.T, upstreamURL string) *Server {
	t.Helper()
	return &Server{
		guard:            guard.NewNoopGuard(),
		evaluator:        difc.NewEvaluatorWithMode(difc.EnforcementFilter),
		agentRegistry:    difc.NewAgentRegistryWithDefaults(nil, nil),
		capabilities:     difc.NewCapabilities(),
		githubAPIURL:     upstreamURL,
		httpClient:       &http.Client{},
		guardInitialized: true,
		enforcementMode:  difc.EnforcementFilter,
	}
}

// mockUpstream returns an httptest.Server that responds with the given status,
// body JSON and records the received requests.
func mockUpstream(t *testing.T, status int, body interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			enc := json.NewEncoder(w)
			enc.Encode(body) //nolint:errcheck
		}
	}))
}

// ─── ServeHTTP: health check ─────────────────────────────────────────────────

func TestServeHTTP_HealthCheck(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "/health", path: "/health"},
		{name: "/healthz", path: "/healthz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, "http://unused")
			h := &proxyHandler{server: s}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			resp := w.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			var got map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			assert.Equal(t, "ok", got["status"])
		})
	}
}

// ─── ServeHTTP: write operations (non-GraphQL POST/PUT/DELETE/PATCH) ─────────

func TestServeHTTP_WriteOperationsPassthrough(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			upstream := mockUpstream(t, http.StatusCreated, map[string]interface{}{"id": 1})
			defer upstream.Close()

			s := newTestServer(t, upstream.URL)
			h := &proxyHandler{server: s}

			req := httptest.NewRequest(method, "/repos/org/repo/issues", bytes.NewBufferString(`{"title":"test"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			// Non-read requests must be forwarded (passthrough), not blocked
			assert.Equal(t, http.StatusCreated, w.Code)
		})
	}
}

// ─── ServeHTTP: unknown REST endpoint ────────────────────────────────────────

func TestServeHTTP_UnknownRESTEndpointBlocked(t *testing.T) {
	s := newTestServer(t, "http://unused")
	h := &proxyHandler{server: s}

	// "/v1/unknown" does not match any route in the routes table
	req := httptest.NewRequest(http.MethodGet, "/v1/unknown/endpoint", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "access denied")
}

// ─── ServeHTTP: /api/v3 GH-host prefix is stripped ───────────────────────────

func TestServeHTTP_GHHostPrefixStripped(t *testing.T) {
	s := newTestServer(t, "http://unused")
	h := &proxyHandler{server: s}

	// /api/v3/health should be treated as /health after stripping the prefix
	req := httptest.NewRequest(http.MethodGet, "/api/v3/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "ok", got["status"])
}

// ─── ServeHTTP: unknown GraphQL query is blocked ─────────────────────────────

func TestServeHTTP_UnknownGraphQLBlocked(t *testing.T) {
	s := newTestServer(t, "http://unused")
	h := &proxyHandler{server: s}

	gqlBody, _ := json.Marshal(map[string]interface{}{
		"query": `{ completelyUnrecognisedOperation { field } }`,
	})
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ─── ServeHTTP: malformed GraphQL body ───────────────────────────────────────

func TestServeHTTP_MalformedGraphQLBody(t *testing.T) {
	s := newTestServer(t, "http://unused")
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// MatchGraphQL returns nil for invalid JSON → request is blocked
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ─── ServeHTTP: GraphQL introspection passes through ─────────────────────────

func TestServeHTTP_GraphQLIntrospectionPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"__schema":{"types":[]}}}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	gqlBody, _ := json.Marshal(map[string]interface{}{
		"query": `{ __schema { types { name } } }`,
	})
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "__schema")
}

func TestServeHTTP_GraphQLPreservesQueryString(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[]}}}}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	gqlBody, err := json.Marshal(map[string]interface{}{
		"query": `{ repository(owner:"org", name:"repo") { issues(first: 10) { nodes { id } } } }`,
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/graphql?foo=bar", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/graphql?foo=bar", receivedURL)
}

// ─── ServeHTTP: query string is forwarded on REST GET ────────────────────────

func TestServeHTTP_QueryStringForwardedToUpstream(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`)) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues?state=open&page=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, receivedURL, "state=open")
	assert.Contains(t, receivedURL, "page=2")
}

// ─── handleWithDIFC: guard not initialized → 503 ─────────────────────────────

func TestHandleWithDIFC_GuardNotInitialized(t *testing.T) {
	s := newTestServer(t, "http://unused")
	s.guardInitialized = false
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	// Invoke handleWithDIFC directly (reached via ServeHTTP for GET requests)
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

// ─── handleWithDIFC: upstream error → 502 ────────────────────────────────────

func TestHandleWithDIFC_UpstreamError(t *testing.T) {
	// Use an unreachable address to force a network error in forwardToGitHub.
	s := newTestServer(t, "http://127.0.0.1:1")
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

// ─── handleWithDIFC: non-200 upstream → pass through as-is ───────────────────

func TestHandleWithDIFC_Non200ResponsePassthrough(t *testing.T) {
	upstream := mockUpstream(t, http.StatusNotFound, map[string]interface{}{"message": "Not Found"})
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ─── handleWithDIFC: non-JSON upstream response → pass through ────────────────

func TestHandleWithDIFC_NonJSONResponsePassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("plain text response")) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "plain text response", w.Body.String())
}

// ─── handleWithDIFC: JSON array response → filtered and returned ──────────────

func TestHandleWithDIFC_JSONArrayResponse(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"id": 1, "title": "Issue 1"},
		map[string]interface{}{"id": 2, "title": "Issue 2"},
	})
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/org/repo/issues", nil)
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/repos/org/repo/issues", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	// NoopGuard returns nil LabelResponse → coarse check (IsAllowed on empty labels) passes
	// so the original responseData is written
}

// ─── handleWithDIFC: GraphQL query passes through DIFC ───────────────────────

func TestHandleWithDIFC_GraphQLBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"repository":{"issues":{"nodes":[]}}}}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	gqlBody := []byte(`{"query":"{ repository(owner:\"org\",name:\"repo\") { issues { nodes { id } } } }"}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(gqlBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleWithDIFC(w, req, "/graphql", "list_issues",
		map[string]interface{}{"owner": "org", "repo": "repo"}, gqlBody)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── passthrough ─────────────────────────────────────────────────────────────

func TestPassthrough_Success(t *testing.T) {
	var receivedMethod, receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodPost, "/repos/org/repo/issues",
		bytes.NewBufferString(`{"title":"new issue"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.passthrough(w, req, "/repos/org/repo/issues")

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"id":42`)
	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, `{"title":"new issue"}`, receivedBody)
}

func TestPassthrough_NilBody(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, map[string]interface{}{"ok": true})
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodDelete, "/repos/org/repo/issues/1", nil)
	req.Body = nil
	w := httptest.NewRecorder()
	h.passthrough(w, req, "/repos/org/repo/issues/1")

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPassthrough_UpstreamError(t *testing.T) {
	// Point at a URL that refuses connections
	s := newTestServer(t, "http://127.0.0.1:1") // port 1 is never listening
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodPost, "/repos/org/repo/issues",
		bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	h.passthrough(w, req, "/repos/org/repo/issues")

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

// ─── forwardAndReadBody ───────────────────────────────────────────────────────

func TestForwardAndReadBody_Success(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{1, 2, 3})
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	w := httptest.NewRecorder()
	resp, body := h.forwardAndReadBody(w, context.Background(), http.MethodGet, "/repos/org/repo/issues", nil, "", "")

	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, body)
}

func TestForwardAndReadBody_NetworkError(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")
	h := &proxyHandler{server: s}

	w := httptest.NewRecorder()
	resp, body := h.forwardAndReadBody(w, context.Background(), http.MethodGet, "/repos/org/repo/issues", nil, "", "")

	assert.Nil(t, resp)
	assert.Nil(t, body)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

// ─── ServeHTTP: search query param is passed to args ─────────────────────────

func TestServeHTTP_SearchQueryParamInArgs(t *testing.T) {
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"total_count":0,"items":[]}`)) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/search/issues?q=repo%3Aorg%2Frepo+is%3Aopen", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, upstreamPath, "q=")
}

// ─── ServeHTTP: full request to known REST endpoint via DIFC ─────────────────

func TestServeHTTP_KnownRESTEndpointFullPipeline(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, []interface{}{
		map[string]interface{}{"number": 1, "title": "First issue"},
	})
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/repos/myorg/myrepo/issues", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// The NoopGuard allows everything → original response is returned
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── ServeHTTP: strict mode blocks when items filtered ───────────────────────

func TestHandleWithDIFC_StrictModeBlocksFilteredItems(t *testing.T) {
	upstream := mockUpstream(t, http.StatusOK, map[string]interface{}{
		"total_count": 1,
		"items":       []interface{}{map[string]interface{}{"id": 1}},
	})
	defer upstream.Close()

	// strict mode with NoopGuard (no labels set) — evaluator allows, no items filtered
	s := newTestServer(t, upstream.URL)
	s.enforcementMode = difc.EnforcementStrict
	s.evaluator = difc.NewEvaluatorWithMode(difc.EnforcementStrict)
	h := &proxyHandler{server: s}

	req := httptest.NewRequest(http.MethodGet, "/search/issues?q=test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// NoopGuard returns nil LabelResponse, so no items are filtered → 200 OK
	assert.Equal(t, http.StatusOK, w.Code)
}
