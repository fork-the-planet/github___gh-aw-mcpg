package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCircuitBreaker_InitialStateClosed verifies new circuit breakers start CLOSED.
func TestCircuitBreaker_InitialStateClosed(t *testing.T) {
	t.Parallel()
	cb := newCircuitBreaker("test", 3, 60*time.Second)
	assert.Equal(t, circuitClosed, cb.State())
	assert.NoError(t, cb.Allow())
}

// TestCircuitBreaker_OpensAfterThreshold verifies the circuit opens after N consecutive errors.
func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	t.Parallel()
	cb := newCircuitBreaker("test", 3, 60*time.Second)

	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitClosed, cb.State(), "should remain CLOSED after 1 error")
	assert.NoError(t, cb.Allow())

	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitClosed, cb.State(), "should remain CLOSED after 2 errors")
	assert.NoError(t, cb.Allow())

	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitOpen, cb.State(), "should be OPEN after 3 errors (threshold)")

	err := cb.Allow()
	require.Error(t, err, "OPEN circuit should reject requests")
	var openErr *ErrCircuitOpen
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, "test", openErr.ServerID)
}

// TestCircuitBreaker_SuccessResetsCounter verifies that a success resets the consecutive-error counter.
func TestCircuitBreaker_SuccessResetsCounter(t *testing.T) {
	t.Parallel()
	cb := newCircuitBreaker("test", 3, 60*time.Second)

	cb.RecordRateLimit(time.Time{})
	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitClosed, cb.State(), "still CLOSED after 2 errors")

	cb.RecordSuccess()
	assert.Equal(t, circuitClosed, cb.State(), "still CLOSED after success")

	// After a success the counter resets, so 2 more errors should NOT open the circuit.
	cb.RecordRateLimit(time.Time{})
	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitClosed, cb.State(), "should be CLOSED (counter reset by success)")
}

// TestCircuitBreaker_HalfOpenAfterCooldown verifies OPEN → HALF-OPEN transition.
func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Minute)
	cb.nowFunc = func() time.Time { return fakeNow }

	cb.RecordRateLimit(time.Time{})
	require.Equal(t, circuitOpen, cb.State(), "should be OPEN after 1 error")

	// Before cooldown: still OPEN.
	fakeNow = fakeNow.Add(30 * time.Second)
	require.Error(t, cb.Allow(), "should reject before cooldown elapses")

	// After cooldown: transitions to HALF-OPEN.
	fakeNow = fakeNow.Add(31 * time.Second)
	err := cb.Allow()
	assert.NoError(t, err, "should allow probe after cooldown")
	assert.Equal(t, circuitHalfOpen, cb.State(), "should be HALF-OPEN after cooldown")
}

// TestCircuitBreaker_HalfOpenClosesOnSuccess verifies HALF-OPEN → CLOSED on probe success.
func TestCircuitBreaker_HalfOpenClosesOnSuccess(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Minute)
	cb.nowFunc = func() time.Time { return fakeNow }

	cb.RecordRateLimit(time.Time{})
	require.Equal(t, circuitOpen, cb.State())

	fakeNow = fakeNow.Add(2 * time.Minute)
	require.NoError(t, cb.Allow()) // probe allowed

	cb.RecordSuccess()
	assert.Equal(t, circuitClosed, cb.State(), "should be CLOSED after probe success")
	assert.NoError(t, cb.Allow(), "CLOSED circuit should allow requests")
}

// TestCircuitBreaker_HalfOpenReOpensOnRateLimit verifies HALF-OPEN → OPEN on probe failure.
func TestCircuitBreaker_HalfOpenReOpensOnRateLimit(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Minute)
	cb.nowFunc = func() time.Time { return fakeNow }

	cb.RecordRateLimit(time.Time{})
	require.Equal(t, circuitOpen, cb.State())

	fakeNow = fakeNow.Add(2 * time.Minute)
	require.NoError(t, cb.Allow()) // probe allowed

	cb.RecordRateLimit(time.Time{})
	assert.Equal(t, circuitOpen, cb.State(), "should be OPEN again after probe is rate-limited")

	err := cb.Allow()
	require.Error(t, err)
	var openErr *ErrCircuitOpen
	require.ErrorAs(t, err, &openErr)
}

// TestCircuitBreaker_ResetAtFromHeader verifies the reset time from upstream is used.
func TestCircuitBreaker_ResetAtFromHeader(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Hour)
	cb.nowFunc = func() time.Time { return fakeNow }

	resetAt := fakeNow.Add(30 * time.Second)
	cb.RecordRateLimit(resetAt)
	require.Equal(t, circuitOpen, cb.State())

	// Before the reset time: still OPEN.
	fakeNow = fakeNow.Add(15 * time.Second)
	require.Error(t, cb.Allow())

	// After the reset time: transitions to HALF-OPEN (before cooldown would elapse).
	fakeNow = fakeNow.Add(20 * time.Second)
	err := cb.Allow()
	assert.NoError(t, err, "should allow probe after reset time")
	assert.Equal(t, circuitHalfOpen, cb.State())
}

// TestCircuitBreaker_HalfOpenBlocksConcurrentProbes verifies that only one probe is allowed in HALF-OPEN.
func TestCircuitBreaker_HalfOpenBlocksConcurrentProbes(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Minute)
	cb.nowFunc = func() time.Time { return fakeNow }

	cb.RecordRateLimit(time.Time{})
	require.Equal(t, circuitOpen, cb.State())

	// Advance past cooldown to trigger HALF-OPEN.
	fakeNow = fakeNow.Add(2 * time.Minute)

	// First Allow() should succeed (the probe).
	require.NoError(t, cb.Allow())
	assert.Equal(t, circuitHalfOpen, cb.State())

	// Second Allow() should be rejected — probe is already in flight.
	err := cb.Allow()
	require.Error(t, err, "concurrent requests in HALF-OPEN should be rejected")
	var openErr *ErrCircuitOpen
	require.ErrorAs(t, err, &openErr)

	// After the probe succeeds, requests should be allowed again.
	cb.RecordSuccess()
	assert.Equal(t, circuitClosed, cb.State())
	assert.NoError(t, cb.Allow())
}

// TestCircuitBreaker_DefaultsApplied verifies zero-value config gets sensible defaults.
func TestCircuitBreaker_DefaultsApplied(t *testing.T) {
	t.Parallel()
	cb := newCircuitBreaker("test", 0, 0)
	assert.Equal(t, DefaultRateLimitThreshold, cb.threshold)
	assert.Equal(t, DefaultRateLimitCooldown, cb.cooldown)
}

// TestCircuitBreaker_ErrOpenMessage verifies ErrCircuitOpen.Error() content.
func TestCircuitBreaker_ErrOpenMessage(t *testing.T) {
	t.Parallel()

	t.Run("no reset time", func(t *testing.T) {
		t.Parallel()
		err := &ErrCircuitOpen{ServerID: "github"}
		assert.ErrorContains(t, err, "github")
		assert.ErrorContains(t, err, "OPEN")
	})

	t.Run("with reset time", func(t *testing.T) {
		t.Parallel()
		reset := time.Now().Add(30 * time.Second)
		err := &ErrCircuitOpen{ServerID: "github", ResetAt: reset}
		assert.ErrorContains(t, err, "github")
		assert.ErrorContains(t, err, "retry after")
	})
}

// TestIsRateLimitToolResult verifies rate-limit detection from GitHub MCP tool results.
func TestIsRateLimitToolResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		result    interface{}
		wantHit   bool
		wantReset bool // whether a non-zero reset time is expected
	}{
		{
			name: "standard rate limit exceeded message",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "failed to search repositories: 403 API rate limit exceeded [rate reset in 42s]",
					},
				},
			},
			wantHit:   true,
			wantReset: true,
		},
		{
			name: "secondary rate limit",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "secondary rate limit triggered",
					},
				},
			},
			wantHit: true,
		},
		{
			name: "too many requests",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "too many requests",
					},
				},
			},
			wantHit: true,
		},
		{
			name: "non-rate-limit error",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "repository not found",
					},
				},
			},
			wantHit: false,
		},
		{
			name: "successful result (isError false)",
			result: map[string]interface{}{
				"isError": false,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "API rate limit exceeded but isError is false",
					},
				},
			},
			wantHit: false,
		},
		{
			name:    "nil result",
			result:  nil,
			wantHit: false,
		},
		{
			name:    "non-map result",
			result:  "some string",
			wantHit: false,
		},
		{
			name: "rate reset in 0s",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "API rate limit exceeded [rate reset in 0s]",
					},
				},
			},
			wantHit:   true,
			wantReset: false, // 0s means no future time
		},
		{
			name: "non-map content items are skipped before matching",
			result: map[string]interface{}{
				"isError": true,
				"content": []interface{}{
					"not-a-map", // triggers the !ok continue branch
					map[string]interface{}{
						"type": "text",
						"text": "API rate limit exceeded",
					},
				},
			},
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hit, resetAt := isRateLimitToolResult(tt.result)
			assert.Equal(t, tt.wantHit, hit, "isRateLimitToolResult mismatch")
			if tt.wantReset {
				assert.False(t, resetAt.IsZero(), "expected non-zero resetAt")
			}
		})
	}
}

// TestParseRateLimitResetFromText verifies reset time parsing from error messages.
func TestParseRateLimitResetFromText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		wantZero bool
	}{
		{
			name:     "42 seconds",
			text:     "rate limit exceeded [rate reset in 42s]",
			wantZero: false,
		},
		{
			name:     "0 seconds gives zero time",
			text:     "rate limit exceeded [rate reset in 0s]",
			wantZero: true,
		},
		{
			name:     "no pattern",
			text:     "some other error",
			wantZero: true,
		},
		{
			name:     "pattern without s/]) terminator gives zero time",
			text:     "rate reset in 42",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseRateLimitResetFromText(tt.text)
			if tt.wantZero {
				assert.True(t, got.IsZero(), "expected zero time, got %v", got)
			} else {
				assert.False(t, got.IsZero(), "expected non-zero time")
				assert.True(t, got.After(time.Now()), "expected future time")
			}
		})
	}
}

// TestExtractRateLimitErrorText verifies extraction of error text from backend results.
func TestExtractRateLimitErrorText(t *testing.T) {
	t.Parallel()

	t.Run("extracts text from standard rate-limit result", func(t *testing.T) {
		t.Parallel()
		result := map[string]interface{}{
			"isError": true,
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "failed to search: 403 API rate limit exceeded [rate reset in 42s]",
				},
			},
		}
		assert.Equal(t, "failed to search: 403 API rate limit exceeded [rate reset in 42s]", extractRateLimitErrorText(result))
	})

	t.Run("returns fallback for nil result", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "rate limit exceeded", extractRateLimitErrorText(nil))
	})

	t.Run("returns fallback for empty content", func(t *testing.T) {
		t.Parallel()
		result := map[string]interface{}{"isError": true, "content": []interface{}{}}
		assert.Equal(t, "rate limit exceeded", extractRateLimitErrorText(result))
	})

	t.Run("skips non-map content items and returns text from valid item", func(t *testing.T) {
		t.Parallel()
		result := map[string]interface{}{
			"content": []interface{}{
				"not-a-map", // skipped via the !ok continue branch
				map[string]interface{}{
					"text": "API rate limit exceeded",
				},
			},
		}
		assert.Equal(t, "API rate limit exceeded", extractRateLimitErrorText(result))
	})

	t.Run("returns fallback when all content items are non-maps", func(t *testing.T) {
		t.Parallel()
		result := map[string]interface{}{
			"content": []interface{}{"not-a-map", 42},
		}
		assert.Equal(t, "rate limit exceeded", extractRateLimitErrorText(result))
	})
}

// TestCircuitBreakerState_String verifies the string representation of each circuit breaker state.
func TestCircuitBreakerState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state circuitBreakerState
		want  string
	}{
		{circuitClosed, "CLOSED"},
		{circuitOpen, "OPEN"},
		{circuitHalfOpen, "HALF-OPEN"},
		{circuitBreakerState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

// TestIsRateLimitText_Direct directly verifies isRateLimitText with each pattern and edge cases.
func TestIsRateLimitText_Direct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "rate limit exceeded",
			text: "403 API rate limit exceeded",
			want: true,
		},
		{
			name: "rate limit combined with 403 status",
			text: "error: rate limit triggered, status 403",
			want: true,
		},
		{
			name: "api rate limit phrase",
			text: "api rate limit reached",
			want: true,
		},
		{
			name: "secondary rate limit phrase",
			text: "secondary rate limit triggered",
			want: true,
		},
		{
			name: "too many requests phrase",
			text: "too many requests, please slow down",
			want: true,
		},
		{
			name: "case insensitive match",
			text: "RATE LIMIT EXCEEDED",
			want: true,
		},
		{
			name: "rate limit phrase without 403 or qualifier",
			text: "rate limit information page",
			want: false,
		},
		{
			name: "unrelated error",
			text: "repository not found",
			want: false,
		},
		{
			name: "empty string",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isRateLimitText(tt.text))
		})
	}
}

// TestCircuitBreaker_RecordSuccessFromOpenState verifies that calling RecordSuccess on an
// OPEN circuit (e.g. an in-flight request completing after the circuit opened) closes
// the circuit directly, exercising the "else if prev != circuitClosed" branch.
func TestCircuitBreaker_RecordSuccessFromOpenState(t *testing.T) {
	t.Parallel()
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Minute)
	cb.nowFunc = func() time.Time { return fakeNow }

	cb.RecordRateLimit(time.Time{})
	require.Equal(t, circuitOpen, cb.State())

	// Simulate an in-flight request completing successfully while the circuit is OPEN.
	cb.RecordSuccess()
	assert.Equal(t, circuitClosed, cb.State(), "RecordSuccess from OPEN should close the circuit")
	assert.NoError(t, cb.Allow(), "CLOSED circuit should allow requests after RecordSuccess from OPEN")
}

// TestCircuitBreaker_HalfOpenAllowsWhenNoProbeInFlight exercises the defensive
// fallback path in Allow() where the circuit is HALF-OPEN but probeInFlight is false.
// This shouldn't normally occur, but the circuit should allow through defensively.
func TestCircuitBreaker_HalfOpenAllowsWhenNoProbeInFlight(t *testing.T) {
	t.Parallel()
	cb := newCircuitBreaker("test", 1, time.Minute)

	// Manually set the state to HALF-OPEN with no probe in flight.
	cb.mu.Lock()
	cb.state = circuitHalfOpen
	cb.probeInFlight = false
	cb.mu.Unlock()

	err := cb.Allow()
	assert.NoError(t, err, "HALF-OPEN with no probe in flight should allow through defensively")
}

// TestCircuitBreaker_RecordRateLimitWhenAlreadyOpen verifies that calling RecordRateLimit
// on an already-OPEN circuit keeps it OPEN and updates the reset time.
func TestCircuitBreaker_RecordRateLimitWhenAlreadyOpen(t *testing.T) {
	t.Parallel()

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := newCircuitBreaker("test", 1, time.Hour)
	cb.nowFunc = func() time.Time { return fakeNow }

	initialReset := fakeNow.Add(30 * time.Second)
	cb.RecordRateLimit(initialReset)
	require.Equal(t, circuitOpen, cb.State(), "should be OPEN after threshold errors")

	// Record another rate limit while already OPEN with a later reset time.
	laterReset := fakeNow.Add(90 * time.Second)
	cb.RecordRateLimit(laterReset)
	assert.Equal(t, circuitOpen, cb.State(), "should remain OPEN")

	// The reset time should be updated to the later value.
	cb.mu.Lock()
	gotReset := cb.resetAt
	cb.mu.Unlock()
	assert.Equal(t, laterReset, gotReset, "resetAt should be updated to the later reset time")
}
