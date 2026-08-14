package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// HTTPClient wraps the standard http.Client with retry logic and configuration
type HTTPClient struct {
	client      *http.Client
	retryConfig lib.RetryConfig
	logger      *lib.Logger
}

// NewHTTPClient creates an HTTP client with timeout, retry, and TLS configuration.
// If tlsConfig specifies a custom CA or InsecureSkipVerify, a custom transport is built.
func NewHTTPClient(timeout time.Duration, retryConfig models.RetryConfig, tlsConfig models.TLSConfig, logger *lib.Logger) *HTTPClient {
	client := &http.Client{
		Timeout: timeout,
	}

	transport, err := BuildTLSTransport(tlsConfig, logger)
	if err != nil {
		logger.Warn("Failed to build TLS transport, using defaults", "error", err)
	} else if transport != nil {
		client.Transport = transport
	}

	return &HTTPClient{
		client:      client,
		retryConfig: lib.NewRetryConfigFromModel(retryConfig),
		logger:      logger,
	}
}

// newDownloadClient returns an *http.Client tuned for streaming large response
// bodies. It drops the whole-request deadline (Timeout: 0) so a big but
// steadily-progressing download is never cut off mid-body, reuses this client's
// TLS settings by cloning its transport, and bounds the header phase with
// ResponseHeaderTimeout. Body-phase stall detection is the caller's job, since
// no transport setting bounds an in-flight body read.
func (c *HTTPClient) newDownloadClient(stallTimeout time.Duration) *http.Client {
	var transport *http.Transport
	if t, ok := c.client.Transport.(*http.Transport); ok && t != nil {
		transport = t.Clone()
	} else if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = dt.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.ResponseHeaderTimeout = stallTimeout

	// With Timeout: 0 and the body stall watchdog armed only after headers
	// arrive, nothing else bounds the connect and TLS-handshake phases. A custom
	// TLS transport (BuildTLSTransport) ships without a dialer, so a black-holed
	// connect would hang forever; bound both phases when unset.
	if transport.DialContext == nil {
		transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	if transport.TLSHandshakeTimeout == 0 {
		transport.TLSHandshakeTimeout = 10 * time.Second
	}

	return &http.Client{Timeout: 0, Transport: transport}
}

// DefaultHTTPClient creates an HTTP client with sensible defaults (no custom TLS).
func DefaultHTTPClient() *HTTPClient {
	return NewHTTPClient(
		30*time.Second,
		models.RetryConfig{
			MaxAttempts:      5,
			InitialBackoffMs: 1000,
			MaxBackoffMs:     30000,
		},
		models.TLSConfig{},
		lib.DefaultLogger,
	)
}

// Get performs an HTTP GET request with retry logic
func (c *HTTPClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.Do(req)
}

// Post performs an HTTP POST request with retry logic
func (c *HTTPClient) Post(url string, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)

	return c.Do(req)
}

// PostJSON performs an HTTP POST request with JSON content type
func (c *HTTPClient) PostJSON(url string, jsonBody []byte) (*http.Response, error) {
	return c.Post(url, "application/json", jsonBody)
}

// Put performs an HTTP PUT request with retry logic
func (c *HTTPClient) Put(url string, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)

	return c.Do(req)
}

// Do executes an HTTP request with retry logic for transient errors
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var lastErr error

	// Retry logic
	for attempt := 0; attempt < c.retryConfig.MaxAttempts; attempt++ {
		// Clone request body if needed (body can only be read once)
		var bodyBytes []byte
		if req.Body != nil {
			bodyBytes, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Execute request
		startTime := time.Now()
		resp, lastErr = c.client.Do(req)
		duration := time.Since(startTime)

		// Log the request
		lib.LogServiceCall(c.logger, req.URL.Host, req.URL.Path, req.Method)

		// Success
		if lastErr == nil {
			// Log response
			lib.LogServiceResponse(c.logger, req.URL.Host, resp.StatusCode, duration)

			// Check if HTTP status indicates error
			if resp.StatusCode >= 400 {
				errorType := lib.ClassifyHTTPError(resp.StatusCode)

				// Return the response (so the caller can read and classify the
				// error body) for non-transient statuses, and for transient
				// statuses once no retry attempt remains. Only retry a transient
				// status when a further attempt will actually run.
				if errorType == models.ErrorTypeNonTransient || attempt >= c.retryConfig.MaxAttempts-1 {
					return resp, nil
				}

				statusErr := fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
				lib.LogRetry(c.logger, req.URL.String(), attempt, c.retryConfig.MaxAttempts, statusErr)
				lastErr = statusErr

				// Close response body before retry
				_ = resp.Body.Close()

				backoff := lib.CalculateBackoff(attempt, c.retryConfig.InitialBackoffMs, c.retryConfig.MaxBackoffMs)
				time.Sleep(backoff)

				// Reset request body for retry
				if bodyBytes != nil {
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}

				continue
			}

			return resp, nil
		}

		// Network error occurred
		// Check if it's a retryable network error
		if lib.IsNetworkError(lastErr) {
			errorType := models.ErrorTypeTransient
			if lib.ShouldRetry(errorType, attempt, c.retryConfig.MaxAttempts) {
				lib.LogRetry(c.logger, req.URL.String(), attempt, c.retryConfig.MaxAttempts, lastErr)

				// Wait before retry
				if attempt < c.retryConfig.MaxAttempts-1 {
					backoff := lib.CalculateBackoff(attempt, c.retryConfig.InitialBackoffMs, c.retryConfig.MaxBackoffMs)
					time.Sleep(backoff)
				}

				// Reset request body for retry
				if bodyBytes != nil {
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}

				continue
			}
		}

		// Non-retryable error
		return nil, lastErr
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", c.retryConfig.MaxAttempts, lastErr)
}

// DoOnce executes a request exactly once with no retry. It is for callers that
// own their own outer polling/availability loop (e.g. TORCH status polling),
// where wrapping each attempt in the shared retry loop would be wrong.
func (c *HTTPClient) DoOnce(req *http.Request) (*http.Response, error) {
	startTime := time.Now()
	resp, err := c.client.Do(req)
	lib.LogServiceCall(c.logger, req.URL.Host, req.URL.Path, req.Method)
	if err == nil {
		lib.LogServiceResponse(c.logger, req.URL.Host, resp.StatusCode, time.Since(startTime))
	}
	return resp, err
}

// ApplyAuth sets the authentication headers from the given auth config.
// Basic auth or OAuth2 client-credentials set Authorization; basic auth takes
// precedence. The API key sets its own header and can accompany either scheme.
// No header is set when auth is unconfigured.
func (c *HTTPClient) ApplyAuth(req *http.Request, auth models.AuthConfig) error {
	if err := c.applyAuthorizationHeader(req, auth); err != nil {
		return err
	}

	if auth.APIKey != "" {
		req.Header.Set(auth.APIKeyHeaderName(), auth.APIKey)
	}

	return nil
}

func (c *HTTPClient) applyAuthorizationHeader(req *http.Request, auth models.AuthConfig) error {
	if auth.Username != "" && auth.Password != "" {
		credentials := auth.Username + ":" + auth.Password
		encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
		req.Header.Set("Authorization", "Basic "+encoded)
		return nil
	}

	if auth.OAuthIssuerURI != "" && auth.OAuthClientID != "" && auth.OAuthClientSecret != "" {
		token, err := FetchOAuth2Token(auth.OAuthIssuerURI, auth.OAuthClientID, auth.OAuthClientSecret, c)
		if err != nil {
			return fmt.Errorf("failed to get OAuth2 token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

// FHIRRequest describes a single FHIR-JSON request end to end.
type FHIRRequest struct {
	Method      string
	URL         string
	ContentType string // defaults to application/fhir+json when empty
	Body        any    // marshaled to JSON when non-nil
	Auth        models.AuthConfig
	Service     string // labels classified errors (e.g. "DIMP", "validation")
}

// DoFHIRJSON executes a FHIR-JSON request end to end: marshal the body, apply
// auth, run the shared retry loop, classify any HTTP-status error into a
// *ServiceError, and JSON-decode a success response into out (skipped when
// out is nil). This is the deep entry point DIMP and validation share.
func (c *HTTPClient) DoFHIRJSON(fhirReq FHIRRequest, out any) error {
	var bodyReader io.Reader
	if fhirReq.Body != nil {
		jsonBody, err := json.Marshal(fhirReq.Body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(fhirReq.Method, fhirReq.URL, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	contentType := fhirReq.ContentType
	if contentType == "" {
		contentType = "application/fhir+json"
	}
	req.Header.Set("Content-Type", contentType)

	if err := c.ApplyAuth(req, fhirReq.Auth); err != nil {
		return fmt.Errorf("failed to add auth header: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return classifyHTTPResponse(fhirReq.Service, resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// Download downloads a file from a URL and writes it to a writer
// Returns the number of bytes downloaded
func (c *HTTPClient) Download(url string, writer io.Writer) (int64, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Copy response body to writer
	bytesWritten, err := io.Copy(writer, resp.Body)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to download: %w", err)
	}

	return bytesWritten, nil
}

// DownloadWithProgress downloads a file with progress callback
// The callback is called periodically with bytes downloaded so far
func (c *HTTPClient) DownloadWithProgress(url string, writer io.Writer, progressCallback func(int64)) (int64, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create a progress reader that calls the callback
	reader := &ProgressReader{
		Reader:   resp.Body,
		Callback: progressCallback,
	}

	// Copy response body to writer
	bytesWritten, err := io.Copy(writer, reader)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to download: %w", err)
	}

	return bytesWritten, nil
}

// ProgressReader wraps an io.Reader and calls a callback with bytes read
type ProgressReader struct {
	Reader   io.Reader
	Callback func(int64)
	total    int64
}

func (r *ProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.total += int64(n)
	if r.Callback != nil && n > 0 {
		r.Callback(r.total)
	}
	return n, err
}
