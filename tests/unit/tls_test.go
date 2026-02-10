package unit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// generateTestCACert creates a self-signed CA certificate PEM for testing
func generateTestCACert(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// generateTestServerCert creates a self-signed server certificate (not a CA) for testing
func generateTestServerCert(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Server"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: false,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestBuildTLSTransport_EmptyConfig(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	transport, err := services.BuildTLSTransport(models.TLSConfig{}, logger)

	assert.NoError(t, err)
	assert.Nil(t, transport, "Empty config should return nil transport (use Go defaults)")
}

func TestBuildTLSTransport_InsecureSkipVerify(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		InsecureSkipVerify: true,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, transport, "Should return a transport when InsecureSkipVerify is set")
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func TestBuildTLSTransport_ValidCACert(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	certPEM := generateTestCACert(t)
	certFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0644))

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		CACertPath: certFile,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, transport, "Should return a transport when CA cert is provided")
	assert.NotNil(t, transport.TLSClientConfig.RootCAs, "RootCAs should be set")
}

func TestBuildTLSTransport_ValidServerCert(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	// Self-signed server cert (not a CA) — AppendCertsFromPEM accepts these too
	certPEM := generateTestServerCert(t)
	certFile := filepath.Join(t.TempDir(), "server.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0644))

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		CACertPath: certFile,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.TLSClientConfig.RootCAs)
}

func TestBuildTLSTransport_InvalidCACertPath(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		CACertPath: "/nonexistent/path/ca.pem",
	}, logger)

	assert.Error(t, err)
	assert.Nil(t, transport)
	assert.Contains(t, err.Error(), "failed to read CA certificate file")
}

func TestBuildTLSTransport_InvalidPEMContent(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	badFile := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(badFile, []byte("this is not a PEM file"), 0644))

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		CACertPath: badFile,
	}, logger)

	assert.Error(t, err)
	assert.Nil(t, transport)
	assert.Contains(t, err.Error(), "failed to parse any certificates")
}

func TestBuildTLSTransport_BothCACertAndInsecureSkip(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelInfo)

	certPEM := generateTestCACert(t)
	certFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0644))

	transport, err := services.BuildTLSTransport(models.TLSConfig{
		CACertPath:         certFile,
		InsecureSkipVerify: true,
	}, logger)

	require.NoError(t, err)
	require.NotNil(t, transport)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	assert.NotNil(t, transport.TLSClientConfig.RootCAs, "RootCAs should be set even with InsecureSkipVerify")
}
