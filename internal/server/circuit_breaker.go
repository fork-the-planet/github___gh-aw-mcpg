package server

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

// circuitBreakerState represents the state of a circuit breaker.
type circuitBreakerState int

const (
	// circuitClosed is normal operation — requests pass through.
	circuitClosed circuitBreakerState = iota
	// circuitOpen means the circuit is tripped — requests are rejected immediately.
	circuitOpen
	// circuitHalfOpen means one probe request is allowed to test recovery.
	circuitHalfOpen
)

func (s circuitBreakerState) String() string {
	switch s {
	case circuitClosed:
		return "CLOSED"
	case circuitOpen:
		return "OPEN"
	case circuitHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

// DefaultRateLimitThreshold is the number of consecutive rate-limit errors
// before the circuit breaker opens.
const DefaultRateLimitThreshold = 3

// DefaultRateLimitCooldown is the number of seconds the circuit stays OPEN
// before transitioning to HALF-OPEN to probe one request.
const DefaultRateLimitCooldown = 60 * time.Second

var logCircuitBreaker = logger.New("server:circuit_breaker")

// circuitBreaker implements a per-backend rate-limit circuit breaker.
//
// State transitions:
//
//	CLOSED  → OPEN      : after threshold consecutive rate-limit errors
//	OPEN    → HALF-OPEN : after cooldown period elapses
//	HALF-OPEN → CLOSED  : probe request succeeds
//	HALF-OPEN → OPEN    : probe request is rate-limited again
type circuitBreaker struct {
	mu sync.Mutex

	state             circuitBreakerState
	consecutiveErrors int
	openedAt          time.Time
	// resetAt is the time when the upstream rate limit resets, parsed from
	// the X-RateLimit-Reset header or the tool response message.
	resetAt       time.Time
	probeInFlight bool
	serverID      string

	threshold int
	cooldown  time.Duration

	// nowFunc returns the current time. Defaults to time.Now; overridden in tests
	// to avoid flaky time.Sleep-based assertions.
	nowFunc func() time.Time
}

// newCircuitBreaker creates a circuit breaker for the given server ID.
// threshold is the number of consecutive rate-limit errors before opening;
// cooldown is how long to stay OPEN before probing.
func newCircuitBreaker(serverID string, threshold int, cooldown time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = DefaultRateLimitThreshold
	}
	if cooldown <= 0 {
		cooldown = DefaultRateLimitCooldown
	}
	return &circuitBreaker{
		serverID:  serverID,
		state:     circuitClosed,
		threshold: threshold,
		cooldown:  cooldown,
		nowFunc:   time.Now,
	}
}

// ErrCircuitOpen is returned when the circuit breaker is OPEN and a request is rejected.
type ErrCircuitOpen struct {
	ServerID string
	ResetAt  time.Time
}

func (e *ErrCircuitOpen) Error() string {
	if e.ResetAt.IsZero() {
		return fmt.Sprintf("rate limit circuit breaker is OPEN for server %q — requests temporarily rejected", e.ServerID)
	}
	return fmt.Sprintf("rate limit circuit breaker is OPEN for server %q — rate limit resets at %s (retry after %s)",
		e.ServerID, e.ResetAt.UTC().Format(time.RFC3339), time.Until(e.ResetAt).Round(time.Second))
}

// Allow reports whether a request should be allowed through. It also handles
// the OPEN → HALF-OPEN transition when the cooldown has elapsed.
// Returns an *ErrCircuitOpen error when the circuit is OPEN.
func (cb *circuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitClosed:
		return nil

	case circuitOpen:
		// Check whether we should transition to HALF-OPEN.
		// We use the upstream reset time when available, otherwise the cooldown.
		now := cb.nowFunc()
		var openUntil time.Time
		if !cb.resetAt.IsZero() && cb.resetAt.After(cb.openedAt) {
			openUntil = cb.resetAt
		} else {
			openUntil = cb.openedAt.Add(cb.cooldown)
		}
		if now.After(openUntil) {
			logCircuitBreaker.Printf("server %q circuit breaker OPEN → HALF-OPEN after cooldown", cb.serverID)
			logger.LogInfo("backend", "circuit breaker for server %q transitioning OPEN → HALF-OPEN", cb.serverID)
			cb.state = circuitHalfOpen
			cb.probeInFlight = true
			return nil // allow the single probe
		}
		return &ErrCircuitOpen{ServerID: cb.serverID, ResetAt: cb.resetAt}

	case circuitHalfOpen:
		// Only one probe is allowed; further requests are blocked until the probe resolves.
		if cb.probeInFlight {
			return &ErrCircuitOpen{ServerID: cb.serverID, ResetAt: cb.resetAt}
		}
		// This shouldn't normally happen (probe resolved but state wasn't updated),
		// but allow through defensively.
		return nil
	}

	return nil
}

// RecordSuccess records a successful (non-rate-limited) response.
// In HALF-OPEN state this closes the circuit.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	prev := cb.state
	cb.consecutiveErrors = 0
	cb.probeInFlight = false
	if cb.state == circuitHalfOpen {
		cb.state = circuitClosed
		cb.resetAt = time.Time{}
		logCircuitBreaker.Printf("server %q circuit breaker HALF-OPEN → CLOSED (probe succeeded)", cb.serverID)
		logger.LogInfo("backend", "circuit breaker for server %q recovered: HALF-OPEN → CLOSED", cb.serverID)
	} else if prev != circuitClosed {
		cb.state = circuitClosed
	}
}

// RecordRateLimit records a rate-limit error for the given server.
// resetAt is the time the upstream rate limit resets (may be zero if unknown).
// When the consecutive error count reaches threshold the circuit opens.
func (cb *circuitBreaker) RecordRateLimit(resetAt time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveErrors++
	cb.probeInFlight = false
	if !resetAt.IsZero() {
		cb.resetAt = resetAt
	}

	switch cb.state {
	case circuitClosed:
		if cb.consecutiveErrors >= cb.threshold {
			cb.state = circuitOpen
			cb.openedAt = cb.nowFunc()
			logger.LogError("backend",
				"circuit breaker for server %q OPENED after %d consecutive rate-limit errors; resets at %s",
				cb.serverID, cb.consecutiveErrors, formatResetAt(cb.resetAt))
			logCircuitBreaker.Printf("server %q circuit breaker CLOSED → OPEN (errors=%d)", cb.serverID, cb.consecutiveErrors)
		} else {
			logger.LogWarn("backend",
				"rate-limit error for server %q (consecutive=%d/%d); resets at %s",
				cb.serverID, cb.consecutiveErrors, cb.threshold, formatResetAt(cb.resetAt))
		}

	case circuitHalfOpen:
		// Probe failed — re-open the circuit.
		cb.state = circuitOpen
		cb.openedAt = cb.nowFunc()
		logger.LogError("backend",
			"circuit breaker for server %q re-OPENED after probe was rate-limited; resets at %s",
			cb.serverID, formatResetAt(cb.resetAt))
		logCircuitBreaker.Printf("server %q circuit breaker HALF-OPEN → OPEN (probe rate-limited)", cb.serverID)

	case circuitOpen:
		// Already open — update reset time.
		logger.LogWarn("backend", "server %q circuit breaker still OPEN; resets at %s",
			cb.serverID, formatResetAt(cb.resetAt))
	}
}

// State returns the current circuit breaker state (for observability).
func (cb *circuitBreaker) State() circuitBreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// formatResetAt returns a human-readable representation of a reset time.
func formatResetAt(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return fmt.Sprintf("%s (in %s)", t.UTC().Format(time.RFC3339), time.Until(t).Round(time.Second))
}

// extractRateLimitErrorText extracts the text content from a raw tool result
// that has been identified as a rate-limit error. Returns the original backend
// message so agents see the actual upstream error rather than a synthetic one.
func extractRateLimitErrorText(result interface{}) string {
	m, ok := result.(map[string]interface{})
	if !ok {
		return "rate limit exceeded"
	}
	contents, _ := m["content"].([]interface{})
	for _, c := range contents {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := cm["text"].(string); ok && text != "" {
			return text
		}
	}
	return "rate limit exceeded"
}

// isRateLimitToolResult reports whether a raw tool call result indicates
// a rate-limit error from the GitHub MCP server. It inspects the `isError`
// flag and the text content for well-known rate-limit phrases.
//
// The GitHub MCP server returns rate-limit errors as:
//
//	{"content":[{"type":"text","text":"... 403 API rate limit exceeded ..."}],"isError":true}
func isRateLimitToolResult(result interface{}) (bool, time.Time) {
	m, ok := result.(map[string]interface{})
	if !ok {
		return false, time.Time{}
	}

	// Only inspect error results.
	isErr, _ := m["isError"].(bool)
	if !isErr {
		return false, time.Time{}
	}

	contents, _ := m["content"].([]interface{})
	for _, c := range contents {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		text, _ := cm["text"].(string)
		if isRateLimitText(text) {
			resetAt := parseRateLimitResetFromText(text)
			return true, resetAt
		}
	}
	return false, time.Time{}
}

// isRateLimitText returns true when the message indicates a GitHub rate-limit error.
func isRateLimitText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "rate limit exceeded") ||
		(strings.Contains(lower, "rate limit") && strings.Contains(lower, "403")) ||
		strings.Contains(lower, "api rate limit") ||
		strings.Contains(lower, "secondary rate limit") ||
		strings.Contains(lower, "too many requests")
}

// parseRateLimitResetFromText attempts to extract a reset timestamp from the
// rate-limit error text. The GitHub MCP server includes messages like
// "API rate limit exceeded [rate reset in 42s]".
// Returns zero time when the value cannot be parsed or is 0 seconds.
func parseRateLimitResetFromText(text string) time.Time {
	// Look for "[rate reset in Ns]" pattern.
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "rate reset in ")
	if idx < 0 {
		return time.Time{}
	}
	rest := text[idx+len("rate reset in "):]
	// Find the first non-digit character.
	end := strings.IndexAny(rest, "s])")
	if end < 0 {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(secs) * time.Second)
}
