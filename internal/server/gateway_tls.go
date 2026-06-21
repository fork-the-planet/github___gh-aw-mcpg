package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logGatewayTLS = logger.New("server:tls")

// LoadGatewayTLS loads a TLS configuration for the gateway HTTP server from PEM
// certificate and key files. When caPath is non-empty the returned config
// requires client certificates signed by that CA (mutual TLS / mTLS).
//
// Pass an empty caPath to use one-way TLS (server-only authentication).
//
// Example — one-way TLS (server cert only):
//
//	tlsCfg, err := LoadGatewayTLS("/path/server.crt", "/path/server.key", "")
//
// Example — mutual TLS (client certs required):
//
//	tlsCfg, err := LoadGatewayTLS("/path/server.crt", "/path/server.key", "/path/ca.crt")
func LoadGatewayTLS(certPath, keyPath, caPath string) (*tls.Config, error) {
	logGatewayTLS.Printf("loading gateway TLS: cert=%s, key=%s, ca=%s", certPath, keyPath, caPath)

	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server TLS certificate/key: %w", err)
	}
	logGatewayTLS.Printf("server TLS key pair loaded: certChainLen=%d", len(serverCert.Certificate))

	cfg := httputil.NewServerTLSConfig(serverCert)

	if caPath != "" {
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", caPath)
		}
		logGatewayTLS.Printf("CA certificate pool built: ca=%s", caPath)

		// Require and verify client certificates signed by the provided CA.
		cfg.ClientCAs = caPool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		logGatewayTLS.Printf("mTLS enabled: client certificates required, CA=%s", caPath)
	} else {
		logGatewayTLS.Print("one-way TLS configured: client certificates not required")
	}

	logGatewayTLS.Printf("gateway TLS configuration ready: minVersion=%s, mtls=%v", tls.VersionName(cfg.MinVersion), caPath != "")
	return cfg, nil
}
