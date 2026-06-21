package httputil

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
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
