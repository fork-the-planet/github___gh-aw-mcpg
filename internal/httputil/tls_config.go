package httputil

import "crypto/tls"

// MinTLSVersion is the minimum TLS version enforced across gateway listeners
// and outbound HTTP clients.
const MinTLSVersion = tls.VersionTLS12

// NewServerTLSConfig creates a server TLS config with the standard minimum TLS
// version and a single certificate chain.
func NewServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   MinTLSVersion,
	}
}

// NewClientTLSConfig creates an HTTP client TLS config with the standard
// minimum TLS version.
func NewClientTLSConfig() *tls.Config {
	return &tls.Config{MinVersion: MinTLSVersion}
}
