package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReflectEndpoint_BothModes_NoAuthRequired(t *testing.T) {
	tests := []struct {
		name         string
		createServer func(addr string, us *UnifiedServer, apiKey, hmacSecret string) *http.Server
	}{
		{name: "routed mode", createServer: CreateHTTPServerForRoutedMode},
		{name: "gateway mode", createServer: CreateHTTPServerForMCP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			us, err := NewUnified(context.Background(), &config.Config{Servers: map[string]*config.ServerConfig{}})
			require.NoError(t, err)
			t.Cleanup(func() { us.Close() })

			us.AgentRegistry.Register("proxy", []difc.Tag{"repo:github/private-repo"}, []difc.Tag{"approved"})
			us.AgentRegistry.Register("abc123def456", nil, []difc.Tag{"unapproved"})

			httpServer := tt.createServer(":0", us, "test-api-key", "")
			req := httptest.NewRequest(http.MethodGet, "/reflect", nil)
			w := httptest.NewRecorder()

			httpServer.Handler.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var got difc.ReflectResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
			assert.Equal(t, us.Mode.String(), got.Mode)
			assert.ElementsMatch(t, []string{"repo:github/private-repo"}, got.Agents["proxy"].Secrecy)
			assert.ElementsMatch(t, []string{"approved"}, got.Agents["proxy"].Integrity)
			assert.Empty(t, got.Agents["abc123def456"].Secrecy)
			assert.ElementsMatch(t, []string{"unapproved"}, got.Agents["abc123def456"].Integrity)
			_, err = time.Parse(time.RFC3339, got.Timestamp)
			assert.NoError(t, err)
		})
	}
}

func TestReflectEndpoint_EmptyRegistry(t *testing.T) {
	us, err := NewUnified(context.Background(), &config.Config{Servers: map[string]*config.ServerConfig{}})
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	httpServer := CreateHTTPServerForMCP(":0", us, "", "")
	req := httptest.NewRequest(http.MethodGet, "/reflect", nil)
	w := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var got difc.ReflectResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Empty(t, got.Agents)
}
