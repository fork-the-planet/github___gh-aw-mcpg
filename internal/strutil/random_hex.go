package strutil

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// RandomHex returns a hex-encoded string of n cryptographically random bytes.
// The returned string has length 2*n.
func RandomHex(n int) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("failed to generate random bytes: negative size %d", n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate %d random bytes: %w", n, err)
	}
	return hex.EncodeToString(b), nil
}

// RandomHexWithFallback returns a hex-encoded string of n random bytes.
// On the normal path it returns the same output as RandomHex(n) — a string of
// length 2*n containing cryptographically random hex characters.
// If crypto/rand is unavailable, it falls back to a hex-encoded pid+nanosecond
// value that is unique within a single process run. The fallback is non-cryptographic
// and should only arise in unusual runtime environments; it always produces a
// 32-character hex string (16 bytes), regardless of n. For the typical call site
// (n == 16) the fallback output length matches the normal output length.
func RandomHexWithFallback(n int) string {
	s, err := RandomHex(n)
	if err != nil {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint64(b[:8], uint64(os.Getpid()))
		binary.LittleEndian.PutUint64(b[8:], uint64(time.Now().UnixNano()))
		return hex.EncodeToString(b)
	}
	return s
}
