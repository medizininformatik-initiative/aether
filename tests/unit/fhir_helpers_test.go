package unit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestWriteNDJSONLine(t *testing.T) {
	var buf bytes.Buffer
	resource := lib.FHIRResource{"resourceType": "Patient", "id": "p1"}

	require.NoError(t, lib.WriteNDJSONLine(&buf, resource))

	out := buf.String()
	assert.True(t, strings.HasSuffix(out, "\n"), "each NDJSON record ends with a newline")
	assert.Contains(t, out, `"resourceType":"Patient"`)
	assert.Contains(t, out, `"id":"p1"`)
	assert.Equal(t, 1, strings.Count(out, "\n"), "single resource writes exactly one line")
}

func TestGroupByResourceType(t *testing.T) {
	tests := []struct {
		name      string
		resources []lib.FHIRResource
		want      map[string]int
		wantErr   bool
	}{
		{
			name: "groups by type preserving counts",
			resources: []lib.FHIRResource{
				{"resourceType": "Patient", "id": "1"},
				{"resourceType": "Observation", "id": "2"},
				{"resourceType": "Patient", "id": "3"},
			},
			want: map[string]int{"Patient": 2, "Observation": 1},
		},
		{
			name:      "empty input yields empty map",
			resources: []lib.FHIRResource{},
			want:      map[string]int{},
		},
		{
			name: "missing resourceType is an error",
			resources: []lib.FHIRResource{
				{"id": "1"},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, err := lib.GroupByResourceType(tt.resources)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, groups, len(tt.want))
			for rt, count := range tt.want {
				assert.Len(t, groups[rt], count)
			}
		})
	}
}

func TestValidateFHIRResource(t *testing.T) {
	tests := []struct {
		name     string
		resource lib.FHIRResource
		wantErr  bool
	}{
		{"valid resource", lib.FHIRResource{"resourceType": "Patient"}, false},
		{"missing resourceType", lib.FHIRResource{"id": "1"}, true},
		{"non-string resourceType", lib.FHIRResource{"resourceType": 42}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := lib.ValidateFHIRResource(tt.resource)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
