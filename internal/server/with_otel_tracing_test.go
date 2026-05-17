package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithOTELTracing_PassesRequestToInnerHandler verifies that WithOTELTracing
// delegates to the wrapped inner handler so that the actual request handling
// logic runs correctly.
func TestWithOTELTracing_PassesRequestToInnerHandler(t *testing.T) {
	t.Parallel()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := WithOTELTracing(inner, "test-tag")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called, "inner handler should have been called by WithOTELTracing")
}

// TestWithOTELTracing_ForwardsResponseStatus verifies that the HTTP status code
// written by the inner handler reaches the client unchanged.
func TestWithOTELTracing_ForwardsResponseStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantStatus int
	}{
		{"200 OK", http.StatusOK},
		{"202 Accepted", http.StatusAccepted},
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.wantStatus)
			})
			handler := WithOTELTracing(inner, "routed")

			req := httptest.NewRequest(http.MethodGet, "/mcp/server", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestWithOTELTracing_PreservesResponseBody verifies that the response body written
// by the inner handler is forwarded to the client intact.
func TestWithOTELTracing_PreservesResponseBody(t *testing.T) {
	t.Parallel()

	const body = `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	handler := WithOTELTracing(inner, "unified")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, body, w.Body.String(), "response body should be forwarded unchanged")
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// TestWithOTELTracing_WorksWithoutSessionID verifies that WithOTELTracing does not
// panic when the inner handler does not inject a session ID into the request context.
// The enrichment closure reads SessionIDFromContext (which returns "") and calls
// span.SetAttributes on the no-op span returned by oteltrace.SpanFromContext —
// neither call should panic.
func TestWithOTELTracing_WorksWithoutSessionID(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally do NOT inject a session ID — context remains empty.
		w.WriteHeader(http.StatusOK)
	})

	handler := WithOTELTracing(inner, "unified")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	}, "WithOTELTracing should not panic when session ID is absent from context")

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestWithOTELTracing_WorksWithSessionInContext verifies that WithOTELTracing does
// not panic when the inner handler injects a session ID via in-place pointer mutation
// of the request struct — the same technique used by setupSessionCallback so that
// r.Context() reflects the updated context after ServeHTTP returns.
func TestWithOTELTracing_WorksWithSessionInContext(t *testing.T) {
	t.Parallel()

	const testSessionID = "test-session-abc123"

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Replicate what setupSessionCallback does: mutate *r so the enrichment
		// closure can read the session ID from r.Context() after ServeHTTP.
		*r = *injectSessionContext(r, testSessionID, "test-backend")
		w.WriteHeader(http.StatusOK)
	})

	handler := WithOTELTracing(inner, "routed")

	req := httptest.NewRequest(http.MethodPost, "/mcp/test-backend", nil)
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	}, "WithOTELTracing should not panic when session ID is present in context")

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestWithOTELTracing_SessionIDReadAfterInnerHandler verifies the post-handler
// enrichment: the session ID injected by the inner handler (via pointer mutation)
// is accessible from r.Context() after ServeHTTP — confirming the enrichment
// closure can observe it to set span attributes.
func TestWithOTELTracing_SessionIDReadAfterInnerHandler(t *testing.T) {
	t.Parallel()

	const expectedSessionID = "session-xyz-789"
	var observedSessionID string

	// Wrap the inner handler so we can inspect the session ID that the enrichment
	// closure sees (by reading it from the same request context after ServeHTTP).
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*r = *injectSessionContext(r, expectedSessionID, "backend-a")
		w.WriteHeader(http.StatusOK)
	})

	// Proxy that captures the session ID from the request context after the inner
	// handler has run (mimicking what the enrichment closure in WithOTELTracing does).
	observingProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r)
		observedSessionID = SessionIDFromContext(r.Context())
	})

	handler := WithOTELTracing(observingProxy, "routed")

	req := httptest.NewRequest(http.MethodPost, "/mcp/backend-a", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, expectedSessionID, observedSessionID,
		"session ID injected by inner handler should be visible from r.Context() after ServeHTTP")
}

// TestWithOTELTracing_DifferentTagsAreIndependent verifies that two handlers
// created with different tag values are independent and both route their own
// requests correctly.
func TestWithOTELTracing_DifferentTagsAreIndependent(t *testing.T) {
	t.Parallel()

	makeTaggedHandler := func(tag string) (http.Handler, *string) {
		observed := new(string)
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*observed = tag
			w.WriteHeader(http.StatusOK)
		})
		return WithOTELTracing(inner, tag), observed
	}

	h1, s1 := makeTaggedHandler("unified")
	h2, s2 := makeTaggedHandler("routed")

	h1.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	h2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/server", nil))

	assert.Equal(t, "unified", *s1, "h1 inner handler should observe 'unified' tag")
	assert.Equal(t, "routed", *s2, "h2 inner handler should observe 'routed' tag")
}

// TestWithOTELTracing_PreservesResponseHeaders verifies that response headers set
// by the inner handler are forwarded to the client.
func TestWithOTELTracing_PreservesResponseHeaders(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})

	handler := WithOTELTracing(inner, "unified")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
}

// TestWithOTELTracing_EmptyTag verifies that an empty tag string is handled
// without panic — it results in a blank "gateway.tag" span attribute value.
func TestWithOTELTracing_EmptyTag(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := WithOTELTracing(inner, "")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	}, "empty tag should not cause a panic")

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestWithOTELTracing_InnerHandlerError verifies that when the inner handler
// returns an error status (5xx), it is still forwarded to the client and no panic
// occurs during span recording.
func TestWithOTELTracing_InnerHandlerError(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	})

	handler := WithOTELTracing(inner, "unified")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}
