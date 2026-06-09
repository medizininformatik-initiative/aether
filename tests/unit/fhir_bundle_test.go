package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/lib"
)

func TestResourceType(t *testing.T) {
	assert.Equal(t, "Patient", lib.ResourceType(map[string]any{"resourceType": "Patient"}))
	assert.Equal(t, "", lib.ResourceType(map[string]any{}), "missing resourceType yields empty string")
	assert.Equal(t, "", lib.ResourceType(map[string]any{"resourceType": 42}), "non-string resourceType yields empty string")
}

func TestResourceID(t *testing.T) {
	assert.Equal(t, "p1", lib.ResourceID(map[string]any{"id": "p1"}))
	assert.Equal(t, "", lib.ResourceID(map[string]any{}), "missing id yields empty string")
	assert.Equal(t, "", lib.ResourceID(map[string]any{"id": 42}), "non-string id yields empty string")
}

func TestIsProvenance(t *testing.T) {
	assert.True(t, lib.IsProvenance(map[string]any{"resourceType": "Provenance"}))
	assert.False(t, lib.IsProvenance(map[string]any{"resourceType": "Patient"}))
	assert.False(t, lib.IsProvenance(map[string]any{}), "missing resourceType is not Provenance")
}

func TestIsBundle(t *testing.T) {
	assert.True(t, lib.IsBundle(map[string]any{"resourceType": "Bundle"}))
	assert.False(t, lib.IsBundle(map[string]any{"resourceType": "Patient"}))
	assert.False(t, lib.IsBundle(map[string]any{}), "missing resourceType is not Bundle")
}

func TestLibResourceReference(t *testing.T) {
	assert.Equal(t, "Patient/p1", lib.ResourceReference(map[string]any{"resourceType": "Patient", "id": "p1"}))
	assert.Equal(t, "", lib.ResourceReference(map[string]any{"resourceType": "Patient"}), "missing id yields empty reference")
	assert.Equal(t, "", lib.ResourceReference(map[string]any{"id": "p1"}), "missing resourceType yields empty reference")
	assert.Equal(t, "", lib.ResourceReference(map[string]any{}))
}

func TestEntryResource(t *testing.T) {
	entry := map[string]any{
		"fullUrl":  "urn:uuid:p1",
		"resource": map[string]any{"resourceType": "Patient", "id": "p1"},
	}
	res, ok := lib.EntryResource(entry)
	assert.True(t, ok)
	assert.Equal(t, "Patient", lib.ResourceType(res))

	_, ok = lib.EntryResource(map[string]any{"fullUrl": "urn:uuid:x"})
	assert.False(t, ok, "entry without resource yields ok=false")
}

func TestBundleResources(t *testing.T) {
	bundle := map[string]any{
		"resourceType": "Bundle",
		"type":         "collection",
		"entry": []any{
			map[string]any{"resource": map[string]any{"resourceType": "Patient", "id": "p1"}},
			"not-an-object",                         // malformed entry: skipped
			map[string]any{"fullUrl": "urn:uuid:x"}, // entry without resource: skipped
			map[string]any{"resource": map[string]any{"resourceType": "Observation", "id": "o1"}},
		},
	}
	resources := lib.BundleResources(bundle)
	assert.Len(t, resources, 2, "malformed entries and resource-less entries are skipped")
	assert.Equal(t, "Patient", lib.ResourceType(resources[0]))
	assert.Equal(t, "Observation", lib.ResourceType(resources[1]))

	assert.Empty(t, lib.BundleResources(map[string]any{"resourceType": "Bundle"}), "bundle without entry array yields no resources")
}

func TestBundleEntries(t *testing.T) {
	bundle := map[string]any{
		"resourceType": "Bundle",
		"entry": []any{
			map[string]any{"fullUrl": "urn:uuid:p1", "resource": map[string]any{"resourceType": "Patient"}},
			"not-an-object", // malformed entry: skipped
			map[string]any{"request": map[string]any{"url": "Patient"}},
		},
	}
	entries := lib.BundleEntries(bundle)
	assert.Len(t, entries, 2, "malformed entries are skipped")
	assert.Equal(t, "urn:uuid:p1", entries[0]["fullUrl"])

	// Entries are the underlying maps: mutation propagates to the bundle.
	entries[1]["fullUrl"] = "urn:uuid:added"
	rawEntries := bundle["entry"].([]any)
	assert.Equal(t, "urn:uuid:added", rawEntries[2].(map[string]any)["fullUrl"])

	assert.Empty(t, lib.BundleEntries(map[string]any{"resourceType": "Bundle"}), "bundle without entry array yields no entries")
}
