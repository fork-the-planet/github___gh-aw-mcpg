package proxy

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestInjectRetryAfterIfRateLimited verifies Retry-After injection and logging for
// rate-limited upstream responses.
func TestInjectRetryAfterIfRateLimited(t *testing.T) {
	t.Parallel()

	t.Run("HTTP 429 injects Retry-After", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		future := time.Now().Add(30 * time.Second)
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"X-Ratelimit-Reset": []string{strconv.FormatInt(future.Unix(), 10)},
			},
		}
		injectRetryAfterIfRateLimited(w, resp)
		retryAfter := w.Header().Get("Retry-After")
		assert.NotEmpty(t, retryAfter, "Retry-After should be set on 429")
		secs, err := strconv.Atoi(retryAfter)
		assert.NoError(t, err)
		assert.Greater(t, secs, 0, "Retry-After should be positive")
	})

	t.Run("X-Ratelimit-Remaining 0 injects Retry-After", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		future := time.Now().Add(60 * time.Second)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Ratelimit-Remaining": []string{"0"},
				"X-Ratelimit-Reset":     []string{strconv.FormatInt(future.Unix(), 10)},
			},
		}
		injectRetryAfterIfRateLimited(w, resp)
		assert.NotEmpty(t, w.Header().Get("Retry-After"), "Retry-After should be set when remaining=0")
	})

	t.Run("non-zero remaining does not inject Retry-After", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Ratelimit-Remaining": []string{"100"},
			},
		}
		injectRetryAfterIfRateLimited(w, resp)
		assert.Empty(t, w.Header().Get("Retry-After"))
	})

	t.Run("200 with no rate-limit headers does not inject Retry-After", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
		}
		injectRetryAfterIfRateLimited(w, resp)
		assert.Empty(t, w.Header().Get("Retry-After"))
	})

	t.Run("429 without reset header uses default delay", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
		}
		injectRetryAfterIfRateLimited(w, resp)
		retryAfter := w.Header().Get("Retry-After")
		assert.Equal(t, "60", retryAfter, "default delay should be 60 seconds")
	})
}

// TestComputeRetryAfter verifies the retry-after calculation.
func TestComputeRetryAfter(t *testing.T) {
	t.Parallel()

	t.Run("zero time returns default", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 60, computeRetryAfter(time.Time{}))
	})

	t.Run("past time returns default", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 60, computeRetryAfter(time.Now().Add(-time.Minute)))
	})

	t.Run("future time returns seconds until reset", func(t *testing.T) {
		t.Parallel()
		future := time.Now().Add(30 * time.Second)
		secs := computeRetryAfter(future)
		// Allow ±2s for timing jitter.
		assert.GreaterOrEqual(t, secs, 29)
		assert.LessOrEqual(t, secs, 32)
	})

	t.Run("very far future is clamped to max", func(t *testing.T) {
		t.Parallel()
		farFuture := time.Now().Add(24 * time.Hour)
		assert.Equal(t, 3600, computeRetryAfter(farFuture))
	})
}
