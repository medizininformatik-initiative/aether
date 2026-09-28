package models

import (
	"encoding/json"
	"fmt"
)

// CRTDLDocument represents the CRTDL file structure
// Includes all top-level fields to preserve the full document during preprocessing.
// Extra captures any unknown top-level fields so they survive JSON round-trips
// (see issue #356).
type CRTDLDocument struct {
	Display          string                     `json:"-"`
	Version          string                     `json:"-"`
	CohortDefinition json.RawMessage            `json:"-"`
	DataExtraction   DataExtraction             `json:"-"`
	Extra            map[string]json.RawMessage `json:"-"`
}

// DataExtraction represents the dataExtraction section of a CRTDL document.
// Extra preserves unknown sibling fields.
type DataExtraction struct {
	AttributeGroups []AttributeGroup           `json:"-"`
	Extra           map[string]json.RawMessage `json:"-"`
}

// AttributeGroup represents an attributeGroup in the CRTDL dataExtraction.
// Extra preserves unknown fields such as `includeReferenceOnly`.
type AttributeGroup struct {
	ID             string                     `json:"-"` // Required: used for provenance-based resource routing
	Name           string                     `json:"-"` // Group name (used for CSV filename)
	GroupReference string                     `json:"-"` // Profile URL for resource matching
	Attributes     []Attribute                `json:"-"` // List of attributes to extract
	Extra          map[string]json.RawMessage `json:"-"`
}

// Attribute represents a single attribute within an attributeGroup.
// Extra preserves unknown fields.
type Attribute struct {
	AttributeRef string                     `json:"-"` // Element ID reference to lookup
	MustHave     bool                       `json:"-"` // Whether this attribute is required
	LinkedGroups []string                   `json:"-"` // Profile URLs for linked groups (resolved to IDs during preprocessing)
	Extra        map[string]json.RawMessage `json:"-"`
}

// --- Custom JSON marshaling preserves unknown fields (issue #356) ---

func unmarshalKnown(raw map[string]json.RawMessage, key string, target any) error {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	delete(raw, key)
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return json.Unmarshal(v, target)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("CRTDL marshal failed for %T: %v", v, err))
	}
	return b
}

func mergeExtra(out map[string]json.RawMessage, extra map[string]json.RawMessage) {
	for k, v := range extra {
		out[k] = v
	}
}

func (a *Attribute) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributeRef", &a.AttributeRef); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "mustHave", &a.MustHave); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "linkedGroups", &a.LinkedGroups); err != nil {
		return err
	}
	if len(raw) > 0 {
		a.Extra = raw
	}
	return nil
}

func (a Attribute) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, a.Extra)
	out["attributeRef"] = mustMarshal(a.AttributeRef)
	out["mustHave"] = mustMarshal(a.MustHave)
	if len(a.LinkedGroups) > 0 {
		out["linkedGroups"] = mustMarshal(a.LinkedGroups)
	}
	return json.Marshal(out)
}

func (g *AttributeGroup) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "id", &g.ID); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "name", &g.Name); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "groupReference", &g.GroupReference); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributes", &g.Attributes); err != nil {
		return err
	}
	if len(raw) > 0 {
		g.Extra = raw
	}
	return nil
}

func (g AttributeGroup) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, g.Extra)
	out["id"] = mustMarshal(g.ID)
	out["name"] = mustMarshal(g.Name)
	out["groupReference"] = mustMarshal(g.GroupReference)
	out["attributes"] = mustMarshal(g.Attributes)
	return json.Marshal(out)
}

func (d *DataExtraction) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "attributeGroups", &d.AttributeGroups); err != nil {
		return err
	}
	if len(raw) > 0 {
		d.Extra = raw
	}
	return nil
}

func (d DataExtraction) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, d.Extra)
	out["attributeGroups"] = mustMarshal(d.AttributeGroups)
	return json.Marshal(out)
}

func (c *CRTDLDocument) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "display", &c.Display); err != nil {
		return err
	}
	if err := unmarshalKnown(raw, "version", &c.Version); err != nil {
		return err
	}
	if v, ok := raw["cohortDefinition"]; ok {
		c.CohortDefinition = append(c.CohortDefinition[:0], v...)
		delete(raw, "cohortDefinition")
	}
	if err := unmarshalKnown(raw, "dataExtraction", &c.DataExtraction); err != nil {
		return err
	}
	if len(raw) > 0 {
		c.Extra = raw
	}
	return nil
}

func (c CRTDLDocument) MarshalJSON() ([]byte, error) {
	out := map[string]json.RawMessage{}
	mergeExtra(out, c.Extra)
	if c.Display != "" {
		out["display"] = mustMarshal(c.Display)
	}
	if c.Version != "" {
		out["version"] = mustMarshal(c.Version)
	}
	if len(c.CohortDefinition) > 0 {
		out["cohortDefinition"] = append(json.RawMessage(nil), c.CohortDefinition...)
	}
	out["dataExtraction"] = mustMarshal(c.DataExtraction)
	return json.Marshal(out)
}
