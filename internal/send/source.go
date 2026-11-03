package send

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FindCSVFiles lists the CSV files below dir, sorted by relative name. A file
// is a CSV file when its name ends in ".csv", in any letter case. Other files
// are not part of the result. FindCSVFiles follows symbolic links to files and
// to directories. A link that points to one of its own parent directories is
// an error, and so is a link that points to nothing.
func FindCSVFiles(dir string) ([]File, error) {
	root, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}

	w := csvWalker{ancestors: map[string]bool{}}
	if err := w.walk(root, root, ""); err != nil {
		return nil, fmt.Errorf("list CSV files in %s: %w", dir, err)
	}
	if len(w.files) == 0 {
		return nil, fmt.Errorf("no CSV files found in %s", dir)
	}

	sort.Slice(w.files, func(i, j int) bool { return w.files[i].Name < w.files[j].Name })
	return w.files, nil
}

// resolveDir returns the absolute path of dir without symbolic links and
// checks that it is a directory.
func resolveDir(dir string) (string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve directory %s: %w", dir, err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("read directory %s: %w", dir, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("read directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return root, nil
}

// csvWalker collects CSV files. ancestors holds the real paths of the
// directories on the current path from the root, to detect link loops.
type csvWalker struct {
	ancestors map[string]bool
	files     []File
}

// walk reads the directory at path. real is path without symbolic links, and
// prefix is the relative name of the directory with a trailing slash.
func (w *csvWalker) walk(path, real, prefix string) error {
	if w.ancestors[real] {
		return fmt.Errorf("%s is a symbolic link loop", strings.TrimSuffix(prefix, "/"))
	}
	w.ancestors[real] = true
	defer delete(w.ancestors, real)

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := w.visit(filepath.Join(path, entry.Name()), prefix+entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// visit handles one directory entry with the relative name name.
func (w *csvWalker) visit(path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if info.IsDir() {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		return w.walk(path, real, name+"/")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".csv") {
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	w.files = append(w.files, File{Path: path, Name: name, EntryName: name})
	return nil
}

// SourceFile records the content of one source file.
type SourceFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Source is a snapshot of the files that a send reads. A repeated send uses it
// to find out that the files changed.
type Source struct {
	Dir       string       `json:"dir"`
	Files     []SourceFile `json:"files"`
	Completed bool         `json:"completed"`
}

// SnapshotSource records the size and the SHA-256 hash of each file.
func SnapshotSource(dir string, files []File) (Source, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Source{}, fmt.Errorf("resolve directory %s: %w", dir, err)
	}

	snapshot := Source{Dir: root, Files: make([]SourceFile, 0, len(files))}
	for _, file := range files {
		size, sum, err := hashFile(file.Path)
		if err != nil {
			return Source{}, fmt.Errorf("hash %s: %w", file.Name, err)
		}
		snapshot.Files = append(snapshot.Files, SourceFile{Name: file.Name, Size: size, SHA256: sum})
	}
	return snapshot, nil
}

func hashFile(path string) (size int64, sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	size, err = io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

// Compare returns nil when current has the same directory and files as s. The
// Completed flag is not part of the comparison. Otherwise the error lists the
// differences.
func (s Source) Compare(current Source) error {
	var diffs []string
	if s.Dir != current.Dir {
		diffs = append(diffs, fmt.Sprintf("directory is %s, was %s", current.Dir, s.Dir))
	}

	stored := indexByName(s.Files)
	now := indexByName(current.Files)

	changed, added := diffFiles(stored, current.Files)
	diffs = append(diffs, changed...)
	var removed []string
	for _, f := range s.Files {
		if _, ok := now[f.Name]; !ok {
			removed = append(removed, f.Name)
		}
	}
	if len(added) > 0 {
		diffs = append(diffs, "added: "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		diffs = append(diffs, "removed: "+strings.Join(removed, ", "))
	}

	if len(diffs) == 0 {
		return nil
	}
	return errors.New(strings.Join(diffs, "; "))
}

func indexByName(files []SourceFile) map[string]SourceFile {
	index := make(map[string]SourceFile, len(files))
	for _, f := range files {
		index[f.Name] = f
	}
	return index
}

// diffFiles compares current with stored. It returns a message for each file
// whose content changed and the names of the files that stored lacks.
func diffFiles(stored map[string]SourceFile, current []SourceFile) (changed, added []string) {
	for _, f := range current {
		old, ok := stored[f.Name]
		switch {
		case !ok:
			added = append(added, f.Name)
		case old.Size != f.Size:
			changed = append(changed, fmt.Sprintf("size changed: %s (%d -> %d bytes)", f.Name, old.Size, f.Size))
		case old.SHA256 != f.SHA256:
			changed = append(changed, fmt.Sprintf("content changed: %s", f.Name))
		}
	}
	return changed, added
}

// SaveSource writes the snapshot to path. It writes a temporary file first, so
// a crash leaves the old file intact.
func SaveSource(path string, s Source) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal source: %w", err)
	}

	tmpName := path + ".tmp"
	if err := os.WriteFile(tmpName, data, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write source file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace source file: %w", err)
	}
	return nil
}

// LoadSource reads the snapshot at path.
func LoadSource(path string) (Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Source{}, fmt.Errorf("read source file: %w", err)
	}
	var s Source
	if err := json.Unmarshal(data, &s); err != nil {
		return Source{}, fmt.Errorf("parse source file %s: %w", path, err)
	}
	return s, nil
}
