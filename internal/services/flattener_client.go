package services

import (
	"bytes"
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

// FlattenerClient handles communication with the fhir-flattener service.
type FlattenerClient struct {
	baseURL    string
	httpClient *HTTPClient
	logger     *lib.Logger
}

// NewFlattenerClient creates a new flattener client. It wraps the shared
// HTTPClient so retry, TLS, and error classification follow the single shared
// path. If transport is non-nil it is applied for custom TLS; timeout defaults
// to 30 minutes when unset (flattening large datasets can be slow).
func NewFlattenerClient(config models.FlatteningConfig, retryConfig models.RetryConfig, transport *http.Transport, logger *lib.Logger) *FlattenerClient {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	client := &http.Client{Timeout: timeout}
	if transport != nil {
		client.Transport = transport
	}

	return &FlattenerClient{
		baseURL: config.ServiceURL,
		httpClient: &HTTPClient{
			client:      client,
			retryConfig: lib.NewRetryConfigFromModel(retryConfig),
			logger:      logger,
		},
		logger: logger,
	}
}

// Flatten sends resources to the fhir-flattener service and returns the
// flattened rows in ViewDefinition column order. The service responds with
// NDJSON (one JSON object per row); values are mapped to columns by name, so
// a change of column order upstream cannot mislabel columns. Transient errors
// (network + HTTP 5xx) are retried by the shared HTTPClient.
func (c *FlattenerClient) Flatten(viewDef models.ViewDefinition, resources []map[string]any) ([][]string, error) {
	if len(resources) == 0 {
		c.logger.Debug("No resources to flatten", "viewDefinition", viewDef.Name)
		return nil, nil
	}

	// _format=ndjson is redundant with the Accept header today (flattener
	// 0.1.0-alpha.7 only reads Accept) but is sent for robustness.
	url := c.baseURL + "/fhir/ViewDefinition/$run?_format=ndjson"

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

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Flattener HTTP request failed", "viewDefinition", viewDef.Name, "error", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, classifyHTTPResponse("Flattener", resp)
	}

	rows, err := c.decodeNDJSONRows(resp.Body, ExtractColumnNames(viewDef), len(resources))
	if err != nil {
		return nil, err
	}

	c.logger.Debug("Flattener returned rows", "viewDefinition", viewDef.Name, "rows", len(rows))
	return rows, nil
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

// HealthCheck verifies the flattener service is available. Transient errors are
// retried by the shared HTTPClient.
func (c *FlattenerClient) HealthCheck() error {
	req, err := http.NewRequest("GET", c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("flattener health check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return classifyHTTPResponse("Flattener", resp)
	}

	return nil
}

// Flattener is the seam the flattener client satisfies so pipeline steps can be
// tested against a fake adapter.
type Flattener interface {
	Flatten(viewDef models.ViewDefinition, resources []map[string]any) ([][]string, error)
}

var _ Flattener = (*FlattenerClient)(nil)
