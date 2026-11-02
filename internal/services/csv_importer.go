package services

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

// ImportCSVFiles copies all CSV files below sourceDir into destinationDir.
// The scan is recursive and keeps the relative path of each file.
// It deletes destinationDir first, so the result holds only the current files.
// It returns the number of copied files and their total size in bytes.
func ImportCSVFiles(sourceDir, destinationDir string, logger *lib.Logger) (int, int64, error) {
	isDir, err := statDir(sourceDir)
	if err != nil {
		return 0, 0, err
	}
	if !isDir {
		return 0, 0, fmt.Errorf("expected directory but got file: %s", sourceDir)
	}

	inside, err := isWithin(sourceDir, destinationDir)
	if err != nil {
		return 0, 0, err
	}
	if inside {
		return 0, 0, fmt.Errorf("source directory is inside the destination directory: %s", sourceDir)
	}
	// A repeated run must not send files that are no longer in sourceDir.
	if err := os.RemoveAll(destinationDir); err != nil {
		return 0, 0, fmt.Errorf("failed to clear destination directory %s: %w", destinationDir, err)
	}

	files, totalBytes, err := copyCSVTree(sourceDir, destinationDir, logger)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to import CSV files: %w", err)
	}
	if files == 0 {
		return 0, 0, fmt.Errorf("no CSV files found in directory: %s\n\nExpected files with extension: .csv", sourceDir)
	}
	return files, totalBytes, nil
}

// isWithin reports whether path is dir or lies below dir, after symlinks are resolved.
func isWithin(path, dir string) (bool, error) {
	resolvedPath, err := resolvePath(path)
	if err != nil {
		return false, err
	}
	resolvedDir, err := resolvePath(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(resolvedDir, resolvedPath)
	return err == nil && filepath.IsLocal(rel), nil
}

// resolvePath returns the absolute path with symlinks resolved. A path that does
// not exist stays unresolved.
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %s: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %s: %w", path, err)
	}
	return resolved, nil
}

// copyCSVTree walks sourceDir and copies every CSV file.
func copyCSVTree(sourceDir, destinationDir string, logger *lib.Logger) (int, int64, error) {
	var files int
	var totalBytes int64
	err := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !isCSVFile(entry) {
			return nil
		}
		size, err := copyCSVFile(sourceDir, path, destinationDir, logger)
		if err != nil {
			return err
		}
		files++
		totalBytes += size
		return nil
	})
	return files, totalBytes, err
}

func isCSVFile(entry fs.DirEntry) bool {
	return !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".csv")
}

// copyCSVFile copies one CSV file and keeps its path relative to sourceDir.
func copyCSVFile(sourceDir, path, destinationDir string, logger *lib.Logger) (int64, error) {
	rel, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve relative path of %s: %w", path, err)
	}
	size, err := copyPlainFile(path, filepath.Join(destinationDir, rel))
	if err != nil {
		return 0, fmt.Errorf("failed to copy %s: %w", rel, err)
	}
	logger.Debug("CSV file imported", "file", rel, "size", size)
	return size, nil
}

func copyPlainFile(sourcePath, destPath string) (int64, error) {
	src, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return 0, err
	}
	dst, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(dst, src)
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	return written, err
}
