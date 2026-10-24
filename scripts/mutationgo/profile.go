package main

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
)

// block is a block of statements in a cover profile, without its count.
type block struct {
	// File is the import path of the file, as the profile names it.
	File                string
	StartLine, StartCol int
	EndLine, EndCol     int
	NumStmt             int
}

// writeMergedProfile writes one cover profile in set mode. A block counts as
// run if any test ran it.
func writeMergedProfile(w io.Writer, results []testResult) error {
	merged := map[block]int{}

	for _, result := range results {
		for _, profile := range result.Profiles {
			for _, b := range profile.Blocks {
				key := block{profile.FileName, b.StartLine, b.StartCol, b.EndLine, b.EndCol, b.NumStmt}
				merged[key] = max(merged[key], min(b.Count, 1))
			}
		}
	}

	blocks := slices.SortedFunc(maps.Keys(merged), func(a, b block) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.StartLine, b.StartLine),
			cmp.Compare(a.StartCol, b.StartCol),
			cmp.Compare(a.EndLine, b.EndLine),
			cmp.Compare(a.EndCol, b.EndCol),
		)
	})

	// bufio.Writer keeps the first write error and Flush returns it.
	out := bufio.NewWriter(w)
	_, _ = fmt.Fprintln(out, "mode: set")

	for _, b := range blocks {
		_, _ = fmt.Fprintf(out, "%s:%d.%d,%d.%d %d %d\n",
			b.File, b.StartLine, b.StartCol, b.EndLine, b.EndCol, b.NumStmt, merged[b])
	}

	return out.Flush()
}
