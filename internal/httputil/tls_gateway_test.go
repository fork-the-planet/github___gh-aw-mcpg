package httputil_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw-mcpg/internal/httputil"
	"github.com/github/gh-aw-mcpg/internal/proxy"
)

// generateClientCert creates an ephemeral client certificate signed by the CA
// whose cert is at caCertPath and whose private key is at caKeyPath.
// It returns a tls.Certificate with ExtKeyUsageClientAuth.
func generateClientCert(t *testing.T, dir, caCertPath, caKeyPath string) (tls.Certificate, error) {
	t.Helper()

	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	block, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, err
	}

	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return tls.Certificate{}, err
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPath := filepath.Join(dir, "client.crt")
	if err := writePEMFile(certPath, "CERTIFICATE", certDER, 0644); err != nil {
		return tls.Certificate{}, err
	}

	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPath := filepath.Join(dir, "client.key")
	if err := writePEMFile(keyPath, "EC PRIVATE KEY", clientKeyDER, 0600); err != nil {
		return tls.Certificate{}, err
	}

	return tls.LoadX509KeyPair(certPath, keyPath)
}

// writePEMFile writes a DER-encoded block as PEM to path with the given mode.
func writePEMFile(path, blockType string, derBytes []byte, mode os.FileMode) error {
	data := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: derBytes})
	return os.WriteFile(path, data, mode)
}

// generateMTLSCerts generates a CA, server cert, and client cert for mTLS tests.
// The server cert has ExtKeyUsageServerAuth; the client cert has ExtKeyUsageClientAuth.
type mtlsCerts struct {
	caCertPath     string
	serverCertPath string
	serverKeyPath  string
	clientCert     tls.Certificate
	caPool         *x509.CertPool
}

func generateMTLSCerts(t *testing.T, dir string) (*mtlsCerts, error) {
	t.Helper()

	// --- CA ---
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, err
	}

	caCertPath := filepath.Join(dir, "ca.crt")
	if err := writePEMFile(caCertPath, "CERTIFICATE", caCertDER, 0644); err != nil {
		return nil, err
	}

	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, err
	}
	caKeyPath := filepath.Join(dir, "ca.key")
	if err := writePEMFile(caKeyPath, "EC PRIVATE KEY", caKeyDER, 0600); err != nil {
		return nil, err
	}

	// --- Server cert ---
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serverSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	serverCertPath := filepath.Join(dir, "server.crt")
	if err := writePEMFile(serverCertPath, "CERTIFICATE", serverCertDER, 0644); err != nil {
		return nil, err
	}

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, err
	}
	serverKeyPath := filepath.Join(dir, "server.key")
	if err := writePEMFile(serverKeyPath, "EC PRIVATE KEY", serverKeyDER, 0600); err != nil {
		return nil, err
	}

	// --- Client cert ---
	clientTLSCert, err := generateClientCert(t, dir, caCertPath, caKeyPath)
	if err != nil {
		return nil, err
	}

	caPool := x509.NewCertPool()
	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, err
	}
	caPool.AppendCertsFromPEM(caPEM)

	return &mtlsCerts{
		caCertPath:     caCertPath,
		serverCertPath: serverCertPath,
		serverKeyPath:  serverKeyPath,
		clientCert:     clientTLSCert,
		caPool:         caPool,
	}, nil
}

func TestLoadGatewayTLS_ServerOnly(t *testing.T) {
	dir := t.TempDir()
	tlsCfg, err := proxy.GenerateSelfSignedTLS(dir)
	require.NoError(t, err)

	cfg, err := httputil.LoadGatewayTLS(tlsCfg.CertPath, tlsCfg.KeyPath, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.Certificates, 1, "should load one certificate")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, tls.NoClientCert, cfg.ClientAuth, "server-only TLS should not require client certs")
	assert.Nil(t, cfg.ClientCAs, "server-only TLS should have no CA pool")
}

func TestLoadGatewayTLS_MutualTLS(t *testing.T) {
	dir := t.TempDir()
	tlsCfg, err := proxy.GenerateSelfSignedTLS(dir)
	require.NoError(t, err)

	cfg, err := httputil.LoadGatewayTLS(tlsCfg.CertPath, tlsCfg.KeyPath, tlsCfg.CACertPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth, "mTLS should require client certs")
	assert.NotNil(t, cfg.ClientCAs, "mTLS should populate CA pool")
}

func TestLoadGatewayTLS_ServerServesMTLS(t *testing.T) {
	dir := t.TempDir()
	certs, err := generateMTLSCerts(t, dir)
	require.NoError(t, err)

	cfg, err := httputil.LoadGatewayTLS(certs.serverCertPath, certs.serverKeyPath, certs.caCertPath)
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      certs.caPool,
				Certificates: []tls.Certificate{certs.clientCert},
			},
		},
	}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err, "mTLS handshake should succeed with valid client cert")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestLoadGatewayTLS_InvalidCertPath(t *testing.T) {
	_, err := httputil.LoadGatewayTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to load server TLS certificate/key")
}

func TestLoadGatewayTLS_InvalidCAPath(t *testing.T) {
	dir := t.TempDir()
	tlsCfg, err := proxy.GenerateSelfSignedTLS(dir)
	require.NoError(t, err)

	_, err = httputil.LoadGatewayTLS(tlsCfg.CertPath, tlsCfg.KeyPath, "/nonexistent/ca.pem")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to read CA certificate")
}

func TestLoadGatewayTLS_MalformedCA(t *testing.T) {
	dir := t.TempDir()
	tlsCfg, err := proxy.GenerateSelfSignedTLS(dir)
	require.NoError(t, err)

	// Write garbage as CA cert
	badCA := dir + "/bad-ca.pem"
	require.NoError(t, os.WriteFile(badCA, []byte("NOT A VALID PEM"), 0644))

	_, err = httputil.LoadGatewayTLS(tlsCfg.CertPath, tlsCfg.KeyPath, badCA)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse CA certificate")
}
