package ui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/medizininformatik-initiative/aether/internal/ui"
)

func TestRenderBar(t *testing.T) {
	assert.Equal(t, "[........]", ui.RenderBar(0, 8))
	assert.Equal(t, "[####....]", ui.RenderBar(0.5, 8))
	assert.Equal(t, "[########]", ui.RenderBar(1, 8))
}

func TestRenderBar_ClampsOutOfRange(t *testing.T) {
	assert.Equal(t, "[....]", ui.RenderBar(-0.5, 4))
	assert.Equal(t, "[####]", ui.RenderBar(1.5, 4))
}
