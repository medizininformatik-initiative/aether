package main

import "slices"

type callKind int

const (
	// callOther is every call that the wrapper hands to the real go command.
	callOther callKind = iota
	// callCoverage is the test run of gremlins that measures coverage.
	callCoverage
	// callMutant is the test run of gremlins for one mutant.
	callMutant
)

const allPackages = "./..."

// classify tells which of the three go calls of gremlins the arguments are.
func classify(args []string) callKind {
	if len(args) < 3 || args[0] != "test" || args[len(args)-1] != allPackages {
		return callOther
	}

	if coverProfilePath(args) != "" {
		return callCoverage
	}

	if slices.Contains(args, "-failfast") {
		return callMutant
	}

	return callOther
}

// coverProfilePath returns the value of the -coverprofile flag, or an empty
// string if the flag is absent. The last argument is the package pattern, not
// a flag value.
func coverProfilePath(args []string) string {
	return flagValue(args[:len(args)-1], "-coverprofile")
}
