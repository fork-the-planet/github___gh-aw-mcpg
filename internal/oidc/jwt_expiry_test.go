package oidc

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRawJWT assembles header.payload.signature using RawURLEncoding (no padding),
// which is how real JWTs are formed. The payload is already base64-encoded by the caller.
func makeRawJWT(rawPayload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	return fmt.Sprintf("%s.%s.dummysig", header, rawPayload)
}

// encodePayloadRaw encodes raw JSON as base64url without padding (standard JWT format).
func encodePayloadRaw(json string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(json))
}

// TestExtractJWTExpiry_ValidToken_NoPaddingNeeded tests a JWT whose payload raw-base64
// length is divisible by 4 (no "=" padding characters need to be added).
//
// {"exp":1} encodes to "eyJleHAiOjF9" — length 12, 12%4 == 0.
func TestExtractJWTExpiry_ValidToken_NoPaddingNeeded(t *testing.T) {
	// payload raw length = 12, mod4 = 0 → no padding added
	rawPayload := encodePayloadRaw(`{"exp":1}`)
	assert.Equal(t, 12, len(rawPayload))
	assert.Equal(t, 0, len(rawPayload)%4)

	token := makeRawJWT(rawPayload)
	got, err := extractJWTExpiry(token)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(1, 0), got)
}

// TestExtractJWTExpiry_ValidToken_TwoCharPadding tests a JWT whose payload raw-base64
// length is 2 mod 4, so "==" must be appended before decoding.
//
// {"exp":12} encodes to "eyJleHAiOjEyfQ" — length 14, 14%4 == 2.
func TestExtractJWTExpiry_ValidToken_TwoCharPadding(t *testing.T) {
	// payload raw length = 14, mod4 = 2 → "==" appended
	rawPayload := encodePayloadRaw(`{"exp":12}`)
	assert.Equal(t, 14, len(rawPayload))
	assert.Equal(t, 2, len(rawPayload)%4)

	token := makeRawJWT(rawPayload)
	got, err := extractJWTExpiry(token)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(12, 0), got)
}

// TestExtractJWTExpiry_ValidToken_OneCharPadding tests a JWT whose payload raw-base64
// length is 3 mod 4, so "=" must be appended before decoding.
//
// {"exp":123} encodes to "eyJleHAiOjEyM30" — length 15, 15%4 == 3.
func TestExtractJWTExpiry_ValidToken_OneCharPadding(t *testing.T) {
	// payload raw length = 15, mod4 = 3 → "=" appended
	rawPayload := encodePayloadRaw(`{"exp":123}`)
	assert.Equal(t, 15, len(rawPayload))
	assert.Equal(t, 3, len(rawPayload)%4)

	token := makeRawJWT(rawPayload)
	got, err := extractJWTExpiry(token)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(123, 0), got)
}

// TestExtractJWTExpiry_RealisticExpiry tests a JWT with a realistic Unix timestamp.
func TestExtractJWTExpiry_RealisticExpiry(t *testing.T) {
	const expUnix = int64(1735689600) // 2025-01-01 00:00:00 UTC
	rawPayload := encodePayloadRaw(fmt.Sprintf(`{"exp":%d}`, expUnix))
	token := makeRawJWT(rawPayload)

	got, err := extractJWTExpiry(token)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(expUnix, 0), got)
}

// TestExtractJWTExpiry_ExtraClaimsIgnored verifies that unrelated JWT claims
// (iss, sub, aud, iat) are silently ignored and do not affect expiry extraction.
func TestExtractJWTExpiry_ExtraClaimsIgnored(t *testing.T) {
	const expUnix = int64(9999999999)
	rawPayload := encodePayloadRaw(fmt.Sprintf(
		`{"iss":"https://example.com","sub":"user:42","aud":"https://api.example.com","iat":1700000000,"exp":%d}`,
		expUnix,
	))
	token := makeRawJWT(rawPayload)

	got, err := extractJWTExpiry(token)
	require.NoError(t, err)
	assert.Equal(t, time.Unix(expUnix, 0), got)
}

// TestExtractJWTExpiry_ZeroExp tests that a JWT with exp=0 returns an appropriate
// error, since zero means "no expiry claim" in this context.
func TestExtractJWTExpiry_ZeroExp(t *testing.T) {
	rawPayload := encodePayloadRaw(`{"exp":0}`)
	token := makeRawJWT(rawPayload)

	_, err := extractJWTExpiry(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT has no exp claim")
}

// TestExtractJWTExpiry_MissingExpClaim verifies that a JWT without any exp field
// also returns an error (claims.Exp will be zero-valued).
func TestExtractJWTExpiry_MissingExpClaim(t *testing.T) {
	rawPayload := encodePayloadRaw(`{"iss":"https://example.com","sub":"user:42"}`)
	token := makeRawJWT(rawPayload)

	_, err := extractJWTExpiry(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT has no exp claim")
}

// TestExtractJWTExpiry_WrongPartCount verifies that tokens with a part count
// other than 3 (separated by ".") are rejected with a descriptive error.
func TestExtractJWTExpiry_WrongPartCount(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		wantParts   int
	}{
		{"one part", "headeronly", 1},
		{"two parts", "header.payload", 2},
		{"four parts", "a.b.c.d", 4},
		{"five parts", "a.b.c.d.e", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractJWTExpiry(tt.token)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "malformed JWT")
			assert.Contains(t, err.Error(), fmt.Sprintf("got %d", tt.wantParts))
		})
	}
}

// TestExtractJWTExpiry_InvalidBase64Payload verifies that a JWT whose payload
// segment is not valid base64url returns a decode error.
func TestExtractJWTExpiry_InvalidBase64Payload(t *testing.T) {
	invalidBase64 := "!!!not-base64!!!"
	token := fmt.Sprintf("header.%s.sig", invalidBase64)

	_, err := extractJWTExpiry(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode JWT payload")
}

// TestExtractJWTExpiry_InvalidJSONPayload verifies that a JWT whose payload
// is valid base64 but contains non-JSON content returns a parse error.
func TestExtractJWTExpiry_InvalidJSONPayload(t *testing.T) {
	invalidJSON := base64.URLEncoding.EncodeToString([]byte(`{not valid json`))
	token := fmt.Sprintf("header.%s.sig", invalidJSON)

	_, err := extractJWTExpiry(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JWT claims")
}

// TestExtractJWTExpiry_EmptyPayload verifies that an empty payload segment
// is handled gracefully (empty JSON object results in zero exp).
func TestExtractJWTExpiry_EmptyPayload(t *testing.T) {
	emptyPayload := base64.URLEncoding.EncodeToString([]byte(`{}`))
	token := fmt.Sprintf("header.%s.sig", emptyPayload)

	_, err := extractJWTExpiry(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT has no exp claim")
}

// TestExtractJWTExpiry_EmptyToken verifies that an empty string is rejected.
func TestExtractJWTExpiry_EmptyToken(t *testing.T) {
	_, err := extractJWTExpiry("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed JWT")
}
