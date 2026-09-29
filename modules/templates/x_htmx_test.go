// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gitea.dev/modules/htmxui"
	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xIconRE matches {{xIcon "name"}} the same way tools/lint-templates-svg.ts
// matches {{svg "name"}}. The Node linter is the canonical check; this copy
// exists so the experimental frontend can be validated with Go alone, which is
// the whole point of the experiment.
var (
	xIconRE = regexp.MustCompile(`xIcon ["']([^"']+)["']`)
	// reHtmxTarget finds the fragment targets the markup swaps into
	reHtmxTarget = regexp.MustCompile(`hx-(?:target|select)="(#[a-zA-Z][-\w]*)"`)
	// reDictTarget finds the targets a template passes to the shared pager,
	// which builds its hx-target at render time
	reDictTarget = regexp.MustCompile(`"Target" "([a-zA-Z][-\w]*)"`)
	reElementID  = regexp.MustCompile(`\bid="([a-zA-Z][-\w]*)"`)
	// reTrKey finds the translation keys a template asks for
	reTrKey = regexp.MustCompile(`(?:ctx\.Locale\.Tr|ctx\.Tr)\s+"([^"]+)"`)
	// reInlineStyle finds style attributes, which belong in the stylesheet
	reInlineStyle = regexp.MustCompile(`style="[^"]*"`)
	// reScriptTag finds script elements; each of them has to load a file
	reScriptTag = regexp.MustCompile(`<script[^>]*>`)
	// reEventAttr finds inline event handlers, which a CSP without
	// 'unsafe-inline' blocks and which have no business in a template
	reEventAttr = regexp.MustCompile(`\son[a-z]+="`)
)

func xTemplateFiles(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(setting.StaticRootPath, "templates", "x")
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".tmpl" {
			return err
		}
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		files[path] = string(content)
		return nil
	}))
	require.NotEmpty(t, files)
	return files
}

func TestXHtmxTemplatesExist(t *testing.T) {
	setting.IsProd = true
	if err := PageRendererReload(); err != nil {
		t.Fatalf("template parse error: %v", err)
	}
	for _, name := range []string{
		"x/home", "x/explore/repos", "x/explore/repos_list", "x/explore/users", "x/explore/users_list",
		"x/repo/home", "x/repo/dir", "x/repo/view", "x/repo/file_list", "x/repo/tree_nodes",
		"x/repo/commits", "x/repo/commits_list", "x/repo/commits_fragment", "x/repo/commit", "x/repo/blame",
		"x/repo/issues", "x/repo/issue_list", "x/repo/issues_fragment", "x/repo/issue", "x/repo/empty", "x/repo/migrating", "x/base/error",
	} {
		assert.True(t, PageRenderer().tmplRenderer.Templates().HasTemplate(name), "experimental template %q is missing", name)
	}
}

func TestXHtmxIconsExist(t *testing.T) {
	known := htmxui.IconNames()
	for path, content := range xTemplateFiles(t) {
		for _, match := range xIconRE.FindAllSubmatch([]byte(content), -1) {
			name := string(match[1])
			assert.True(t, slices.Contains(known, name), "%s references unknown experimental icon %q", path, name)
		}
	}
}

// TestXHtmxTranslationKeysExist catches the class of bug where a page renders a
// raw locale key ("repo.issues.open") because the key it asks for is not in
// locale_en-US.json. Nothing else notices that: the page is still valid HTML.
func TestXHtmxTranslationKeysExist(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(setting.StaticRootPath, "options", "locale", "locale_en-US.json"))
	require.NoError(t, err)
	keys := map[string]string{}
	require.NoError(t, json.Unmarshal(raw, &keys))

	for path, content := range xTemplateFiles(t) {
		for _, match := range reTrKey.FindAllStringSubmatch(content, -1) {
			key := match[1]
			_, ok := keys[key]
			assert.True(t, ok, "%s asks for the missing translation key %q", path, key)
		}
	}
}

// TestXHtmxUnusedTranslationKeys keeps the experimental locale block from
// accumulating keys no template asks for any more.
func TestXHtmxUnusedTranslationKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(setting.StaticRootPath, "options", "locale", "locale_en-US.json"))
	require.NoError(t, err)
	keys := map[string]string{}
	require.NoError(t, json.Unmarshal(raw, &keys))

	used := map[string]struct{}{}
	for _, content := range xTemplateFiles(t) {
		for _, match := range reTrKey.FindAllStringSubmatch(content, -1) {
			used[match[1]] = struct{}{}
		}
	}
	for key := range keys {
		if strings.HasPrefix(key, "x.") {
			_, ok := used[key]
			assert.True(t, ok, "translation key %q is not used by any experimental template", key)
		}
	}
}

// TestXHtmxFragmentTargetsExist checks that every HTMX swap target is an
// element some template actually renders. A target that only exists on another
// page is the same bug as a missing one: the swap silently does nothing.
func TestXHtmxFragmentTargetsExist(t *testing.T) {
	files := xTemplateFiles(t)
	ids := map[string]string{}
	for path, content := range files {
		for _, match := range reElementID.FindAllStringSubmatch(content, -1) {
			ids[match[1]] = path
		}
	}
	for path, content := range files {
		targets := append(reHtmxTarget.FindAllStringSubmatch(content, -1), reDictTarget.FindAllStringSubmatch(content, -1)...)
		for _, match := range targets {
			id := strings.TrimPrefix(match[1], "#")
			_, ok := ids[id]
			assert.True(t, ok, "%s swaps into #%s, which no experimental template renders", path, id)
		}
	}
}

// TestXHtmxNoInlineScriptOrStyle keeps the pages CSP-clean and the styles in one
// place. The only allowed style attribute is a label colour, which is data
// taken from the database and therefore cannot live in the stylesheet.
func TestXHtmxNoInlineScriptOrStyle(t *testing.T) {
	for path, content := range xTemplateFiles(t) {
		for _, tag := range reScriptTag.FindAllString(content, -1) {
			assert.Contains(t, tag, "src=", "%s has a script without a src: %s", path, tag)
			assert.Contains(t, tag, "nonce=", "%s has a script without a CSP nonce: %s", path, tag)
		}
		assert.NotRegexp(t, reEventAttr, content, "%s has an inline event handler", path)
		assert.NotContains(t, content, "javascript:", "%s must not contain a javascript: URL", path)
		for _, match := range reInlineStyle.FindAllString(content, -1) {
			assert.Contains(t, match, "border-color:", "%s has an inline style that belongs in app.css: %s", path, match)
		}
	}
}
