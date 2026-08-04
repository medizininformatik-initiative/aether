package services

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// renderCSVCell is the last step between a value decoded from the flattener's
// NDJSON response and a CSV cell. Its default branch also guards against a
// value that encoding/json cannot marshal. The decoder never produces such a
// value, so only a direct call from this package reaches that guard.
func TestRenderCSVCell(t *testing.T) {
	tests := map[string]struct {
		value    any
		expected string
	}{
		"null becomes an empty cell":     {nil, ""},
		"string stays verbatim":          {"Doe, John", "Doe, John"},
		"integer keeps its source text":  {json.Number("42"), "42"},
		"decimal keeps trailing zeros":   {json.Number("1.500"), "1.500"},
		"true becomes true":              {true, "true"},
		"false becomes false":            {false, "false"},
		"object becomes compact JSON":    {map[string]any{"code": "abc"}, `{"code":"abc"}`},
		"array becomes compact JSON":     {[]any{"a", json.Number("1")}, `["a",1]`},
		"empty string stays empty":       {"", ""},
		"nested object becomes one cell": {map[string]any{"a": map[string]any{"b": "c"}}, `{"a":{"b":"c"}}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, renderCSVCell(tt.value))
		})
	}

	t.Run("unmarshalable value falls back to its Go rendering", func(t *testing.T) {
		// A channel has no JSON representation. encoding/json never decodes
		// one, so this asserts the defensive branch, not a real response.
		value := make(chan int)

		assert.Equal(t, fmt.Sprint(value), renderCSVCell(value))
	})
}
