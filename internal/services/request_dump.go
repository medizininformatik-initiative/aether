package services

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// RequestDump writes the body of a failed send request to disk, so the payload
// the server rejected can be examined and sent again.
//
// The files hold pseudonymized patient data. Delete them after analysis.
type RequestDump struct {
	dir    string
	logger *lib.Logger
}

// NewRequestDump returns a dump that writes into dir. Write on a nil dump does
// nothing, so callers can pass one unconditionally.
func NewRequestDump(dir string, logger *lib.Logger) *RequestDump {
	return &RequestDump{dir: dir, logger: logger}
}

// Write saves body as "<name>.request.json" and the server answer as
// "<name>.response.txt". The body is written unchanged, so the file can be sent
// to the server again without a repair step.
func (d *RequestDump) Write(name string, body []byte, statusCode int, responseBody []byte) {
	if d == nil {
		return
	}

	if err := os.MkdirAll(d.dir, 0o750); err != nil {
		d.logger.Warn("Failed to create dump directory", "dir", d.dir, "error", err)
		return
	}

	requestPath := filepath.Join(d.dir, name+".request.json")
	if err := os.WriteFile(requestPath, body, 0o600); err != nil {
		d.logger.Warn("Failed to write failed request", "path", requestPath, "error", err)
		return
	}

	responsePath := filepath.Join(d.dir, name+".response.txt")
	response := fmt.Sprintf("HTTP %d\n\n%s\n", statusCode, responseBody)
	if err := os.WriteFile(responsePath, []byte(response), 0o600); err != nil {
		d.logger.Warn("Failed to write failed response", "path", responsePath, "error", err)
	}

	d.logger.Warn("Wrote failed request to disk", "path", requestPath, "status", statusCode)
}
