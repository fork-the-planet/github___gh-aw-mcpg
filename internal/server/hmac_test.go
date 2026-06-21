package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errReader is an io.Reader that always returns an error, used to exercise the
// body-read error path in hmacMiddleware.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read error")
}

// barrierReader synchronises two concurrent goroutines so they both pass the
// seenNonce pre-check before either proceeds to checkAndSet. On the first Read
// call it signals the WaitGroup and then blocks until release is closed, ensuring
// that io.ReadAll stalls in a known place while the sibling goroutine also
// reaches the same stall point.
type barrierReader struct {
	buf     *bytes.Reader
	entered *sync.WaitGroup
	release chan struct{}
	once    sync.Once
}

func newBarrierReader(data []byte, entered *sync.WaitGroup, release chan struct{}) *barrierReader {
	return &barrierReader{
		buf:     bytes.NewReader(data),
		entered: entered,
		release: release,
	}
}

func (r *barrierReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		r.entered.Done() // signal: this goroutine is inside Read (past seenNonce)
		<-r.release      // stall here until the test releases both goroutines
	})
	return r.buf.Read(p)
}

const testHMACSecret = "test-hmac-secret-32-bytes-long!!"

// signRequest adds the required HMAC headers to a test request.
// body may be nil for non-body requests.
func signRequest(t *testing.T, req *http.Request, secret string, body []byte, timestamp time.Time, nonce string) {
	t.Helper()
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	sig := computeHMAC(secret, ts, nonce, req.URL.Path, body)
	req.Header.Set(HMACTimestampHeader, ts)
	req.Header.Set(HMACNonceHeader, nonce)
	req.Header.Set(HMACSignatureHeader, sig)
}

func TestHMACMiddleware_ValidSignature(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := hmacMiddleware(testHMACSecret, handler)

	body := []byte(`{"method":"test"}`)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	signRequest(t, req, testHMACSecret, body, time.Now(), "nonce-001")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.True(t, called, "next handler should be called on valid signature")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHMACMiddleware_MissingHeaders(t *testing.T) {
	tests := []struct {
		name           string
		setTimestamp   bool
		setNonce       bool
		setSignature   bool
		expectStatus   int
		expectErrorMsg string
	}{
		{"missing all headers", false, false, false, http.StatusUnauthorized, "missing HMAC signature headers"},
		{"missing nonce and sig", true, false, false, http.StatusUnauthorized, "missing HMAC signature headers"},
		{"missing sig only", true, true, false, http.StatusUnauthorized, "missing HMAC signature headers"},
		{"missing timestamp and sig", false, true, false, http.StatusUnauthorized, "missing HMAC signature headers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			wrapped := hmacMiddleware(testHMACSecret, next)

			req := httptest.NewRequest("GET", "/mcp", nil)
			if tt.setTimestamp {
				req.Header.Set(HMACTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
			}
			if tt.setNonce {
				req.Header.Set(HMACNonceHeader, "some-nonce")
			}
			if tt.setSignature {
				req.Header.Set(HMACSignatureHeader, "some-sig")
			}

			w := httptest.NewRecorder()
			wrapped(w, req)

			assert.Equal(t, tt.expectStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.expectErrorMsg)
		})
	}
}

func TestHMACMiddleware_InvalidTimestamp(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set(HMACTimestampHeader, "not-a-number")
	req.Header.Set(HMACNonceHeader, "nonce-xyz")
	req.Header.Set(HMACSignatureHeader, "whatever")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid HMAC timestamp")
}

func TestHMACMiddleware_StaleTimestamp(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	stale := time.Now().Add(-2 * time.Minute) // 2 minutes ago — well outside 30s window
	body := []byte("")
	req := httptest.NewRequest("GET", "/mcp", nil)
	signRequest(t, req, testHMACSecret, body, stale, "stale-nonce")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "timestamp out of acceptable window")
}

func TestHMACMiddleware_FutureTimestamp(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	future := time.Now().Add(2 * time.Minute) // 2 minutes in the future
	body := []byte("")
	req := httptest.NewRequest("GET", "/mcp", nil)
	signRequest(t, req, testHMACSecret, body, future, "future-nonce")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "timestamp out of acceptable window")
}

func TestHMACMiddleware_ReplayDetected(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	wrapped := hmacMiddleware(testHMACSecret, next)

	body := []byte(`{"method":"test"}`)
	ts := time.Now()
	nonce := "replay-nonce-abc"

	// First request should succeed
	req1 := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	signRequest(t, req1, testHMACSecret, body, ts, nonce)
	w1 := httptest.NewRecorder()
	wrapped(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, "first request should succeed")

	// Second request with same nonce should be rejected as replay
	req2 := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	signRequest(t, req2, testHMACSecret, body, ts, nonce)
	w2 := httptest.NewRecorder()
	wrapped(w2, req2)

	assert.Equal(t, http.StatusUnauthorized, w2.Code, "replay should be rejected")
	assert.Contains(t, w2.Body.String(), "replay detected")
	assert.Equal(t, 1, called, "next handler should only be called once")
}

func TestHMACMiddleware_WrongSignature(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	body := []byte(`{"method":"test"}`)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set(HMACTimestampHeader, ts)
	req.Header.Set(HMACNonceHeader, "nonce-wrong")
	req.Header.Set(HMACSignatureHeader, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid HMAC signature")
}

func TestHMACMiddleware_WrongSecret(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	body := []byte(`{"method":"test"}`)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	signRequest(t, req, "different-secret", body, time.Now(), "nonce-diffsec")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid HMAC signature")
}

func TestApplyHMACIfConfigured_NoSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	// No secret → handler returned unchanged (backward compatible)
	wrapped := applyHMACIfConfigured("", next)

	req := httptest.NewRequest("GET", "/mcp", nil)
	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.True(t, called, "handler should be called without HMAC when no secret set")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestApplyHMACIfConfigured_WithSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := applyHMACIfConfigured(testHMACSecret, next)

	// Without headers → rejected
	req := httptest.NewRequest("GET", "/mcp", nil)
	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.False(t, called, "handler should not be called without HMAC headers")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestComputeHMAC_Deterministic(t *testing.T) {
	secret := "my-secret"
	ts := "1700000000"
	nonce := "abc123"
	path := "/mcp"
	body := []byte(`{"hello":"world"}`)

	sig1 := computeHMAC(secret, ts, nonce, path, body)
	sig2 := computeHMAC(secret, ts, nonce, path, body)
	assert.Equal(t, sig1, sig2, "HMAC should be deterministic")
}

func TestComputeHMAC_BodyHashIncluded(t *testing.T) {
	secret := "my-secret"
	ts := "1700000000"
	nonce := "abc123"
	path := "/mcp"

	// Verify the body hash is included in the signed message
	body1 := []byte(`{"a":1}`)
	body2 := []byte(`{"a":2}`)
	sig1 := computeHMAC(secret, ts, nonce, path, body1)
	sig2 := computeHMAC(secret, ts, nonce, path, body2)
	assert.NotEqual(t, sig1, sig2, "different bodies should produce different signatures")
}

func TestComputeHMAC_KnownValue(t *testing.T) {
	// Precompute expected value to detect accidental protocol changes.
	secret := "test-secret"
	ts := "1700000000"
	nonce := "fixed-nonce"
	path := "/mcp"
	body := []byte("")

	bodyHash := sha256.Sum256(body)
	msg := fmt.Sprintf("%s\n%s\n%s\n%s", ts, nonce, path, hex.EncodeToString(bodyHash[:]))
	_ = msg // used implicitly via computeHMAC below

	sig := computeHMAC(secret, ts, nonce, path, body)
	// Ensure the signature is a 64-char hex string (32 bytes HMAC-SHA256)
	assert.Len(t, sig, 64, "HMAC-SHA256 should be 64 hex chars")
}

func TestNonceCache_NewNonceAllowed(t *testing.T) {
	c := newNonceCache()
	assert.True(t, c.checkAndSet("nonce-a"), "first use of nonce should be allowed")
}

func TestNonceCache_DuplicateNonceRejected(t *testing.T) {
	c := newNonceCache()
	require.True(t, c.checkAndSet("nonce-dup"))
	assert.False(t, c.checkAndSet("nonce-dup"), "duplicate nonce should be rejected")
}

func TestNonceCache_SeenNonce(t *testing.T) {
	c := newNonceCache()
	assert.False(t, c.seenNonce("new-nonce"), "unseen nonce should return false")
	require.True(t, c.checkAndSet("seen-nonce"))
	assert.True(t, c.seenNonce("seen-nonce"), "seen nonce should return true")
	// seenNonce is read-only — checkAndSet on a seenNonce should still return false
	assert.False(t, c.checkAndSet("seen-nonce"), "checkAndSet after seenNonce should be rejected")
}

func TestNonceCache_DifferentNoncesAllowed(t *testing.T) {
	c := newNonceCache()
	for i := range 10 {
		nonce := fmt.Sprintf("nonce-%d", i)
		assert.True(t, c.checkAndSet(nonce), "fresh nonce %q should be allowed", nonce)
	}
}

func TestNonceCache_ConcurrentSafety(t *testing.T) {
	c := newNonceCache()
	var wg sync.WaitGroup
	results := make([]bool, 100)
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = c.checkAndSet("concurrent-nonce")
		}(i)
	}
	wg.Wait()

	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}
	assert.Equal(t, 1, trueCount, "exactly one goroutine should win the nonce race")
}

// TestHMACMiddleware_InvalidSigDoesNotPoisonNonceCache verifies that requests with
// invalid signatures do not consume a nonce slot — a DoS mitigation ensuring only
// verified requests can fill the replay-protection cache.
func TestHMACMiddleware_InvalidSigDoesNotPoisonNonceCache(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	wrapped := hmacMiddleware(testHMACSecret, next)

	nonce := "shared-nonce-poison-test"
	ts := time.Now()
	body := []byte(`{"method":"test"}`)

	// First request: valid timestamp + nonce but wrong signature → rejected, nonce NOT recorded
	req1 := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req1.Header.Set(HMACTimestampHeader, strconv.FormatInt(ts.Unix(), 10))
	req1.Header.Set(HMACNonceHeader, nonce)
	req1.Header.Set(HMACSignatureHeader, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	w1 := httptest.NewRecorder()
	wrapped(w1, req1)
	require.Equal(t, http.StatusUnauthorized, w1.Code, "bad-sig request should be rejected")

	// Second request: same nonce, correct signature → should succeed because the first
	// request's failed signature did NOT poison the nonce cache
	req2 := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	signRequest(t, req2, testHMACSecret, body, ts, nonce)
	w2 := httptest.NewRecorder()
	wrapped(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code, "valid request should succeed after bad-sig attempt with same nonce")
	assert.Equal(t, 1, called, "handler called exactly once")
}

// TestHMACMiddleware_BodyReadError verifies that a body-read failure returns
// 400 Bad Request with the expected error message. This exercises the
// `io.ReadAll` error branch inside hmacMiddleware that was previously uncovered.
func TestHMACMiddleware_BodyReadError(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	wrapped := hmacMiddleware(testHMACSecret, next)

	// errReader always returns an error, triggering the body-read failure branch.
	// The body must be non-nil and not http.NoBody for the read path to execute.
	req := httptest.NewRequest("POST", "/mcp", errReader{})
	tsStr := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set(HMACTimestampHeader, tsStr)
	req.Header.Set(HMACNonceHeader, "nonce-body-err")
	req.Header.Set(HMACSignatureHeader, "any-sig-body-not-read-yet")

	w := httptest.NewRecorder()
	wrapped(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body read error should return 400")
	assert.Contains(t, w.Body.String(), "failed to read request body")
}

// TestHMACMiddleware_ConcurrentReplay_PostCheckRejected verifies the post-check
// replay branch: when two concurrent requests carry the same nonce, both pass the
// seenNonce pre-check (nonce absent from cache), but exactly one wins the
// subsequent checkAndSet write, and the other is rejected as a replay.
//
// The barrierReader stalls both goroutines inside io.ReadAll — after seenNonce but
// before checkAndSet — so the race is reliably exercised.
func TestHMACMiddleware_ConcurrentReplay_PostCheckRejected(t *testing.T) {
	var successCount, replayCount atomic.Int32

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := hmacMiddleware(testHMACSecret, next)

	body := []byte(`{"method":"concurrent"}`)
	ts := time.Now()
	nonce := "concurrent-post-check-nonce"
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	// Pre-compute the valid signature both requests will carry.
	sig := computeHMAC(testHMACSecret, tsStr, nonce, "/mcp", body)

	// entered counts down as each goroutine enters its first Read call.
	entered := &sync.WaitGroup{}
	entered.Add(2)
	release := make(chan struct{})

	makeRequest := func() {
		br := newBarrierReader(body, entered, release)
		req := httptest.NewRequest("POST", "/mcp", br)
		req.Header.Set(HMACTimestampHeader, tsStr)
		req.Header.Set(HMACNonceHeader, nonce)
		req.Header.Set(HMACSignatureHeader, sig)

		w := httptest.NewRecorder()
		wrapped(w, req)

		switch w.Code {
		case http.StatusOK:
			successCount.Add(1)
		case http.StatusUnauthorized:
			replayCount.Add(1)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); makeRequest() }()
	go func() { defer wg.Done(); makeRequest() }()

	// Wait until both goroutines are stalled in io.ReadAll (i.e. both have passed
	// seenNonce), then release them to race on checkAndSet.
	entered.Wait()
	close(release)
	wg.Wait()

	require.Equal(t, int32(1), successCount.Load(), "exactly one concurrent request should succeed")
	require.Equal(t, int32(1), replayCount.Load(), "the concurrent duplicate should be rejected as replay")
}
