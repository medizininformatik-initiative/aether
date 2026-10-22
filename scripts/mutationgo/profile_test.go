package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/cover"
)

const testModule = "example.com/mod"

const profileOne = `mode: set
example.com/mod/lib/a.go:3.10,5.2 1 1
example.com/mod/lib/a.go:7.10,9.2 2 0
example.com/mod/lib/b.go:4.1,4.20 1 1
other.org/dep/c.go:1.1,2.2 1 1
`

// profilesOf reads a cover profile.
func profilesOf(t *testing.T, content string) []*cover.Profile {
	t.Helper()

	profiles, err := cover.ParseProfilesFromReader(strings.NewReader(content))
	require.NoError(t, err)

	return profiles
}

func TestBuildTestMapKeepsCoveredBlocksOfTheModuleByRelativePath(t *testing.T) {
	got := buildTestMap(testModule, []testResult{
		{ID: testID{Package: "example.com/mod/lib", Name: "TestA"}, Profiles: profilesOf(t, profileOne)},
		{ID: testID{Package: "example.com/mod/lib", Name: "TestB"}, Profiles: profilesOf(t,
			"mode: set\nexample.com/mod/lib/a.go:3.10,5.2 1 1\nexample.com/mod/lib/b.go:4.1,4.20 1 0\n")},
	})

	assert.Equal(t, []testID{
		{Package: "example.com/mod/lib", Name: "TestA"},
		{Package: "example.com/mod/lib", Name: "TestB"},
	}, got.Tests)
	assert.Equal(t, map[string][]mapBlock{
		"lib/a.go": {{StartLine: 3, EndLine: 5, Tests: []int{0, 1}}},
		"lib/b.go": {{StartLine: 4, EndLine: 4, Tests: []int{0}}},
	}, got.Files)
}
