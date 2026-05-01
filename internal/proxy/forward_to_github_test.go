package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForwardToGitHub_AuthHeader verifies that forwardToGitHub correctly selects
// the Authorization header based on clientAuth and githubToken precedence rules.
func TestForwardToGitHub_AuthHeader(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		clientAuth  string
		wantAuthHdr string
	}{
		{
			name:        "uses clientAuth when provided (takes precedence over token)",
			githubToken: "server-token",
			clientAuth:  "Bearer client-token",
			wantAuthHdr: "Bearer client-token",
		},
		{
			name:        "uses githubToken when clientAuth is empty",
			githubToken: "server-token",
			clientAuth:  "",
			wantAuthHdr: "token server-token",
		},
		{
			name:        "no auth header when both are empty",
			githubToken: "",
			clientAuth:  "",
			wantAuthHdr: "",
		},
		{
			name:        "clientAuth used even when githubToken present",
			githubToken: "my-server-token",
			clientAuth:  "token my-client-token",
			wantAuthHdr: "token my-client-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuth string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedAuth = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			s := &Server{
				githubAPIURL: upstream.URL,
				githubToken:  tt.githubToken,
				httpClient:   upstream.Client(),
			}

			resp, err := s.forwardToGitHub(context.Background(), http.MethodGet, "/repos/org/repo", nil, "", tt.clientAuth)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantAuthHdr, capturedAuth)
		})
	}
}

// TestForwardToGitHub_ContentType verifies that the Content-Type header is forwarded
// when provided, and not set when empty.
func TestForwardToGitHub_ContentType(t *testing.T) {
	tests := []struct {
		name            string
		contentType     string
		wantContentType string
	}{
		{
			name:            "sets content-type when provided",
			contentType:     "application/json",
			wantContentType: "application/json",
		},
		{
			name:            "does not set content-type when empty",
			contentType:     "",
			wantContentType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedContentType string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContentType = r.Header.Get("Content-Type")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			s := &Server{
				githubAPIURL: upstream.URL,
				httpClient:   upstream.Client(),
			}

			resp, err := s.forwardToGitHub(context.Background(), http.MethodPost, "/repos/org/repo", nil, tt.contentType, "")
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantContentType, capturedContentType)
		})
	}
}

// TestForwardToGitHub_ForwardsRequestBody verifies that the request body is forwarded
// to the upstream when provided.
func TestForwardToGitHub_ForwardsRequestBody(t *testing.T) {
	const requestBody = `{"query":"{ viewer { login } }"}`
	var capturedBody string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	s := &Server{
		githubAPIURL: upstream.URL,
		httpClient:   upstream.Client(),
	}

	resp, err := s.forwardToGitHub(context.Background(), http.MethodPost, "/graphql", strings.NewReader(requestBody), "application/json", "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	assert.Equal(t, requestBody, capturedBody)
}

// TestForwardToGitHub_ForwardsHTTPMethod verifies that the HTTP method is forwarded
// correctly to the upstream.
func TestForwardToGitHub_ForwardsHTTPMethod(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var capturedMethod string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			s := &Server{
				githubAPIURL: upstream.URL,
				httpClient:   upstream.Client(),
			}

			resp, err := s.forwardToGitHub(context.Background(), method, "/repos/org/repo", nil, "", "")
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, method, capturedMethod)
		})
	}
}
