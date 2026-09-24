package models

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/models"
)

var hasForEachCases = []struct {
	name          string
	forEach       string
	forEachOrNull string
	want          bool
}{
	{"neither set", "", "", false},
	{"forEach set", "component", "", true},
	{"forEachOrNull set", "", "component", true},
	{"both set", "component", "code", true},
}

func TestSelectClauseHasForEach(t *testing.T) {
	for _, tc := range hasForEachCases {
		t.Run(tc.name, func(t *testing.T) {
			sel := models.SelectClause{ForEach: tc.forEach, ForEachOrNull: tc.forEachOrNull}
			assert.Equal(t, tc.want, sel.HasForEach())
		})
	}
}

func TestViewDefSnippetHasForEach(t *testing.T) {
	for _, tc := range hasForEachCases {
		t.Run(tc.name, func(t *testing.T) {
			snippet := models.ViewDefSnippet{ForEach: tc.forEach, ForEachOrNull: tc.forEachOrNull}
			assert.Equal(t, tc.want, snippet.HasForEach())
		})
	}
}
