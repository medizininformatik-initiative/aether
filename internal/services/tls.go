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

	return &http.Transport{TLSClientConfig: tlsCfg}, nil
}
