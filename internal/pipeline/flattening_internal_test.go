package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

func makeBundleEntry(resource map[string]any) map[string]any {
	return map[string]any{"resource": resource}
}

func makeProv(targetRefs []string, groupID string) map[string]any {
	targets := make([]any, len(targetRefs))
	for i, ref := range targetRefs {
		targets[i] = map[string]any{"reference": ref}
	}
	return map[string]any{
		"resourceType": "Provenance",
		"target":       targets,
		"entity": []any{
			map[string]any{
				"role": "source",
				"what": map[string]any{
					"identifier": map[string]any{
						"system": models.AttributeGroupNamingSystem,
						"value":  groupID,
					},
				},
			},
		},
	}
}

func TestExtractBundleResources(t *testing.T) {
	t.Run("separates clinical resources from Provenance and builds index", func(t *testing.T) {
		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		condition := map[string]any{"resourceType": "Condition", "id": "c1"}
		prov := makeProv([]string{"Patient/p1", "Condition/c1"}, "group-x")

		bundle := map[string]any{
			"resourceType": "Bundle",
			"entry": []any{
				makeBundleEntry(patient),
				makeBundleEntry(condition),
				makeBundleEntry(prov),
			},
		}

		resources, index := extractBundleResources(bundle)
		assert.Len(t, resources, 2)
		assert.Equal(t, "Patient", resources[0]["resourceType"])
		assert.Equal(t, "Condition", resources[1]["resourceType"])
		assert.Equal(t, []string{"group-x"}, index["Patient/p1"])
		assert.Equal(t, []string{"group-x"}, index["Condition/c1"])
	})

	t.Run("returns nil for non-array entry", func(t *testing.T) {
		bundle := map[string]any{
			"resourceType": "Bundle",
			"entry":        "not an array",
		}
		resources, index := extractBundleResources(bundle)
		assert.Nil(t, resources)
		assert.Empty(t, index)
	})

	t.Run("returns nil for missing entry", func(t *testing.T) {
		resources, index := extractBundleResources(map[string]any{"resourceType": "Bundle"})
		assert.Nil(t, resources)
		assert.Empty(t, index)
	})

	t.Run("skips non-map entries and entries without resource", func(t *testing.T) {
		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		bundle := map[string]any{
			"resourceType": "Bundle",
			"entry": []any{
				"not a map",
				map[string]any{"fullUrl": "urn:uuid:no-resource"},
				map[string]any{"resource": "not a map"},
				makeBundleEntry(patient),
			},
		}
		resources, _ := extractBundleResources(bundle)
		assert.Len(t, resources, 1)
		assert.Equal(t, "p1", resources[0]["id"])
	})

	t.Run("handles bundle without Provenance", func(t *testing.T) {
		patient := map[string]any{"resourceType": "Patient", "id": "p1"}
		bundle := map[string]any{
			"resourceType": "Bundle",
			"entry":        []any{makeBundleEntry(patient)},
		}
		resources, index := extractBundleResources(bundle)
		assert.Len(t, resources, 1)
		assert.Empty(t, index)
	})
}

func TestExtractAttributeGroupID(t *testing.T) {
	entityWithSystem := func(system, value string) []any {
		return []any{
			map[string]any{
				"role": "source",
				"what": map[string]any{
					"identifier": map[string]any{
						"system": system,
						"value":  value,
					},
				},
			},
		}
	}

	tests := []struct {
		name       string
		provenance map[string]any
		want       string
	}{
		{
			name:       "missing entity array",
			provenance: map[string]any{"resourceType": "Provenance"},
			want:       "",
		},
		{
			name:       "entity is not an array",
			provenance: map[string]any{"entity": "not-an-array"},
			want:       "",
		},
		{
			name:       "empty entity array",
			provenance: map[string]any{"entity": []any{}},
			want:       "",
		},
		{
			name:       "entity entry of wrong type",
			provenance: map[string]any{"entity": []any{"not-a-map", 42}},
			want:       "",
		},
		{
			name:       "entity missing what",
			provenance: map[string]any{"entity": []any{map[string]any{"role": "source"}}},
			want:       "",
		},
		{
			name:       "what is not a map",
			provenance: map[string]any{"entity": []any{map[string]any{"what": "not-a-map"}}},
			want:       "",
		},
		{
			name: "what missing identifier",
			provenance: map[string]any{"entity": []any{
				map[string]any{"what": map[string]any{"reference": "Patient/1"}},
			}},
			want: "",
		},
		{
			name: "identifier is not a map",
			provenance: map[string]any{"entity": []any{
				map[string]any{"what": map[string]any{"identifier": "not-a-map"}},
			}},
			want: "",
		},
		{
			name:       "identifier system does not match naming system",
			provenance: map[string]any{"entity": entityWithSystem("https://example.com/other", "group-x")},
			want:       "",
		},
		{
			name:       "matching system with missing value",
			provenance: map[string]any{"entity": []any{map[string]any{"what": map[string]any{"identifier": map[string]any{"system": models.AttributeGroupNamingSystem}}}}},
			want:       "",
		},
		{
			name:       "matching system with empty value",
			provenance: map[string]any{"entity": entityWithSystem(models.AttributeGroupNamingSystem, "")},
			want:       "",
		},
		{
			name:       "matching system yields group ID",
			provenance: map[string]any{"entity": entityWithSystem(models.AttributeGroupNamingSystem, "group-a")},
			want:       "group-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				assert.Equal(t, tt.want, extractAttributeGroupID(tt.provenance))
			})
		})
	}
}

func TestPerGroupBudget(t *testing.T) {
	tests := []struct {
		name           string
		batchSizeBytes int
		numGroups      int
		want           int
	}{
		{name: "even division", batchSizeBytes: 100, numGroups: 4, want: 25},
		{name: "budget below one byte clamps to one", batchSizeBytes: 10, numGroups: 100, want: 1},
		{name: "single group gets full budget", batchSizeBytes: 100, numGroups: 1, want: 100},
		{name: "integer division truncates but stays at least one", batchSizeBytes: 3, numGroups: 2, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, perGroupBudget(tt.batchSizeBytes, tt.numGroups))
		})
	}
}

func TestBuildProvenanceIndex(t *testing.T) {
	t.Run("maps each target reference to attribute group ID", func(t *testing.T) {
		provs := []map[string]any{
			makeProv([]string{"Patient/1", "Condition/2"}, "group-a"),
			makeProv([]string{"Observation/3"}, "group-b"),
		}
		index := buildProvenanceIndex(provs)
		assert.Equal(t, []string{"group-a"}, index["Patient/1"])
		assert.Equal(t, []string{"group-a"}, index["Condition/2"])
		assert.Equal(t, []string{"group-b"}, index["Observation/3"])
	})

	t.Run("appends multiple group IDs for the same reference", func(t *testing.T) {
		provs := []map[string]any{
			makeProv([]string{"Procedure/1"}, "group-a"),
			makeProv([]string{"Procedure/1"}, "group-b"),
		}
		index := buildProvenanceIndex(provs)
		assert.ElementsMatch(t, []string{"group-a", "group-b"}, index["Procedure/1"])
	})

	t.Run("skips Provenance without attribute group entity", func(t *testing.T) {
		prov := map[string]any{
			"target": []any{map[string]any{"reference": "Patient/1"}},
			"entity": []any{
				map[string]any{
					"role": "source",
					"what": map[string]any{
						"identifier": map[string]any{
							"system": "https://example.com/other-system",
							"value":  "x",
						},
					},
				},
			},
		}
		assert.Empty(t, buildProvenanceIndex([]map[string]any{prov}))
	})

	t.Run("skips Provenance with non-array target", func(t *testing.T) {
		prov := map[string]any{
			"target": "not-an-array",
			"entity": []any{
				map[string]any{
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}
		assert.Empty(t, buildProvenanceIndex([]map[string]any{prov}))
	})

	t.Run("skips non-map target entries and missing references", func(t *testing.T) {
		prov := map[string]any{
			"target": []any{
				"not-a-map",
				map[string]any{"display": "no reference"},
				map[string]any{"reference": ""},
				map[string]any{"reference": "Patient/1"},
			},
			"entity": []any{
				map[string]any{
					"what": map[string]any{
						"identifier": map[string]any{
							"system": models.AttributeGroupNamingSystem,
							"value":  "group-x",
						},
					},
				},
			},
		}
		index := buildProvenanceIndex([]map[string]any{prov})
		assert.Len(t, index, 1)
		assert.Equal(t, []string{"group-x"}, index["Patient/1"])
	})

	t.Run("skips Provenance with non-array entity", func(t *testing.T) {
		prov := map[string]any{
			"target": []any{map[string]any{"reference": "Patient/1"}},
			"entity": "not-an-array",
		}
		assert.Empty(t, buildProvenanceIndex([]map[string]any{prov}))
	})

	t.Run("skips entity with non-map what", func(t *testing.T) {
		prov := map[string]any{
			"target": []any{map[string]any{"reference": "Patient/1"}},
			"entity": []any{
				map[string]any{"what": "not-a-map"},
			},
		}
		assert.Empty(t, buildProvenanceIndex([]map[string]any{prov}))
	})

	t.Run("returns empty index for empty input", func(t *testing.T) {
		assert.Empty(t, buildProvenanceIndex(nil))
		assert.Empty(t, buildProvenanceIndex([]map[string]any{}))
	})
}
