package httputil

import (
	"crypto/tls"
)

// MinTLSVersion is the minimum TLS version enforced across all gateway listeners
// and clients. Centralising this constant ensures a single point of change if
// the policy is tightened (e.g., to tls.VersionTLS13).
const MinTLSVersion = tls.VersionTLS12

// NewServerTLSConfig returns a *tls.Config for TLS server listeners carrying
// the provided certificate and the gateway-wide minimum TLS version.
func NewServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   MinTLSVersion,
	}
}

// NewClientTLSConfig returns a *tls.Config suitable for outbound HTTP clients,
// enforcing the gateway-wide minimum TLS version.
func NewClientTLSConfig() *tls.Config {
	return &tls.Config{MinVersion: MinTLSVersion}
}
