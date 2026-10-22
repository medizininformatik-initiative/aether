package main

import (
	"regexp"
	"slices"
	"strings"
)

// selectTests returns the tests that run a statement on one of the lines of
// the file, in the order of the map.
func selectTests(tm testMap, file string, lines []int) []testID {
	var indexes []int

	for _, mb := range tm.Files[file] {
		for _, line := range lines {
			if mb.StartLine <= line && line <= mb.EndLine {
				indexes = append(indexes, mb.Tests...)

				break
			}
		}
	}

	slices.Sort(indexes)
	indexes = slices.Compact(indexes)

	tests := make([]testID, 0, len(indexes))
	for _, i := range indexes {
		tests = append(tests, tm.Tests[i])
	}

	return tests
}

// selectedTestArgs turns the arguments of the mutant call into a call that
// runs only the given tests. The test names go in one -run pattern for all
// packages, so a package can run a test of the same name that does not cover
// the line. This only adds work.
func selectedTestArgs(args []string, tests []testID) []string {
	var packages, names []string

	for _, test := range tests {
		if !slices.Contains(packages, test.Package) {
			packages = append(packages, test.Package)
		}

		name := regexp.QuoteMeta(test.Name)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	selected := slices.Clone(args[:len(args)-1])
	selected = append(selected, "-run", "^("+strings.Join(names, "|")+")$")

	return append(selected, packages...)
}
