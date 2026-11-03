package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/send"
)

// sendStep sends pipeline output to the configured destination. It finds the
// input files and hands them to the send package.
type sendStep struct{}

func (sendStep) Name() models.StepName { return models.StepSend }

func (sendStep) Run(ctx *StepContext) (StepResult, error) {
	job := ctx.Job
	layout := ctx.Layout

	if err := job.Config.Services.Send.Validate(); err != nil {
		return StepResult{}, err
	}

	inputDir := layout.InputDir(models.StepSend)

	files, err := discoverSendFiles(inputDir, job.Config.Services.Send.SendAs)
	if err != nil {
		return StepResult{}, err
	}

	result, err := send.Send(context.Background(), send.Request{
		JobID:  job.JobID,
		Config: job.Config.Services.Send,
		Retry:  job.Config.Retry,
		TLS:    job.Config.TLS,
		Files:  files,
		State: send.State{
			ManifestPath:      models.GetUploadManifestPath(layout.JobDir()),
			FailedRequestsDir: layout.FailedRequestsDir(),
		},
	}, ctx.Logger)
	if err != nil {
		return StepResult{}, err
	}

	return StepResult{FilesProcessed: result.FilesProcessed}, nil
}

// discoverSendFiles lists the files to send for the mode. The direct mode takes
// the NDJSON files of the directory; the other modes take every file below it.
func discoverSendFiles(inputDir string, mode models.SendMode) ([]send.File, error) {
	paths, err := listSendPaths(inputDir, mode)
	if err != nil {
		return nil, err
	}

	files := make([]send.File, len(paths))
	for i, path := range paths {
		files[i] = send.File{Path: path, Name: relativeName(inputDir, path)}
	}

	return files, nil
}

// relativeName gives the path relative to the directory, or the base name when
// no relative path exists.
func relativeName(dir, path string) string {
	name, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.Base(path)
	}
	return name
}

// listSendPaths lists the paths to send for the mode.
func listSendPaths(inputDir string, mode models.SendMode) ([]string, error) {
	switch mode {
	case models.SendModeDirectResourceLoad:
		paths, err := findNDJSONFiles(inputDir)
		if err != nil {
			return nil, fmt.Errorf("failed to list NDJSON files in %s: %w", inputDir, err)
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("no NDJSON files found in %s", inputDir)
		}
		return paths, nil
	case models.SendModeTransferLoad, models.SendModeS3Upload:
		paths, err := findAllFilesRecursive(inputDir)
		if err != nil {
			return nil, fmt.Errorf("failed to list input files: %w", err)
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("no files found in %s", inputDir)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("unknown send mode: %s", mode)
	}
}

// findNDJSONFiles finds all NDJSON files in a directory (both .ndjson and .ndjson.zst)
func findNDJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".ndjson") || strings.HasSuffix(name, ".ndjson.zst") {
			files = append(files, filepath.Join(dir, name))
		}
	}

	return files, nil
}

// findAllFilesRecursive walks a directory tree and returns all file paths.
func findAllFilesRecursive(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}
