package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func sampleMap() testMap {
	return testMap{
		Tests: []testID{
			{Package: "example.com/mod/lib", Name: "TestA"},
			{Package: "example.com/mod/lib", Name: "TestB"},
			{Package: "example.com/mod/tests/unit", Name: "TestC"},
		},
		Files: map[string][]mapBlock{
			"lib/a.go": {
				{StartLine: 3, EndLine: 5, Tests: []int{0, 2}},
				{StartLine: 7, EndLine: 9, Tests: []int{1}},
			},
		},
	}
}

func TestSelectTestsReturnsTestsOfBlocksThatHoldAChangedLine(t *testing.T) {
	got := selectTests(sampleMap(), "lib/a.go", []int{5})

	assert.Equal(t, []testID{
		{Package: "example.com/mod/lib", Name: "TestA"},
		{Package: "example.com/mod/tests/unit", Name: "TestC"},
	}, got)
}

func TestSelectTestsJoinsSeveralLinesWithoutDuplicates(t *testing.T) {
	got := selectTests(sampleMap(), "lib/a.go", []int{3, 4, 8})

	assert.Equal(t, []testID{
		{Package: "example.com/mod/lib", Name: "TestA"},
		{Package: "example.com/mod/lib", Name: "TestB"},
		{Package: "example.com/mod/tests/unit", Name: "TestC"},
	}, got)
}

func TestSelectTestsFindsNothingForUncoveredLinesAndFiles(t *testing.T) {
	assert.Empty(t, selectTests(sampleMap(), "lib/a.go", []int{6, 10}))
	assert.Empty(t, selectTests(sampleMap(), "lib/other.go", []int{4}))
}

func TestSelectedTestArgsLimitTheRunToTheTests(t *testing.T) {
	tests := []testID{
		{Package: "example.com/mod/lib", Name: "TestA"},
		{Package: "example.com/mod/lib", Name: "TestB"},
		{Package: "example.com/mod/tests/unit", Name: "TestA"},
	}

	got := selectedTestArgs([]string{"test", "-tags", "x", "-timeout", "5s", "-failfast", "./..."}, tests)

	assert.Equal(t, []string{
		"test", "-tags", "x", "-timeout", "5s", "-failfast",
		"-run", "^(TestA|TestB)$",
		"example.com/mod/lib", "example.com/mod/tests/unit",
	}, got)
}

func TestSelectedTestArgsQuoteRegexCharacters(t *testing.T) {
	got := selectedTestArgs([]string{"test", "./..."}, []testID{{Package: "p", Name: "Test.A"}})

	assert.Equal(t, []string{"test", "-run", `^(Test\.A)$`, "p"}, got)
}
