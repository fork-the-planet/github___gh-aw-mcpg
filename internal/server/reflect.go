package server

import (
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/httputil"
)

// HandleReflect returns an http.HandlerFunc that handles the /reflect endpoint.
func HandleReflect(unifiedServer *UnifiedServer) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteReflectResponse(w, unifiedServer.DIFCComponents)
	}
}
