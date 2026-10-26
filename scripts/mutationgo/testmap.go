package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"golang.org/x/tools/cover"
)

// testID names one top-level test.
type testID struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

// testResult is the coverage of one test that ran alone.
type testResult struct {
	ID       testID
	Profiles []*cover.Profile
}

// mapBlock is a range of lines. Tests holds the indexes, in testMap.Tests, of
// the tests that run a statement in the range.
type mapBlock struct {
	StartLine int   `json:"start"`
	EndLine   int   `json:"end"`
	Tests     []int `json:"tests"`
}

// testMap tells which tests cover which lines. Files is keyed by the path of a
// source file relative to the module root.
type testMap struct {
	Tests []testID              `json:"tests"`
	Files map[string][]mapBlock `json:"files"`
}

// buildTestMap joins the coverage of single tests. Blocks of files outside the
// module are not in the result, and neither are blocks that no test ran.
func buildTestMap(modulePath string, results []testResult) testMap {
	prefix := modulePath + "/"
	tm := testMap{Files: map[string][]mapBlock{}}
	byRange := map[lineRange][]int{}

	for i, result := range results {
		tm.Tests = append(tm.Tests, result.ID)
		addCoveredRanges(byRange, prefix, i, result.Profiles)
	}

	for key, tests := range byRange {
		tm.Files[key.file] = append(tm.Files[key.file], mapBlock{StartLine: key.start, EndLine: key.end, Tests: tests})
	}

	for _, blocks := range tm.Files {
		slices.SortFunc(blocks, func(a, b mapBlock) int {
			return cmp.Or(cmp.Compare(a.StartLine, b.StartLine), cmp.Compare(a.EndLine, b.EndLine))
		})
	}

	return tm
}

type lineRange struct {
	file       string
	start, end int
}

// addCoveredRanges adds test i to each range of the module that it runs.
func addCoveredRanges(byRange map[lineRange][]int, prefix string, i int, profiles []*cover.Profile) {
	for _, profile := range profiles {
		file, inModule := strings.CutPrefix(profile.FileName, prefix)
		if !inModule {
			continue
		}

		for _, b := range profile.Blocks {
			if b.Count == 0 {
				continue
			}

			key := lineRange{file, b.StartLine, b.EndLine}
			// Blocks on the same lines of one profile give one entry.
			if tests := byRange[key]; len(tests) == 0 || tests[len(tests)-1] != i {
				byRange[key] = append(tests, i)
			}
		}
	}
}

func writeTestMap(path string, tm testMap) error {
	data, err := json.Marshal(tm)
	if err != nil {
		return fmt.Errorf("encode test map: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write test map: %w", err)
	}

	return nil
}

func readTestMap(path string) (testMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return testMap{}, fmt.Errorf("read test map: %w", err)
	}

	var tm testMap
	if err := json.Unmarshal(data, &tm); err != nil {
		return testMap{}, fmt.Errorf("decode test map %s: %w", path, err)
	}

	return tm, nil
}
