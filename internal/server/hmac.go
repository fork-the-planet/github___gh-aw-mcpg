package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

const (
	// HMACTimestampHeader carries the Unix-second timestamp included in the signed payload.
	HMACTimestampHeader = "X-MCP-Timestamp"
	// HMACNonceHeader carries a unique, per-request random nonce included in the signed payload.
	HMACNonceHeader = "X-MCP-Nonce"
	// HMACSignatureHeader carries the hex-encoded HMAC-SHA256 signature.
	HMACSignatureHeader = "X-MCP-Signature"

	// hmacMaxAgeSecs is the maximum age (in seconds) of an acceptable signed request.
	// Requests with a timestamp older than this are rejected as potentially replayed.
	hmacMaxAgeSecs = 30

	// nonceTTL is how long a seen nonce is remembered.  Must be > hmacMaxAgeSecs so
	// that any nonce from a valid request window is still tracked when it expires.
	nonceTTL = 2 * hmacMaxAgeSecs * time.Second
)

var logHMAC = logger.New("server:hmac")

// nonceCache tracks recently-seen nonces to detect replay attacks.
// Nonces are held for nonceTTL seconds after first use, then evicted.
type nonceCache struct {
	mu      sync.Mutex
	entries map[string]time.Time // nonce → eviction deadline
}

func newNonceCache() *nonceCache {
	return &nonceCache{entries: make(map[string]time.Time)}
}

// evictExpired removes entries whose deadline has passed.
// Caller must hold c.mu.
func (c *nonceCache) evictExpired(now time.Time) {
	for n, deadline := range c.entries {
		if now.After(deadline) {
			delete(c.entries, n)
		}
	}
}

// checkAndSet returns true (and records the nonce) when the nonce has not been
// seen before.  Returns false if the nonce is a duplicate.
func (c *nonceCache) checkAndSet(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Evict expired nonces on every call.  The nonce set is bounded by
	// requests within the hmacMaxAgeSecs window, so this stays small.
	c.evictExpired(now)

	if _, seen := c.entries[nonce]; seen {
		return false
	}
	c.entries[nonce] = now.Add(nonceTTL)
	return true
}

// seenNonce returns true if the nonce is already in the cache without modifying state.
// Used as a fast-reject path before the expensive body-read + signature check,
// so that obvious replays are rejected without consuming I/O resources.
func (c *nonceCache) seenNonce(nonce string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	// Evict expired entries so the map doesn't grow unboundedly.
	c.evictExpired(now)
	_, seen := c.entries[nonce]
	return seen
}

// computeHMAC builds the canonical signed message and returns the expected
// hex-encoded HMAC-SHA256 signature.
//
// Signed payload: "<timestamp>\n<nonce>\n<path>\n<hex(sha256(body))>"
func computeHMAC(secret, timestamp, nonce, path string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	msg := timestamp + "\n" + nonce + "\n" + path + "\n" + hex.EncodeToString(bodyHash[:])

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// hmacMiddleware validates that each incoming request carries a correctly signed
// HMAC-SHA256 signature.  It enforces:
//
//  1. Required headers: X-MCP-Timestamp, X-MCP-Nonce, X-MCP-Signature
//  2. Freshness: timestamp must be within hmacMaxAgeSecs seconds of server time
//  3. Replay protection: the (nonce) must not have been seen before
//  4. Signature integrity: HMAC-SHA256(secret, canonical_message) must match
//
// The canonical message is:
//
//	"<timestamp>\n<nonce>\n<path>\n<hex(sha256(body))>"
func hmacMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	cache := newNonceCache()
	logHMAC.Printf("HMAC middleware initialised (maxAge=%ds)", hmacMaxAgeSecs)

	return func(w http.ResponseWriter, r *http.Request) {
		timestamp := r.Header.Get(HMACTimestampHeader)
		nonce := r.Header.Get(HMACNonceHeader)
		sig := r.Header.Get(HMACSignatureHeader)

		if timestamp == "" || nonce == "" || sig == "" {
			logHMAC.Printf("HMAC rejected: missing headers, remote=%s path=%s", r.RemoteAddr, r.URL.Path)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "missing HMAC signature headers", "auth", "hmac_validation_failed", "missing_hmac_headers")
			return
		}

		// Validate timestamp freshness
		tsUnix, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			logHMAC.Printf("HMAC rejected: invalid timestamp %q, remote=%s", timestamp, r.RemoteAddr)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "invalid HMAC timestamp", "auth", "hmac_validation_failed", "invalid_timestamp")
			return
		}
		age := time.Since(time.Unix(tsUnix, 0))
		if age > hmacMaxAgeSecs*time.Second || age < -(hmacMaxAgeSecs*time.Second) {
			logHMAC.Printf("HMAC rejected: timestamp too old/future age=%v, remote=%s", age, r.RemoteAddr)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "HMAC timestamp out of acceptable window", "auth", "hmac_validation_failed", "stale_timestamp")
			return
		}

		// Fast-reject obviously replayed nonces before the body-read cost.
		// This is a read-only check; the authoritative write happens after
		// signature verification to avoid poisoning the cache with invalid requests.
		if cache.seenNonce(nonce) {
			logHMAC.Printf("HMAC rejected: replay detected (pre-check) nonce=%s, remote=%s", nonce, r.RemoteAddr)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "HMAC nonce already used (replay detected)", "auth", "hmac_validation_failed", "replay_detected")
			return
		}

		// Read and restore body for downstream handlers
		var body []byte
		if r.Body != nil && r.Body != http.NoBody {
			body, err = io.ReadAll(r.Body)
			if err != nil {
				rejectRequest(w, r, http.StatusBadRequest, "bad_request", "failed to read request body", "auth", "hmac_validation_failed", "body_read_error")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		expected := computeHMAC(secret, timestamp, nonce, r.URL.Path, body)
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			logHMAC.Printf("HMAC rejected: signature mismatch, remote=%s path=%s", r.RemoteAddr, r.URL.Path)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "invalid HMAC signature", "auth", "hmac_validation_failed", "signature_mismatch")
			return
		}

		// Signature is valid — now atomically record the nonce to block replays.
		// Only valid requests consume nonce slots, preventing DoS via cache poisoning.
		// If a concurrent request with the same nonce also passed the pre-check above,
		// exactly one of them will win here; the other is correctly rejected.
		if !cache.checkAndSet(nonce) {
			logHMAC.Printf("HMAC rejected: replay detected (post-check) nonce=%s, remote=%s", nonce, r.RemoteAddr)
			rejectRequest(w, r, http.StatusUnauthorized, "unauthorized", "HMAC nonce already used (replay detected)", "auth", "hmac_validation_failed", "replay_detected")
			return
		}

		logHMAC.Printf("HMAC verified: remote=%s path=%s", r.RemoteAddr, r.URL.Path)
		next(w, r)
	}
}

// applyHMACIfConfigured wraps handler with HMAC validation when secret is non-empty.
// If secret is empty the handler is returned unchanged (backward-compatible plain HTTP).
func applyHMACIfConfigured(secret string, handler http.HandlerFunc) http.HandlerFunc {
	return applyIfConfigured(secret, handler, hmacMiddleware)
}
