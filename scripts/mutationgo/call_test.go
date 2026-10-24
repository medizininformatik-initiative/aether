package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyRecognizesTheCoverageCallOfGremlins(t *testing.T) {
	args := []string{
		"test", "-tags", "integration", "-coverpkg", "./internal/...",
		"-cover", "-coverprofile", "/work/coverage", "./...",
	}

	assert.Equal(t, callCoverage, classify(args))
	assert.Equal(t, "/work/coverage", coverProfilePath(args))
}

func TestClassifyRecognizesTheMutantCallOfGremlins(t *testing.T) {
	args := []string{"test", "-timeout", "12s", "-failfast", "./..."}

	assert.Equal(t, callMutant, classify(args))
}

func TestClassifyLeavesOtherCallsAlone(t *testing.T) {
	for name, args := range map[string][]string{
		"no arguments":        {},
		"module download":     {"mod", "download"},
		"test of one package": {"test", "-failfast", "./internal/lib"},
		"test without flags":  {"test", "./..."},
		"build":               {"build", "-failfast", "./..."},
		"coverprofile alone":  {"test", "-coverprofile"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, callOther, classify(args))
		})
	}
}
