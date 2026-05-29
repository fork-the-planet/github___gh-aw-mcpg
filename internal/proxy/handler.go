package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/github/gh-aw-mcpg/internal/difc"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
	"github.com/github/gh-aw-mcpg/internal/tracing"
)

var logHandler = logger.New("proxy:handler")

// writeDIFCForbidden writes a 403 JSON response for DIFC policy violations.
// Uses the shared WriteErrorResponse helper so that the response shape is consistent
// with all other error responses in the gateway ({"error": ..., "message": ...}).
func writeDIFCForbidden(w http.ResponseWriter, message string) {
	httputil.WriteErrorResponse(w, http.StatusForbidden, "difc_forbidden", message)
}

// proxyHandler implements http.Handler and runs the DIFC pipeline on proxied requests.
type proxyHandler struct {
	server *Server
	tracing.CachedTracer
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the /api/v3 prefix that GH_HOST adds
	rawPath := StripGHHostPrefix(r.URL.Path)
	// Preserve query string for upstream forwarding
	fullPath := rawPath
	if r.URL.RawQuery != "" {
		fullPath = rawPath + "?" + r.URL.RawQuery
	}

	logHandler.Printf("incoming %s %s", r.Method, rawPath)

	// Health check endpoint
	if rawPath == "/health" || rawPath == "/healthz" {
		httputil.WriteJSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// gh CLI probes /meta during initialization for feature detection.
	// Treat it like GraphQL introspection metadata and pass through unfiltered.
	if r.Method == http.MethodGet && rawPath == "/meta" {
		h.passthrough(w, r, fullPath)
		return
	}

	// Only filter read operations (GET + GraphQL POST to /graphql)
	isGraphQL := IsGraphQLPath(rawPath)
	isRead := r.Method == http.MethodGet || (r.Method == http.MethodPost && isGraphQL)
	if !isRead {
		// Pass through write operations unmodified
		h.passthrough(w, r, fullPath)
		return
	}

	// Route the request to a guard tool name
	var toolName string
	var args map[string]interface{}
	var graphQLBody []byte

	if isGraphQL {
		// Read and parse the GraphQL body
		var err error
		graphQLBody, err = io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			httputil.WriteErrorResponse(w, http.StatusBadRequest, "bad_request", "failed to read request body")
			return
		}

		match := MatchGraphQL(graphQLBody)
		if match == nil {
			// Unknown GraphQL query — fail closed: deny rather than risk leaking unfiltered data
			logHandler.Printf("unknown GraphQL query, blocking request: %s", strutil.Truncate(string(graphQLBody), 500))
			httputil.WriteJSONResponse(w, http.StatusForbidden, map[string]interface{}{
				"errors": []map[string]string{{"message": "access denied: unrecognized GraphQL operation"}},
				"data":   nil,
			})
			return
		}
		// Schema introspection (__type, __schema) is safe metadata — passthrough without DIFC
		if match.ToolName == "graphql_introspection" {
			logHandler.Printf("GraphQL introspection query, passing through")
			clientAuth := r.Header.Get("Authorization")
			resp, respBody := h.forwardAndReadBody(w, r.Context(), http.MethodPost, fullPath, bytes.NewReader(graphQLBody), "application/json", clientAuth)
			if resp == nil {
				return
			}
			h.writeResponse(w, resp, respBody)
			return
		}
		toolName = match.ToolName
		args = match.Args

		// Inject guard-required fields (author{login}, authorAssociation) into
		// the GraphQL query so the guard can label items without enrichment.
		graphQLBody = InjectGuardFields(graphQLBody, toolName)
	} else {
		match := MatchRoute(rawPath)
		if match == nil {
			// Unknown REST endpoint — fail closed: deny rather than risk leaking unfiltered data
			logHandler.Printf("unknown REST endpoint %s, blocking request", rawPath)
			httputil.WriteErrorResponse(w, http.StatusForbidden, "forbidden", "access denied: unrecognized endpoint")
			return
		}
		toolName = match.ToolName
		args = match.Args

		// Pass search query parameter so the guard can scope integrity labels
		if q := r.URL.Query().Get("q"); q != "" {
			args["query"] = q
		}
	}

	// Run the DIFC pipeline
	h.handleWithDIFC(w, r, fullPath, toolName, args, graphQLBody)
}

// handleWithDIFC runs the 6-phase DIFC pipeline on a request.
func (h *proxyHandler) handleWithDIFC(w http.ResponseWriter, r *http.Request, path, toolName string, args map[string]interface{}, graphQLBody []byte) {
	ctx := r.Context()
	s := h.server
	backend := &restBackendCaller{server: s, clientAuth: r.Header.Get("Authorization")}

	// Start a DIFC pipeline span covering all phases for this request
	ctx, difcSpan := h.GetTracer().Start(ctx, "proxy.difc_pipeline",
		oteltrace.WithAttributes(
			tracing.GenAIToolName.String(toolName),
			semconv.URLPathKey.String(r.URL.Path),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
	)
	defer difcSpan.End()

	if !s.guardInitialized {
		errMsg := "returning 503: proxy enforcement not configured (no --policy flag provided)"
		logHandler.Print(errMsg)
		logger.LogError("proxy", "%s", errMsg)
		httputil.WriteErrorResponse(w, http.StatusServiceUnavailable, "service_unavailable", "proxy enforcement not configured")
		return
	}

	// **Phase 0: Get agent labels**
	agentLabels := s.AgentRegistry.GetOrCreate("proxy")
	logHandler.Printf("[DIFC] Phase 0: agent secrecy=%v integrity=%v",
		agentLabels.GetSecrecyTags(), agentLabels.GetIntegrityTags())

	// **Phase 1: Guard labels the resource**
	resource, operation, err := s.guard.LabelResource(ctx, toolName, args, backend, s.Capabilities)
	if err != nil {
		logHandler.Printf("[DIFC] Phase 1 failed: %v", err)
		// On labeling failure, fail closed to prevent enforcement bypass
		httputil.WriteErrorResponse(w, http.StatusBadGateway, "bad_gateway", "resource labeling failed")
		return
	}

	logHandler.Printf("[DIFC] Phase 1: resource=%s op=%s secrecy=%v integrity=%v",
		resource.Description, operation,
		resource.Secrecy.Label.GetTags(), resource.Integrity.Label.GetTags())

	// **Phase 2: Coarse-grained access check**
	coarseOutcome, evalResult := difc.EvaluateCoarseAccess(s.Evaluator, agentLabels.Secrecy, agentLabels.Integrity, resource, operation)
	switch coarseOutcome {
	case difc.CoarseAllowed:
		// access permitted, continue
	case difc.CoarseBypassForRead:
		// Read in filter mode: skip coarse block, proceed to fine-grained filtering
		logHandler.Printf("[DIFC] Phase 2: coarse check failed for read, proceeding to Phase 3")
	case difc.CoarseDenied:
		// Write blocked
		logHandler.Printf("[DIFC] Phase 2: BLOCKED %s %s — %s", r.Method, path, evalResult.Reason)
		deniedErr := fmt.Errorf("DIFC policy violation: %s", evalResult.Reason)
		tracing.RecordSpanError(difcSpan, deniedErr, "access denied: "+evalResult.Reason)
		writeDIFCForbidden(w, deniedErr.Error())
		return
	}

	// **Phase 3: Forward to upstream GitHub API**
	clientAuth := r.Header.Get("Authorization")
	var resp *http.Response
	var respBody []byte

	fwdCtx, fwdSpan := h.GetTracer().Start(ctx, "proxy.backend.forward",
		oteltrace.WithAttributes(
			semconv.URLPathKey.String(path),
			semconv.ServerAddressKey.String(h.server.upstreamHost()),
			tracing.GenAIToolName.String(toolName),
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
	defer fwdSpan.End()
	if graphQLBody != nil {
		resp, respBody = h.forwardAndReadBody(w, fwdCtx, http.MethodPost, path, bytes.NewReader(graphQLBody), "application/json", clientAuth)
	} else {
		resp, respBody = h.forwardAndReadBody(w, fwdCtx, r.Method, path, nil, "", clientAuth)
	}
	if resp != nil {
		fwdSpan.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(resp.StatusCode))
	}
	if resp == nil {
		return
	}

	// For non-200 responses, pass through as-is
	if resp.StatusCode >= 300 {
		h.writeResponse(w, resp, respBody)
		return
	}

	// Parse the response as JSON for DIFC filtering
	var responseData interface{}
	if err := json.Unmarshal(respBody, &responseData); err != nil {
		// Non-JSON response — pass through
		logHandler.Printf("[DIFC] response is not JSON, passing through")
		h.writeResponse(w, resp, respBody)
		return
	}

	// **Phase 4: Guard labels the response**
	// Store tool_args in context so LabelResponse can pass them to the WASM guard
	ctx = guard.SetRequestStateInContext(ctx, map[string]interface{}{
		"tool_args": args,
	})
	labeledData, err := s.guard.LabelResponse(ctx, toolName, responseData, backend, s.Capabilities)
	if err != nil {
		logHandler.Printf("[DIFC] Phase 4 failed: %v", err)
		// On labeling failure, use coarse-grained result
		if evalResult.IsAllowed() {
			h.writeResponse(w, resp, respBody)
		} else {
			h.writeEmptyResponse(w, resp, responseData)
		}
		return
	}

	// **Phase 5: Fine-grained filtering**
	var finalData interface{}
	var useOriginalBody bool // GraphQL responses need original format preserved
	if labeledData != nil {
		if collection, ok := labeledData.(*difc.CollectionLabeledData); ok {
			filtered := s.Evaluator.FilterCollection(
				agentLabels.Secrecy, agentLabels.Integrity, collection, operation)

			logHandler.Printf("[DIFC] Phase 5: %d/%d items accessible",
				filtered.GetAccessibleCount(), filtered.TotalCount)

			// Log filtered items
			if filtered.GetFilteredCount() > 0 {
				logHandler.Printf("[DIFC] Filtered %d items", filtered.GetFilteredCount())
				logger.LogInfo("proxy", "DIFC filtered %d/%d items for %s %s (tool=%s)",
					filtered.GetFilteredCount(), filtered.TotalCount, r.Method, path, toolName)
			}

			// Strict mode: block entire response if any item filtered
			if difc.ShouldBlockFilteredResponse(s.Mode, filtered.GetFilteredCount()) {
				logHandler.Printf("[DIFC] STRICT: blocking response — %d filtered items", filtered.GetFilteredCount())
				writeDIFCForbidden(w, fmt.Sprintf("DIFC policy violation: %d of %d items not accessible",
					filtered.GetFilteredCount(), filtered.TotalCount))
				return
			}

			// For GraphQL: if nothing was filtered, return original response body
			// to preserve the exact response format (ToResult transforms the structure)
			if graphQLBody != nil && filtered.GetFilteredCount() == 0 {
				useOriginalBody = true
			} else if graphQLBody != nil {
				// GraphQL with filtered items: reconstruct the response with only accessible items
				logHandler.Printf("[DIFC] GraphQL response: %d/%d items filtered, reconstructing response",
					filtered.GetFilteredCount(), filtered.TotalCount)
				finalData = rebuildGraphQLResponse(responseData, filtered)
			} else {
				finalData, err = filtered.ToResult()
				if err != nil {
					logHandler.Printf("[DIFC] Phase 5 ToResult failed: %v", err)
					h.writeEmptyResponse(w, resp, responseData)
					return
				}
				// Re-wrap search responses to preserve the envelope
				finalData = rewrapSearchResponse(responseData, finalData)
				// Unwrap single-object responses (e.g., get_file_contents)
				finalData = unwrapSingleObject(responseData, finalData)
			}
		} else {
			// Simple labeled data — already passed coarse check
			if graphQLBody != nil {
				useOriginalBody = true
			} else {
				finalData, err = labeledData.ToResult()
				if err != nil {
					logHandler.Printf("[DIFC] Phase 5 ToResult failed: %v", err)
					h.writeEmptyResponse(w, resp, responseData)
					return
				}
			}
		}
	} else {
		// No fine-grained labels — use coarse result
		if evalResult.IsAllowed() {
			finalData = responseData
		} else {
			h.writeEmptyResponse(w, resp, responseData)
			return
		}
	}

	// **Phase 6: Label accumulation (propagate mode)**
	if labeledData != nil && difc.ShouldAccumulateReadLabels(operation, s.Mode) {
		overall := labeledData.Overall()
		agentLabels.AccumulateFromRead(overall)
		logHandler.Printf("[DIFC] Phase 6: accumulated labels")
	}

	// Write the filtered response
	if useOriginalBody {
		// GraphQL: return original upstream response to preserve exact format
		logHandler.Printf("[DIFC] returning original response body (GraphQL, no items filtered)")
		h.writeResponse(w, resp, respBody)
	} else {
		filteredJSON, err := json.Marshal(finalData)
		if err != nil {
			httputil.WriteErrorResponse(w, http.StatusInternalServerError, "internal_error", "failed to serialize filtered response")
			return
		}
		copyResponseHeaders(w, resp)
		httputil.WriteJSONResponse(w, resp.StatusCode, json.RawMessage(filteredJSON))
	}
}

// passthrough forwards a request to the upstream GitHub API without DIFC filtering.
func (h *proxyHandler) passthrough(w http.ResponseWriter, r *http.Request, path string) {
	logHandler.Printf("passthrough %s %s", r.Method, path)

	var body io.Reader
	if r.Body != nil {
		body = r.Body
		defer r.Body.Close()
	}

	resp, respBody := h.forwardAndReadBody(w, r.Context(), r.Method, path, body, r.Header.Get("Content-Type"), r.Header.Get("Authorization"))
	if resp == nil {
		return
	}

	h.writeResponse(w, resp, respBody)
}

// writeResponse writes an upstream response to the client.
// When the upstream signals rate-limiting (HTTP 429 or X-RateLimit-Remaining == 0),
// it injects a Retry-After header and logs the event at ERROR level.
func (h *proxyHandler) writeResponse(w http.ResponseWriter, resp *http.Response, body []byte) {
	copyResponseHeaders(w, resp)
	injectRetryAfterIfRateLimited(w, resp)
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// writeEmptyResponse writes an empty JSON response matching the shape of the original data.
// originalData should be the parsed upstream response; nil or unrecognized types fall back to "[]".
// For JSON arrays it writes "[]", for GraphQL objects with a "data" key it writes {"data":null},
// and for other JSON objects it writes "{}".
func (h *proxyHandler) writeEmptyResponse(w http.ResponseWriter, resp *http.Response, originalData interface{}) {
	copyResponseHeaders(w, resp)

	var empty string
	switch obj := originalData.(type) {
	case []interface{}:
		empty = "[]"
	case map[string]interface{}:
		// GraphQL responses wrap their payload in a "data" key
		if _, ok := obj["data"]; ok {
			empty = `{"data":null}`
		} else {
			empty = "{}"
		}
	default:
		empty = "[]" // safe default for nil or unknown types
	}
	httputil.WriteJSONResponse(w, resp.StatusCode, json.RawMessage(empty))
}

// forwardAndReadBody forwards a request to the upstream GitHub API and reads the
// entire response body. On success it returns the response and body bytes. It writes
// a 502 error to w and returns nil, nil on failure.
func (h *proxyHandler) forwardAndReadBody(
	w http.ResponseWriter, ctx context.Context,
	method, path string, body io.Reader, contentType, clientAuth string,
) (*http.Response, []byte) {
	resp, err := h.server.forwardToGitHub(ctx, method, path, body, contentType, clientAuth)
	if err != nil {
		httputil.WriteErrorResponse(w, http.StatusBadGateway, "bad_gateway", "upstream request failed")
		return nil, nil
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		httputil.WriteErrorResponse(w, http.StatusBadGateway, "bad_gateway", "failed to read upstream response")
		return nil, nil
	}
	return resp, respBody
}

// copyResponseHeaders copies relevant headers from upstream to the client response.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{
		"Content-Type",
		"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
		"X-RateLimit-Resource", "X-RateLimit-Used",
		"Link", // pagination
		"X-GitHub-Request-Id",
	} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}

// injectRetryAfterIfRateLimited inspects the upstream response for rate-limit signals
// (HTTP 429 or X-Ratelimit-Remaining == 0). When detected it:
//  1. Injects a Retry-After header so the client knows when to retry.
//  2. Logs the event at ERROR level so operators can monitor rate-limit incidents.
func injectRetryAfterIfRateLimited(w http.ResponseWriter, resp *http.Response) {
	is429 := resp.StatusCode == http.StatusTooManyRequests
	// Use Go's canonical header key form (textproto.CanonicalMIMEHeaderKey produces
	// "X-Ratelimit-Remaining", matching GitHub's actual response headers).
	remaining := resp.Header.Get("X-Ratelimit-Remaining")
	resetHeader := resp.Header.Get("X-Ratelimit-Reset")

	isRateLimited := is429 || remaining == "0"
	if !isRateLimited {
		return
	}

	resetAt := httputil.ParseRateLimitResetHeader(resetHeader)
	retryAfter := computeRetryAfter(resetAt)

	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))

	logger.LogError("client",
		"upstream rate limit hit: status=%d X-Ratelimit-Remaining=%s X-Ratelimit-Reset=%s retry-after=%ds",
		resp.StatusCode, remaining, resetHeader, retryAfter)
}

// computeRetryAfter returns the number of seconds to wait before retrying.
// When resetAt is in the future the delay is clamped to [1, 3600] seconds.
// When resetAt is zero or in the past a default of 60 seconds is returned.
func computeRetryAfter(resetAt time.Time) int {
	const (
		defaultDelay = 60
		maxDelay     = 3600
	)
	if resetAt.IsZero() {
		return defaultDelay
	}
	secs := int(time.Until(resetAt).Seconds()) + 1 // add 1s buffer
	if secs < 1 {
		return defaultDelay
	}
	if secs > maxDelay {
		return maxDelay
	}
	return secs
}
