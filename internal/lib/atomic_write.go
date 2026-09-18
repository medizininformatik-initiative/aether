package lib

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// tempNameFormat builds the name of a temporary file from the name of the
// destination file and a unique suffix.
const tempNameFormat = ".%s.tmp.%s"

// uuidGlob matches the 8-4-4-4-12 hexadecimal form of a UUID.
const uuidGlob = "????????-????-????-????-????????????"

// tempFileGlob matches the temporary files that this module creates, and no
// other file. It comes from tempNameFormat, thus it cannot go out of step.
var tempFileGlob = fmt.Sprintf(tempNameFormat, "*", uuidGlob)

// tempName returns the name of the temporary file for the file base.
func tempName(base string) string {
	return fmt.Sprintf(tempNameFormat, base, uuid.New().String())
}

// tempPath returns a unique path next to dest for the file that receives the
// content before the rename.
func tempPath(dest string) string {
	return filepath.Join(filepath.Dir(dest), tempName(filepath.Base(dest)))
}

// RemoveStaleTempFiles removes the temporary files that a killed run left in
// dir. A run that fails removes its own temporary file. This function covers
// the run that the operating system stopped before it could do that.
func RemoveStaleTempFiles(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, tempFileGlob))
	if err != nil {
		return fmt.Errorf("failed to list stale temporary files in %s: %w", dir, err)
	}

	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale temporary file %s: %w", match, err)
		}
	}

	return nil
}

// AtomicWriteStream gives write a buffered writer for a temporary file next to
// dest, then renames that file to dest. A reader thus sees either no file or
// the complete file. If write returns an error or panics, or any step fails,
// the temporary file is removed and dest stays untouched.
func AtomicWriteStream(dest string, perm os.FileMode, write func(io.Writer) error) error {
	tmp := tempPath(dest)

	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}

	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmp)
		}
	}()

	if err := writeAndClose(file, write); err != nil {
		return err
	}

	if err := publish(OSFileOps{}, tmp, dest); err != nil {
		return err
	}

	published = true

	return nil
}

// publish renames tmp to dest. If the rename fails, it removes tmp, so that no
// incomplete file stays behind.
func publish(ops FileOps, tmp, dest string) error {
	if err := ops.Rename(tmp, dest); err != nil {
		_ = ops.Remove(tmp)
		return fmt.Errorf("failed to rename temporary file to %s: %w", dest, err)
	}

	return nil
}

// writeAndClose runs write against a buffered writer for file, then flushes and
// closes file. It reports the error of Close, because a write fault on a
// network mount often surfaces only there.
func writeAndClose(file *os.File, write func(io.Writer) error) error {
	buffered := bufio.NewWriter(file)

	if err := write(buffered); err != nil {
		_ = file.Close()
		return err
	}

	if err := buffered.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to flush temporary file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	return nil
}

// FileOps holds the file operations that AtomicWriteFileWith uses. A caller
// that must simulate a failure in its tests injects its own implementation.
type FileOps interface {
	WriteFile(name string, data []byte, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	Remove(name string) error
}

// OSFileOps does the file operations with the standard library.
type OSFileOps struct{}

func (OSFileOps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (OSFileOps) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
func (OSFileOps) Remove(name string) error             { return os.Remove(name) }

// AtomicWriteFile writes data to a temporary file next to dest and then renames
// it to dest. A reader thus sees either no file or the complete file. The
// temporary file is removed on every failure path.
func AtomicWriteFile(dest string, data []byte, perm os.FileMode) error {
	return AtomicWriteFileWith(OSFileOps{}, dest, data, perm)
}

// AtomicWriteFileWith is AtomicWriteFile with the file operations supplied by
// the caller.
func AtomicWriteFileWith(ops FileOps, dest string, data []byte, perm os.FileMode) error {
	tmp := tempPath(dest)

	if err := ops.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	return publish(ops, tmp, dest)
}
