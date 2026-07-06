package server

// Additional coverage tests for githubhttp.ParseRateLimitResetFromText edge cases
// not covered by circuit_breaker_test.go.

import (
	"testing"
	"time"

	"github.com/github/gh-aw-mcpg/internal/githubhttp"
	"github.com/stretchr/testify/assert"
)

// TestParseRateLimitResetFromText_AdditionalEdgeCases covers code paths in
// githubhttp.ParseRateLimitResetFromText that are not exercised by the existing test suite:
//
//   - Negative seconds: exercises the secs <= 0 branch
//   - Non-numeric digits: exercises the err != nil branch from strconv.ParseInt
//   - Uppercase/mixed-case input: verifies that the case-insensitive search
//     ("rate reset in") still correctly slices into the original string
//   - Bracket terminator: the IndexAny call accepts ']' as well as 's'; ensure
//     the number is still parsed correctly when the text ends with ']'
//   - Large value (3600 s): a 1-hour reset window — valid, should return a
//     future time
//   - Whitespace padding: TrimSpace is called on the captured digit substring;
//     confirm padding before the number is handled
func TestParseRateLimitResetFromText_AdditionalEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		wantZero bool
	}{
		{
			// secs == -5, which satisfies secs <= 0 → zero time.
			name:     "negative seconds returns zero time",
			text:     "API rate limit exceeded [rate reset in -5s]",
			wantZero: true,
		},
		{
			// ParseInt("abc", 10, 64) returns an error → zero time.
			name:     "non-numeric seconds returns zero time",
			text:     "rate limit exceeded [rate reset in abcs]",
			wantZero: true,
		},
		{
			// The function lower-cases the input to locate the pattern but
			// then indexes into the *original* text for the digit substring.
			// The digit terminator search is case-sensitive ('s' not 'S'), so
			// the test uses a lowercase 's' terminator to ensure the offset
			// arithmetic from the case-insensitive search is still correct.
			name:     "uppercase input with lowercase terminator succeeds",
			text:     "RATE LIMIT EXCEEDED [RATE RESET IN 10s]",
			wantZero: false,
		},
		{
			// When the terminator is uppercase 'S' (not in IndexAny's "s])")
			// the search falls through to ']', making the captured substring
			// "10S" which fails ParseInt — returns zero time.
			name:     "uppercase terminator S is not recognised, falls to bracket",
			text:     "RATE LIMIT EXCEEDED [RATE RESET IN 10S]",
			wantZero: true,
		},
		{
			// IndexAny(rest, "s])") will match ']' before it sees 's'; the
			// digit portion must still be parsed correctly.
			name:     "bracket terminator is accepted",
			text:     "API rate limit exceeded [rate reset in 30]",
			wantZero: false,
		},
		{
			// A 1-hour (3600 s) reset window is a valid large value.
			name:     "large seconds value is valid",
			text:     "secondary rate limit exceeded [rate reset in 3600s]",
			wantZero: false,
		},
		{
			// TrimSpace is applied to the digit slice before ParseInt.
			// A leading space inside the bracket (" 15") should still parse.
			name:     "whitespace-padded seconds are trimmed and parsed",
			text:     "rate limit exceeded [rate reset in  15s]",
			wantZero: false,
		},
		{
			// A reset of exactly 1 second — minimum positive value.
			name:     "one second reset is valid",
			text:     "rate reset in 1s",
			wantZero: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := time.Now()
			got := githubhttp.ParseRateLimitResetFromText(tt.text)
			if tt.wantZero {
				assert.True(t, got.IsZero(), "expected zero time for %q, got %v", tt.text, got)
			} else {
				assert.False(t, got.IsZero(), "expected non-zero time for %q", tt.text)
				assert.True(t, got.After(before), "expected future reset time for %q, got %v", tt.text, got)
			}
		})
	}
}
