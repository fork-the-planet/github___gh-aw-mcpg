package httputil

import (
	"crypto/tls"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServerTLSConfig(t *testing.T) {
	cert := tls.Certificate{Certificate: [][]byte{[]byte("leaf-cert")}}

	cfg := NewServerTLSConfig(cert)

	assert.NotNil(t, cfg)
	assert.EqualValues(t, MinTLSVersion, cfg.MinVersion)
	assert.Len(t, cfg.Certificates, 1)
	assert.Equal(t, cert, cfg.Certificates[0])
}

func TestNewClientTLSConfig(t *testing.T) {
	cfg := NewClientTLSConfig()

	assert.NotNil(t, cfg)
	assert.EqualValues(t, MinTLSVersion, cfg.MinVersion)
	assert.Empty(t, cfg.Certificates)
}

func TestConfigureTLSTrustEnvironment(t *testing.T) {
	t.Run("sets all trust env vars to the given path", func(t *testing.T) {
		// Unset all keys before the test so we start from a clean state.
		for _, key := range TLSTrustEnvKeys() {
			t.Setenv(key, "")
		}

		const caPath = "/tmp/ca.crt"
		err := ConfigureTLSTrustEnvironment(caPath)
		require.NoError(t, err)

		for _, key := range TLSTrustEnvKeys() {
			assert.Equal(t, caPath, os.Getenv(key), "expected %s to be set to %s", key, caPath)
		}
	})

	t.Run("does not rely on GITHUB_ENV file writes", func(t *testing.T) {
		assert := assert.New(t)
		githubEnvFile := t.TempDir() + "/github_env"
		const original = "UNCHANGED=1\n"
		require.NoError(t, os.WriteFile(githubEnvFile, []byte(original), 0o644))
		t.Setenv("GITHUB_ENV", githubEnvFile)
		for _, key := range TLSTrustEnvKeys() {
			t.Setenv(key, "")
		}

		const caPath = "/tmp/ca.crt"
		require.NoError(t, ConfigureTLSTrustEnvironment(caPath))

		for _, key := range TLSTrustEnvKeys() {
			assert.Equal(caPath, os.Getenv(key), "expected %s to be set", key)
		}

		content, err := os.ReadFile(githubEnvFile)
		require.NoError(t, err)
		assert.Equal(original, string(content))
	})

	t.Run("returns a defensive copy of trust env keys", func(t *testing.T) {
		keys := TLSTrustEnvKeys()
		require.NotEmpty(t, keys)
		originalFirst := keys[0]
		keys[0] = "MODIFIED_KEY"

		keysAfter := TLSTrustEnvKeys()
		assert.Equal(t, originalFirst, keysAfter[0])
	})

	t.Run("rejects path with embedded newline", func(t *testing.T) {
		err := ConfigureTLSTrustEnvironment("/tmp/ca\n.crt")
		assert.ErrorContains(t, err, "invalid TLS CA cert path contains newline")
	})

	t.Run("rejects path with embedded carriage return", func(t *testing.T) {
		err := ConfigureTLSTrustEnvironment("/tmp/ca\r.crt")
		assert.ErrorContains(t, err, "invalid TLS CA cert path contains newline")
	})
}
