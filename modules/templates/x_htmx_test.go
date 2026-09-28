// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"gitea.dev/modules/htmxui"
	"gitea.dev/modules/setting"
)

// xIconRE matches {{xIcon "name"}} the same way tools/lint-templates-svg.ts
// matches {{svg "name"}}. The Node linter is the canonical check; this copy
// exists so the experimental frontend can be validated with Go alone, which is
// the whole point of the experiment.
var xIconRE = regexp.MustCompile(`xIcon ["']([^"']+)["']`)

func TestXHtmxTemplatesExist(t *testing.T) {
	setting.IsProd = true
	if err := PageRendererReload(); err != nil {
		t.Fatalf("template parse error: %v", err)
	}
	for _, name := range []string{
		"x/home", "x/explore/repos", "x/explore/repos_list", "x/explore/users", "x/explore/users_list",
		"x/repo/home", "x/repo/dir", "x/repo/view", "x/repo/commits", "x/repo/blame", "x/repo/issues", "x/repo/empty",
	} {
		if !PageRenderer().tmplRenderer.Templates().HasTemplate(name) {
			t.Errorf("experimental template %q is missing", name)
		}
	}
}

func TestXHtmxIconsExist(t *testing.T) {
	known := htmxui.IconNames()
	err := filepath.WalkDir(filepath.Join(setting.StaticRootPath, "templates", "x"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".tmpl" {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range xIconRE.FindAllSubmatch(content, -1) {
			if name := string(match[1]); !slices.Contains(known, name) {
				t.Errorf("%s references unknown experimental icon %q", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan experimental templates: %v", err)
	}
}
