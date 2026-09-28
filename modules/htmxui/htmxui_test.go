// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package htmxui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddedAssetsArePresent(t *testing.T) {
	for _, name := range []string{"app.css", "app.js", "htmx.min.js", "LICENSE.htmx.txt"} {
		data, err := Assets().Open(name)
		assert.NoError(t, err, "asset %q must be embedded", name)
		assert.NoError(t, data.Close())
	}
}

func TestIconSetLoads(t *testing.T) {
	names := IconNames()
	assert.NotEmpty(t, names)
	for _, name := range names {
		out := RenderHTML(name)
		assert.NotContains(t, string(out), "x-icon-missing", "icon %q must render", name)
		assert.Contains(t, string(out), "<svg", "icon %q must render an svg element", name)
	}
}

func TestRenderHTMLSizeAndClass(t *testing.T) {
	assert.Equal(t, `<svg class="svg x-repo" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true">`,
		string(RenderHTML("x-repo"))[:len(`<svg class="svg x-repo" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true">`)])

	sized := RenderHTML("x-repo", 32)
	assert.Contains(t, string(sized), `width="32"`)
	assert.Contains(t, string(sized), `height="32"`)
	assert.NotContains(t, string(sized), `width="16"`)

	classed := RenderHTML("x-repo", 16, "x-inline")
	assert.Contains(t, string(classed), `class="x-inline svg x-repo"`)
}

func TestRenderHTMLUnknownIcon(t *testing.T) {
	out := RenderHTML("x-does-not-exist")
	assert.Contains(t, string(out), "x-icon-missing")
	assert.Contains(t, string(out), "x-does-not-exist")
}

func TestRenderHTMLEmptyName(t *testing.T) {
	assert.Empty(t, string(RenderHTML("")))
}

func TestRenderHTMLIsCached(t *testing.T) {
	// the second call must come from the cache and still be correct
	first := RenderHTML("x-repo-starred", 24)
	second := RenderHTML("x-repo-starred", 24)
	assert.Equal(t, first, second)
}
