package httputil

import (
	"net/http"

	"github.com/github/gh-aw-mcpg/internal/difc"
)

// WriteSimpleHealthResponse writes a minimal {"status":"ok"} JSON health response.
// This is the standard health response shape for endpoints that do not have access
// to detailed server status (e.g. the proxy), keeping them consistent with each other.
func WriteSimpleHealthResponse(w http.ResponseWriter) {
	WriteJSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// WriteReflectResponse writes the DIFC label snapshot JSON response for /reflect endpoints.
// Both the server and the proxy expose /reflect; sharing this helper ensures the output
// format evolves consistently as difc.BuildReflectResponse changes.
func WriteReflectResponse(w http.ResponseWriter, difcComponents difc.DIFCComponents) {
	WriteJSONResponse(w, http.StatusOK, difc.BuildReflectResponse(difcComponents))
}
