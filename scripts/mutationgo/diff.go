package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// changedLines returns the numbers of the lines that differ. A mutation changes
// tokens in place, so both contents must have the same number of lines. If they
// do not, ok is false.
func changedLines(original, mutated []byte) (lines []int, ok bool) {
	before := bytes.Split(original, []byte("\n"))
	after := bytes.Split(mutated, []byte("\n"))

	if len(before) != len(after) {
		return nil, false
	}

	for i := range before {
		if !bytes.Equal(before[i], after[i]) {
			lines = append(lines, i+1)
		}
	}

	return lines, true
}

// differingSourceFiles returns the paths, relative to the module root and in
// walk order, of the Go source files in workerDir that are new or different
// from the file in sourceDir. Test files do not count.
func differingSourceFiles(sourceDir, workerDir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(workerDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := entry.Name()
		if entry.IsDir() {
			if path != workerDir && isHiddenDir(name) {
				return filepath.SkipDir
			}

			return nil
		}

		if !isSourceFile(name) {
			return nil
		}

		rel, differs, err := differsFromSource(sourceDir, workerDir, path)
		if differs {
			files = append(files, rel)
		}

		return err
	})

	return files, err
}

// differsFromSource reports whether the file at path in workerDir is new or
// different from the file in sourceDir. It returns the path relative to
// workerDir.
func differsFromSource(sourceDir, workerDir, path string) (rel string, differs bool, err error) {
	rel, err = filepath.Rel(workerDir, path)
	if err != nil {
		return "", false, err
	}

	same, err := sameContent(path, filepath.Join(sourceDir, rel))
	if err != nil {
		return "", false, err
	}

	return filepath.ToSlash(rel), !same, nil
}

func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

func isSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// sameContent reports whether both files have the same content. A missing
// second file counts as different.
func sameContent(path, other string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	otherInfo, err := os.Stat(other)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if info.Size() != otherInfo.Size() {
		return false, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	otherContent, err := os.ReadFile(other)
	if err != nil {
		return false, err
	}

	return bytes.Equal(content, otherContent), nil
}
