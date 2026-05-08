package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/config"
)

// --- helpers ---

// minimalUnifiedServer returns a *UnifiedServer with only the fields needed by the
// functions under test (IsShutdown / isShutdown / shutdownMu).
func minimalUnifiedServer() *UnifiedServer {
	return &UnifiedServer{
		sessions: make(map[string]*Session),
	}
}

// markShutdown flips the shutdown flag without going through InitiateShutdown
// (which tries to close launchers etc. that aren't wired up in unit tests).
func markShutdown(us *UnifiedServer) {
	us.shutdownMu.Lock()
	us.isShutdown = true
	us.shutdownMu.Unlock()
}

// okHandler is a trivial handler that records it was called and writes 200 OK.
func okHandler(called *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	}
}

// ---- rejectIfShutdown ----

func TestRejectIfShutdown_AllowsRequestWhenNotShutdown(t *testing.T) {
	us := minimalUnifiedServer()
	called := false
	handler := rejectIfShutdown(us, okHandler(&called), "test")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called, "next handler should be called when server is not shut down")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRejectIfShutdown_Rejects503WhenShutdown(t *testing.T) {
	us := minimalUnifiedServer()
	markShutdown(us)

	called := false
	handler := rejectIfShutdown(us, okHandler(&called), "test")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.False(t, called, "next handler must NOT be called during shutdown")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Gateway is shutting down", body["error"])
}

func TestRejectIfShutdown_ContentTypeIsJSON(t *testing.T) {
	us := minimalUnifiedServer()
	markShutdown(us)

	handler := rejectIfShutdown(us, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}), "test")
	req := httptest.NewRequest(http.MethodGet, "/mcp/server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestRejectIfShutdown_ConcurrentSafety(t *testing.T) {
	us := minimalUnifiedServer()
	// Flip shutdown mid-way through concurrent requests; the test just
	// verifies there are no data races (run with -race).
	var wg sync.WaitGroup
	handler := rejectIfShutdown(us, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "test")

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 10 {
				markShutdown(us)
			}
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}(i)
	}
	wg.Wait()
}

func TestRejectIfShutdown_LogNamespaceDoesNotAffectBehavior(t *testing.T) {
	for _, ns := range []string{"", "routed", "unified", "test:namespace"} {
		t.Run("ns="+ns, func(t *testing.T) {
			us := minimalUnifiedServer()
			markShutdown(us)
			called := false
			handler := rejectIfShutdown(us, okHandler(&called), ns)
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.False(t, called)
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		})
	}
}

// ---- applyIfConfigured ----

func TestApplyIfConfigured_EmptyKeyReturnsHandlerUnchanged(t *testing.T) {
	called := false
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	middlewareCalled := false
	mw := func(key string, h http.HandlerFunc) http.HandlerFunc {
		middlewareCalled = true
		return h
	}

	result := applyIfConfigured("", base, mw)
	assert.False(t, middlewareCalled, "middleware constructor must not be called for empty key")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	result(w, req)
	assert.True(t, called)
}

func TestApplyIfConfigured_NonEmptyKeyWrapsHandler(t *testing.T) {
	baseCalled := false
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		baseCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// A middleware that sets a custom header before delegating.
	mw := func(key string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-MW-Key", key)
			h(w, r)
		}
	}

	result := applyIfConfigured("my-key", base, mw)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	result(w, req)

	assert.True(t, baseCalled)
	assert.Equal(t, "my-key", w.Header().Get("X-MW-Key"), "middleware should have been applied with the key")
}

func TestApplyIfConfigured_MiddlewareReceivesCorrectKey(t *testing.T) {
	var receivedKey string
	mw := func(key string, h http.HandlerFunc) http.HandlerFunc {
		receivedKey = key
		return h
	}
	_ = applyIfConfigured("the-api-key", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), mw)
	assert.Equal(t, "the-api-key", receivedKey)
}

func TestApplyIfConfigured_MiddlewareCanRejectRequest(t *testing.T) {
	baseCalled := false
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		baseCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Middleware that always rejects.
	mw := func(_ string, _ http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "blocked", http.StatusForbidden)
		}
	}

	result := applyIfConfigured("key", base, mw)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	result(w, req)

	assert.False(t, baseCalled)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ---- applyAuthIfConfigured ----

func TestApplyAuthIfConfigured_EmptyKeyAllowsAllRequests(t *testing.T) {
	called := false
	handler := applyAuthIfConfigured("", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	// No Authorization header — still should pass through with empty key
	w := httptest.NewRecorder()
	handler(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApplyAuthIfConfigured_WithKeyEnforcesAuth(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		authHeader string
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "valid key passes",
			apiKey:     "secret",
			authHeader: "secret",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "invalid key rejected",
			apiKey:     "secret",
			authHeader: "wrong",
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
		{
			name:       "missing auth header rejected",
			apiKey:     "secret",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			handler := applyAuthIfConfigured(tt.apiKey, func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			handler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, tt.wantCalled, called)
		})
	}
}

// ---- getToolResponseFilter ----

func TestGetToolResponseFilter_NilConfig(t *testing.T) {
	result := getToolResponseFilter(nil, "any-server", "any-tool")
	assert.Equal(t, "", result)
}

func TestGetToolResponseFilter_UnknownServer(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {ToolResponseFilters: map[string]string{"search": ".items"}},
		},
	}
	result := getToolResponseFilter(cfg, "unknown", "search")
	assert.Equal(t, "", result)
}

func TestGetToolResponseFilter_UnknownTool(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {ToolResponseFilters: map[string]string{"search": ".items"}},
		},
	}
	result := getToolResponseFilter(cfg, "github", "other-tool")
	assert.Equal(t, "", result)
}

func TestGetToolResponseFilter_KnownToolReturnsFilter(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {ToolResponseFilters: map[string]string{"search_code": ".items[] | .path"}},
		},
	}
	result := getToolResponseFilter(cfg, "github", "search_code")
	assert.Equal(t, ".items[] | .path", result)
}

func TestGetToolResponseFilter_TrimsWhitespace(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {ToolResponseFilters: map[string]string{"tool": "  .foo  "}},
		},
	}
	result := getToolResponseFilter(cfg, "github", "tool")
	assert.Equal(t, ".foo", result, "leading/trailing whitespace should be trimmed")
}

func TestGetToolResponseFilter_NilServerCfg(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"broken": nil,
		},
	}
	result := getToolResponseFilter(cfg, "broken", "any")
	assert.Equal(t, "", result)
}

func TestGetToolResponseFilter_EmptyFiltersMap(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"github": {ToolResponseFilters: map[string]string{}},
		},
	}
	result := getToolResponseFilter(cfg, "github", "search")
	assert.Equal(t, "", result)
}

func TestGetToolResponseFilter_MultipleServersIsolated(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]*config.ServerConfig{
			"alpha": {ToolResponseFilters: map[string]string{"tool": ".alpha"}},
			"beta":  {ToolResponseFilters: map[string]string{"tool": ".beta"}},
		},
	}
	assert.Equal(t, ".alpha", getToolResponseFilter(cfg, "alpha", "tool"))
	assert.Equal(t, ".beta", getToolResponseFilter(cfg, "beta", "tool"))
	assert.Equal(t, "", getToolResponseFilter(cfg, "alpha", "other"))
}
