package unit

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// writeTLSServerCertPEM writes the TLS server's self-signed cert to a PEM file
// so it can be passed to NewAWSS3Uploader as a trusted CA.
func writeTLSServerCertPEM(t *testing.T, server *httptest.Server) string {
	t.Helper()
	cert := server.Certificate()
	require.NotNil(t, cert, "httptest server must expose a certificate")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pemBytes, 0600))
	return path
}

func TestS3Error_Classification_Transient(t *testing.T) {
	transientMessages := []string{
		"SlowDown: Rate exceeded",
		"ServiceUnavailable: Service is temporarily unavailable",
		"InternalError: We encountered an internal error",
		"RequestTimeout: Your socket connection was not read from",
		"connection reset by peer",
		"connection refused",
		"timeout awaiting response headers",
		"TooManyRequests: rate limit exceeded",
	}

	for _, msg := range transientMessages {
		t.Run(msg, func(t *testing.T) {
			err := services.ClassifyS3ErrorForTesting(fmt.Errorf("%s", msg))
			require.NotNil(t, err)
			assert.Equal(t, models.ErrorTypeTransient, err.ErrorType,
				"expected transient for: %s", msg)
			assert.True(t, err.IsRetryable())
		})
	}
}

func TestS3Error_Classification_NonTransient(t *testing.T) {
	nonTransientMessages := []string{
		"NoSuchBucket: The specified bucket does not exist",
		"AccessDenied: Access Denied",
		"InvalidBucketName: The specified bucket is not valid",
		"NoSuchKey: The specified key does not exist",
		"MalformedXML: The XML you provided was not well-formed",
	}

	for _, msg := range nonTransientMessages {
		t.Run(msg, func(t *testing.T) {
			err := services.ClassifyS3ErrorForTesting(fmt.Errorf("%s", msg))
			require.NotNil(t, err)
			assert.Equal(t, models.ErrorTypeNonTransient, err.ErrorType,
				"expected non-transient for: %s", msg)
			assert.False(t, err.IsRetryable())
		})
	}
}

func TestS3Error_ErrorString(t *testing.T) {
	err := &services.S3Error{
		Message:   "NoSuchBucket: bucket does not exist",
		ErrorType: models.ErrorTypeNonTransient,
	}
	assert.Equal(t, "S3 error: NoSuchBucket: bucket does not exist", err.Error())
}

func TestMockS3Uploader_Interface(t *testing.T) {
	// Verify MockS3Uploader satisfies the S3Uploader interface
	var _ services.S3Uploader = &services.MockS3Uploader{}

	mock := &services.MockS3Uploader{
		Bucket: "test-bucket",
	}

	assert.Equal(t, "test-bucket", mock.GetBucket())

	// Upload succeeds
	etag, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.NoError(t, err)
	assert.NotEmpty(t, etag)
	assert.Equal(t, []string{"job/file.ndjson"}, mock.UploadedKeys)

	// Upload again
	etag2, err := mock.UploadFile(context.Background(), "/path/to/file2", "job/file2.ndjson")
	require.NoError(t, err)
	assert.NotEqual(t, etag, etag2, "ETags should differ")
	assert.Len(t, mock.UploadedKeys, 2)
}

func TestMockS3Uploader_Error(t *testing.T) {
	mock := &services.MockS3Uploader{
		Bucket:    "test-bucket",
		UploadErr: fmt.Errorf("simulated upload failure"),
	}

	_, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated upload failure")
	assert.Empty(t, mock.UploadedKeys, "no keys should be recorded on error")
}

func TestMockS3Uploader_CustomETag(t *testing.T) {
	mock := &services.MockS3Uploader{
		Bucket: "test-bucket",
		ETag:   "\"custom-etag\"",
	}

	etag, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.NoError(t, err)
	assert.Equal(t, "\"custom-etag\"", etag)
}

func TestNewAWSS3Uploader_BasicConstruction(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Region:          "eu-central-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)
	require.NotNil(t, uploader)
	assert.Equal(t, "test-bucket", uploader.GetBucket())
}

func TestNewAWSS3Uploader_WithEndpointAndPathStyle(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        "http://localhost:9000",
		Region:          "us-east-1",
		Bucket:          "my-bucket",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UsePathStyle:    true,
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)
	require.NotNil(t, uploader)
	assert.Equal(t, "my-bucket", uploader.GetBucket())
}

func TestNewAWSS3Uploader_WithProxyAuth(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        "http://localhost:9000",
		Region:          "us-east-1",
		Bucket:          "proxy-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}
	auth := models.AuthConfig{
		Username: "proxy-user",
		Password: "proxy-pass",
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, auth, models.TLSConfig{}, logger)
	require.NoError(t, err)
	require.NotNil(t, uploader)
	assert.Equal(t, "proxy-bucket", uploader.GetBucket())
}

func TestAWSS3Uploader_UploadFile_Success(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)

	// Create a temp file to upload
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte(`{"resourceType":"Patient","id":"1"}`+"\n"), 0644))

	etag, err := uploader.UploadFile(context.Background(), testFile, "job-123/test.ndjson")
	require.NoError(t, err)
	assert.Equal(t, "PUT", receivedMethod)
	assert.Contains(t, etag, "abc123")
}

func TestAWSS3Uploader_UploadFile_WithProxyAuth(t *testing.T) {
	var receivedProxyAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedProxyAuth = r.Header.Get("Proxy-Authorization")
		w.Header().Set("ETag", `"def456"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}
	auth := models.AuthConfig{
		Username: "proxy-user",
		Password: "proxy-pass",
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, auth, models.TLSConfig{}, logger)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte("data\n"), 0644))

	etag, err := uploader.UploadFile(context.Background(), testFile, "job/test.ndjson")
	require.NoError(t, err)
	assert.NotEmpty(t, etag)
	// Verify proxy auth header was injected by proxyAuthTransport
	assert.Contains(t, receivedProxyAuth, "Basic ")
}

func TestAWSS3Uploader_UploadFile_FileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)

	_, err = uploader.UploadFile(context.Background(), "/nonexistent/file.ndjson", "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestAWSS3Uploader_UploadFile_S3Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return an S3 error response
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte("data\n"), 0644))

	_, err = uploader.UploadFile(context.Background(), testFile, "job/test.ndjson")
	require.Error(t, err)
	// Error should be wrapped as S3Error
	var s3Err *services.S3Error
	assert.ErrorAs(t, err, &s3Err)
}

func TestAWSS3Uploader_UploadFile_NilETag(t *testing.T) {
	// Server returns 200 but no ETag header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	s3Config := models.S3Config{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}

	uploader, err := services.NewAWSS3Uploader(s3Config, models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.ndjson")
	require.NoError(t, os.WriteFile(testFile, []byte("data\n"), 0644))

	etag, err := uploader.UploadFile(context.Background(), testFile, "job/test.ndjson")
	require.NoError(t, err)
	// ETag should be empty when server doesn't return one
	assert.Empty(t, etag)
}

// Self-signed TLS tests — Issue #238.
//
// httptest.NewTLSServer generates a one-off self-signed cert. An S3 client with
// default TLS config cannot verify it, mirroring the production failure mode
// users hit with private CAs. Three paths must work: rejection, CA trust, and
// opt-out via InsecureSkipVerify.

func tlsS3Config(endpoint string) models.S3Config {
	return models.S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
	}
}

func writeS3TestPayload(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.ndjson")
	require.NoError(t, os.WriteFile(path, []byte("data\n"), 0600))
	return path
}

func TestAWSS3Uploader_SelfSignedCert_UnknownAuthorityRejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"should-not-reach"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	uploader, err := services.NewAWSS3Uploader(tlsS3Config(server.URL), models.AuthConfig{}, models.TLSConfig{}, logger)
	require.NoError(t, err)

	_, err = uploader.UploadFile(context.Background(), writeS3TestPayload(t), "job/test.ndjson")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate signed by unknown authority")
}

func TestAWSS3Uploader_SelfSignedCert_TrustedViaCACertPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"tls-ok"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	tlsConfig := models.TLSConfig{CACertPath: writeTLSServerCertPEM(t, server)}

	uploader, err := services.NewAWSS3Uploader(tlsS3Config(server.URL), models.AuthConfig{}, tlsConfig, logger)
	require.NoError(t, err)

	etag, err := uploader.UploadFile(context.Background(), writeS3TestPayload(t), "job/test.ndjson")
	require.NoError(t, err)
	assert.Contains(t, etag, "tls-ok")
}

func TestAWSS3Uploader_SelfSignedCert_InsecureSkipVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"skipped"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	tlsConfig := models.TLSConfig{InsecureSkipVerify: true}

	uploader, err := services.NewAWSS3Uploader(tlsS3Config(server.URL), models.AuthConfig{}, tlsConfig, logger)
	require.NoError(t, err)

	etag, err := uploader.UploadFile(context.Background(), writeS3TestPayload(t), "job/test.ndjson")
	require.NoError(t, err)
	assert.Contains(t, etag, "skipped")
}

func TestAWSS3Uploader_SelfSignedCert_ProxyAuthLayeredOnTLS(t *testing.T) {
	// Proxy auth must wrap (not replace) the TLS transport.
	var gotProxyAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProxyAuth = r.Header.Get("Proxy-Authorization")
		w.Header().Set("ETag", `"proxy-tls"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := lib.NewLogger(lib.LogLevelDebug)
	tlsConfig := models.TLSConfig{CACertPath: writeTLSServerCertPEM(t, server)}
	auth := models.AuthConfig{Username: "u", Password: "p"}

	uploader, err := services.NewAWSS3Uploader(tlsS3Config(server.URL), auth, tlsConfig, logger)
	require.NoError(t, err)

	_, err = uploader.UploadFile(context.Background(), writeS3TestPayload(t), "job/test.ndjson")
	require.NoError(t, err)
	assert.Contains(t, gotProxyAuth, "Basic ")
}

func TestAWSS3Uploader_CACertPath_NotFound(t *testing.T) {
	logger := lib.NewLogger(lib.LogLevelDebug)
	tlsConfig := models.TLSConfig{CACertPath: "/nonexistent/path/to/ca.pem"}

	_, err := services.NewAWSS3Uploader(tlsS3Config("http://localhost:1"), models.AuthConfig{}, tlsConfig, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build TLS transport")
}
