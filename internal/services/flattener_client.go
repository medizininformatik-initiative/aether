package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// ErrFlattenerStopped means that the flattener stopped answering while a
// request waited for it.
var ErrFlattenerStopped = errors.New("flattener stopped answering")

// FlattenerClient handles communication with the fhir-flattener service.
type FlattenerClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
	liveness   livenessConfig
}

// livenessConfig sets how aether checks that the flattener answers while a
// request waits.
type livenessConfig struct {
	// interval is the time between two probes.
	interval time.Duration
	// timeout is the time limit for one probe. A JVM that is short of memory
	// stops answering, so a probe must fail fast.
	timeout time.Duration
	// maxFailures is the number of probes that fail in sequence before the
	// request is cancelled.
	maxFailures int
}

var defaultLiveness = livenessConfig{
	interval:    30 * time.Second,
	timeout:     10 * time.Second,
	maxFailures: 3,
}

const (
	// metadataPath is the route that the Netty event loop of the flattener
	// serves while Spark runs. The flattener has no /health route.
	metadataPath = "/fhir/metadata"

	// runOperation is the operation on viewDefinitionType that Flatten calls.
	// A service that does not declare it is not a flattener.
	runOperation       = "$run"
	viewDefinitionType = "ViewDefinition"

	// maxMetadataBytes limits how much of the metadata body HealthCheck reads.
	maxMetadataBytes = 1 << 20
)

// NewFlattenerClient creates a new flattener client. It wraps the shared
// HTTPClient so retry, TLS, and error classification follow the single shared
// path. If transport is non-nil it is applied for custom TLS. The request
// timeout is config.Timeout, which FlatteningConfig.Validate requires to be
// positive. A positive config.MaxAttempts replaces retryConfig.MaxAttempts.
func NewFlattenerClient(config models.FlatteningConfig, retryConfig models.RetryConfig, transport *http.Transport, logger *lib.Logger) *FlattenerClient {
	if config.MaxAttempts > 0 {
		retryConfig.MaxAttempts = config.MaxAttempts
	}

	client := &http.Client{Timeout: config.Timeout}
	if transport != nil {
		client.Transport = transport
	}

	return &FlattenerClient{
		baseURL: config.ServiceURL,
		httpClient: &HTTPClient{
			client:         client,
			retryConfig:    lib.NewRetryConfigFromModel(retryConfig),
			logger:         logger,
			noTimeoutRetry: true,
		},
		logger:   logger,
		liveness: defaultLiveness,
	}
}

// Flatten sends resources to the fhir-flattener service and returns the
// flattened rows in ViewDefinition column order. The service responds with
// NDJSON (one JSON object per row); values are mapped to columns by name, so
// a change of column order upstream cannot mislabel columns. Transient errors
// (network + HTTP 5xx) are retried by the shared HTTPClient. A request that
// exceeds the timeout is not retried.
func (c *FlattenerClient) Flatten(viewDef models.ViewDefinition, resources []map[string]any) ([][]string, error) {
	if len(resources) == 0 {
		c.logger.Debug("No resources to flatten", "viewDefinition", viewDef.Name)
		return nil, nil
	}

	// _format=ndjson is redundant with the Accept header today (flattener
	// 0.1.0-alpha.7 only reads Accept) but is sent for robustness.
	url := c.baseURL + "/fhir/" + viewDefinitionType + "/" + runOperation + "?_format=ndjson"

	c.logger.Debug("Sending resources to flattener",
		"viewDefinition", viewDef.Name,
		"resourceCount", len(resources),
		"resourceType", viewDef.Resource,
		"url", url)

	request := models.NewFlatteningRequest(viewDef, resources)
	jsonBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "application/x-ndjson")

	// The response body is streamed, so the watchdog must run until the
	// decode ends.
	stopWatchdog := c.startWatchdog(ctx, cancel, viewDef.Name)
	defer stopWatchdog()

	resp, err := c.send(ctx, req, viewDef.Name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, classifyHTTPResponse("Flattener", resp)
	}

	rows, err := c.decodeNDJSONRows(resp.Body, ExtractColumnNames(viewDef), len(resources))
	if err != nil {
		return nil, c.failure(ctx, err)
	}

	c.logger.Debug("Flattener returned rows", "viewDefinition", viewDef.Name, "rows", len(rows))
	return rows, nil
}

// send sends the flatten request. It logs a failure only when the watchdog did
// not cancel the request, because the watchdog logs its own cause.
func (c *FlattenerClient) send(ctx context.Context, req *http.Request, name string) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err == nil {
		return resp, nil
	}
	if stoppedCause(ctx) == nil {
		c.logger.Error("Flattener HTTP request failed", "viewDefinition", name, "error", err)
	}
	return nil, c.failure(ctx, fmt.Errorf("request failed: %w", err))
}

// failure returns the watchdog cause when the watchdog cancelled the request.
// Otherwise it returns err with the timeout hint.
func (c *FlattenerClient) failure(ctx context.Context, err error) error {
	if cause := stoppedCause(ctx); cause != nil {
		return cause
	}
	return c.timeoutHint(err)
}

// stoppedCause returns the reason that the watchdog cancelled the request. It
// returns nil when the watchdog did not cancel the request.
func stoppedCause(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrFlattenerStopped) {
		return cause
	}
	return nil
}

// startWatchdog probes the metadata route at each liveness interval while a
// request waits, and cancels the request when the probes fail in sequence. The
// returned function stops the watchdog, aborts a probe in flight, and waits
// until the watchdog has exited.
func (c *FlattenerClient) startWatchdog(ctx context.Context, cancel context.CancelCauseFunc, name string) func() {
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.watch(watchCtx, cancel, name)
	}()
	return func() {
		stop()
		<-done
	}
}

func (c *FlattenerClient) watch(ctx context.Context, cancel context.CancelCauseFunc, name string) {
	url := c.baseURL + metadataPath
	probeClient := c.httpClient.withTimeout(c.liveness.timeout)
	ticker := time.NewTicker(c.liveness.interval)
	defer ticker.Stop()
	start := time.Now()
	failures := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		err := probe(ctx, probeClient, url)
		if ctx.Err() != nil {
			// The request ended while the probe ran. The abort is no failure.
			return
		}
		if err == nil {
			failures = 0
			c.logger.Info("Flattener request still in progress",
				"viewDefinition", name,
				"elapsed", time.Since(start).Round(time.Second))
			continue
		}

		failures++
		c.logger.Warn("Flattener health check failed while a request waits",
			"viewDefinition", name, "failures", failures, "error", err)
		if failures >= c.liveness.maxFailures {
			cancel(fmt.Errorf("%w: %d health checks at %s failed in sequence: %w",
				ErrFlattenerStopped, failures, url, err))
			return
		}
	}
}

// probe sends one request without retry. Only an HTTP 2xx status is a pass.
func probe(ctx context.Context, client *HTTPClient, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := client.DoOnce(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return checkMetadataResponse(resp)
}

// checkMetadataResponse applies the one status rule for the metadata route:
// only an HTTP 2xx status is a pass.
func checkMetadataResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	return classifyHTTPResponse("Flattener", resp)
}

// timeoutHint names the configuration keys that prevent a timeout. It
// returns other errors unchanged.
func (c *FlattenerClient) timeoutHint(err error) error {
	if !isClientTimeout(err) {
		return err
	}
	return fmt.Errorf("flattener request timed out after %s: decrease services.flattening.batch_size_mb or increase services.flattening.timeout: %w",
		c.httpClient.Timeout(), err)
}

// decodeNDJSONRows streams the NDJSON response body and maps each object to a
// CSV row by column name. A column absent from an object becomes an empty
// cell, because FHIR elements are optional. The caller counts the columns
// that stay empty for the full job. sizeHint pre-sizes the row slice (the
// resource count is a good lower bound).
func (c *FlattenerClient) decodeNDJSONRows(body io.Reader, columns []string, sizeHint int) ([][]string, error) {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()

	rows := make([][]string, 0, sizeHint)
	obj := make(map[string]any)
	for {
		clear(obj)
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to parse NDJSON from flattener (row %d): %w", len(rows)+1, err)
		}

		row := make([]string, len(columns))
		for i, col := range columns {
			value, ok := obj[col]
			if !ok {
				continue
			}
			row[i] = renderCSVCell(value)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// renderCSVCell renders a decoded JSON value as a CSV cell:
// null becomes an empty cell, a string stays verbatim, a number keeps its
// source text (json.Number), a boolean becomes "true"/"false", and a nested
// object or array becomes compact JSON.
func renderCSVCell(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			// Unreachable for values that encoding/json itself decoded.
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}

// HealthCheck verifies that the flattener answers on its metadata route with a
// CapabilityStatement that declares the $run operation. Each request has the
// short liveness timeout, and the shared HTTPClient retries transient errors.
// A service that answers with another body is not a flattener, and the check
// does not retry.
func (c *FlattenerClient) HealthCheck() error {
	url := c.baseURL + metadataPath
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.withTimeout(c.liveness.timeout).Do(req)
	if err != nil {
		return fmt.Errorf("flattener does not answer at %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkMetadataResponse(resp); err != nil {
		return err
	}

	var statement capabilityStatement
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&statement); err != nil {
		return fmt.Errorf("service at %s is not a flattener: %s returned no CapabilityStatement: %w", c.baseURL, metadataPath, err)
	}
	if statement.ResourceType != "CapabilityStatement" {
		return fmt.Errorf("service at %s is not a flattener: %s returned no CapabilityStatement", c.baseURL, metadataPath)
	}
	if !statement.declaresOperation(viewDefinitionType, runOperation) {
		return fmt.Errorf("service at %s is not a flattener: its CapabilityStatement declares no %s operation for %s",
			c.baseURL, runOperation, viewDefinitionType)
	}
	return nil
}

// Flattener is the seam the flattener client satisfies so pipeline steps can be
// tested against a fake adapter. HealthCheck reports whether the service
// answers. Flatten fails with ErrFlattenerStopped when the service stops
// answering while a request waits.
type Flattener interface {
	HealthCheck() error
	Flatten(viewDef models.ViewDefinition, resources []map[string]any) ([][]string, error)
}

var _ Flattener = (*FlattenerClient)(nil)
