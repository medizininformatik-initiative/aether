package services

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// BuildTLSTransport creates an *http.Transport with custom TLS settings.
// Returns (nil, nil) when no customization is needed (empty config with defaults).
func BuildTLSTransport(tlsConfig models.TLSConfig, logger *lib.Logger) (*http.Transport, error) {
	if tlsConfig.CACertPath == "" && !tlsConfig.InsecureSkipVerify {
		return nil, nil
	}

	tlsCfg, err := buildTLSClientConfig(tlsConfig, logger)
	if err != nil {
		return nil, err
	}

	transport := cloneDefaultTransport()
	transport.TLSClientConfig = tlsCfg

	return transport, nil
}

// cloneDefaultTransport copies http.DefaultTransport so the caller keeps its
// dial timeout, TLS handshake timeout, proxy support, HTTP/2 and
// connection-pool settings. A bare *http.Transport has none of them.
func cloneDefaultTransport() *http.Transport {
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		return defaultTransport.Clone()
	}

	return &http.Transport{}
}

// buildTLSClientConfig makes the TLS configuration from the CA certificate file
// and the verification setting.
func buildTLSClientConfig(tlsConfig models.TLSConfig, logger *lib.Logger) (*tls.Config, error) {
	tlsCfg := &tls.Config{}

	if tlsConfig.InsecureSkipVerify {
		logger.Warn("TLS verification disabled — use only for development/testing")
		tlsCfg.InsecureSkipVerify = true
	}

	if tlsConfig.CACertPath != "" {
		pemData, err := os.ReadFile(tlsConfig.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file %s: %w", tlsConfig.CACertPath, err)
		}

		pool, err := x509.SystemCertPool()
		if err != nil {
			// Fall back to an empty pool if system pool is unavailable
			pool = x509.NewCertPool()
		}

		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("failed to parse any certificates from %s", tlsConfig.CACertPath)
		}

		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}
