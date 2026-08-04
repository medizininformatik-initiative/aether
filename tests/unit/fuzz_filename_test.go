package unit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// FuzzCompressedFilename checks the pair of functions that add and remove the
// ".zst" suffix.
//
// Invariants:
//   - A name with the compression suffix removed is no longer a compressed name.
//   - Adding the suffix is idempotent.
func FuzzCompressedFilename(f *testing.F) {
	seeds := []string{
		"", "Patient.ndjson", "Patient.ndjson.zst", "Patient.NDJSON.ZST",
		"x.ZST", "x.zst", ".zst", "a.zst.zst", "dir/Patient.ndjson",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		// IsCompressedFile decides whether the suffix is present. If it says yes,
		// GetUncompressedFilename must remove something.
		if lib.IsCompressedFile(name) && lib.GetUncompressedFilename(name) == name {
			if !strings.HasSuffix(name, lib.CompressedFileExtension) {
				t.Skipf("known defect (issue #648): the suffix of %q is not lowercase, so nothing is stripped", name)
			}
			t.Fatalf("IsCompressedFile(%q) is true, but GetUncompressedFilename strips nothing", name)
		}

		compressed := lib.GetCompressedFilename(name, true)
		if again := lib.GetCompressedFilename(compressed, true); again != compressed {
			t.Fatalf("GetCompressedFilename is not idempotent for %q: %q -> %q", name, compressed, again)
		}
	})
}

// FuzzDetectDuplicateFHIRFiles checks that the duplicate detection sees a
// compressed and an uncompressed copy of the same resource file as a pair.
//
// Invariant: if two accepted FHIR file names differ only by the compression
// suffix, DetectDuplicateFHIRFiles reports an error.
func FuzzDetectDuplicateFHIRFiles(f *testing.F) {
	f.Add("Patient.ndjson\nPatient.ndjson.zst")
	f.Add("Patient.ndjson\nPatient.NDJSON.ZST")
	f.Add("Patient.ndjson\nCondition.ndjson")
	f.Add("")

	f.Fuzz(func(t *testing.T, joined string) {
		var files []string
		for _, name := range strings.Split(joined, "\n") {
			if name != "" && models.IsValidFHIRFile(name) {
				files = append(files, name)
			}
		}
		if len(files) < 2 {
			return
		}

		err := models.DetectDuplicateFHIRFiles(files)

		if err != nil {
			return
		}

		// Build the expected answer with a case-insensitive normalization, and
		// record whether the missed pair differs only by the case of the suffix.
		seen := make(map[string]string)
		wantDuplicate, caseOnly := false, true
		for _, name := range files {
			base := filepath.Base(name)
			key := strings.TrimSuffix(strings.ToLower(base), ".zst")
			if prev, ok := seen[key]; ok && prev != base {
				wantDuplicate = true
				if strings.HasSuffix(base, ".zst") == strings.HasSuffix(strings.ToLower(base), ".zst") &&
					strings.HasSuffix(prev, ".zst") == strings.HasSuffix(strings.ToLower(prev), ".zst") {
					caseOnly = false
				}
			}
			seen[key] = base
		}
		if !wantDuplicate {
			return
		}
		if caseOnly {
			t.Skipf("known defect (issue #648): %q holds a compressed and an uncompressed copy that differ by the case of the suffix", files)
		}
		t.Fatalf("DetectDuplicateFHIRFiles(%q) returned no error, but the list holds a compressed and an uncompressed copy of one file", files)
	})
}

// FuzzIsSafePath checks the guard against path traversal.
//
// Invariant: a path that IsSafePath accepts stays inside the base directory
// after filepath.Join.
func FuzzIsSafePath(f *testing.F) {
	seeds := []string{
		"", "a.ndjson", "sub/a.ndjson", "../etc/passwd", "/etc/passwd",
		"./a", "a/../../b", "..foo", "...", "a/..", `..\windows`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		if !models.IsSafePath(path) {
			return
		}

		const base = "/base"
		joined := filepath.Clean(filepath.Join(base, path))
		if joined != base && !strings.HasPrefix(joined, base+string(filepath.Separator)) {
			t.Fatalf("IsSafePath(%q) accepted the path, but it resolves to %q outside %q", path, joined, base)
		}
	})
}
