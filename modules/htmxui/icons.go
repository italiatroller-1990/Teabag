// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package htmxui

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"gitea.dev/modules/htmlutil"
)

const defaultIconSize = 16

var reIconClassAt = regexp.MustCompile(`\s+class\s*=\s*"`)

type iconCacheKey struct {
	name  string
	size  int
	class string
}

var (
	// icons holds the raw (normalized) markup of every embedded icon, keyed by name without extension.
	icons = map[string]string{}
	// iconCache memoizes the size/class rewrite, mirroring how modules/svg caches its output.
	iconCache      sync.Map
	iconMu         sync.Mutex
	iconCacheCount int
	iconCacheLimit = 4096
)

func init() {
	entries, err := fs.ReadDir(Assets(), "icons")
	if err != nil {
		panic("htmxui: cannot read embedded icons: " + err.Error())
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".svg") {
			continue
		}
		data, err := fs.ReadFile(Assets(), path.Join("icons", entry.Name()))
		if err != nil {
			panic("htmxui: cannot read embedded icon " + entry.Name() + ": " + err.Error())
		}
		icons[strings.TrimSuffix(entry.Name(), ".svg")] = normalizeIcon(data)
	}
}

// normalizeIcon prepares an icon file for inlining: it guarantees a class attribute.
// The width/height are left in place, since Render rewrites them per requested size.
func normalizeIcon(data []byte) string {
	head, rest, ok := bytes.Cut(bytes.TrimSpace(data), []byte(">"))
	if !ok || !bytes.HasPrefix(head, []byte("<svg")) {
		return string(data)
	}
	if !bytes.Contains(head, []byte(`class="`)) {
		head = append(bytes.TrimSpace(head), ` class="svg"`...)
	}
	return string(head) + ">" + string(rest)
}

// IconNames returns the sorted names of every embedded icon, for tests and tooling.
func IconNames() []string {
	names := make([]string, 0, len(icons))
	for name := range icons {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// RenderHTML renders an icon inline. Arguments: icon name, then the optional
// (size int, class string) pair, matching the convention used by {{svg}}.
func RenderHTML(name string, others ...any) template.HTML {
	rendered, _ := renderHTML(name, others...)
	return rendered
}

func renderHTML(name string, others ...any) (template.HTML, bool) {
	if name == "" {
		return "", false
	}
	size, class := htmlutil.ParseSizeAndClass(defaultIconSize, "", others...)
	raw, ok := icons[name]
	if !ok {
		return template.HTML(fmt.Sprintf(`<span class="x-icon-missing">%s</span>`, template.HTMLEscapeString(name))), false
	}

	key := iconCacheKey{name: name, size: size, class: class}
	if cached, ok := iconCache.Load(key); ok {
		return cached.(template.HTML), true //nolint:forcetypeassert // iconCache only ever holds template.HTML
	}

	// the sources are normalized, so plain replacement is enough and no XML re-parse is needed
	out := raw
	if size != defaultIconSize {
		out = strings.Replace(out, fmt.Sprintf(`width="%d"`, defaultIconSize), fmt.Sprintf(`width="%d"`, size), 1)
		out = strings.Replace(out, fmt.Sprintf(`height="%d"`, defaultIconSize), fmt.Sprintf(`height="%d"`, size), 1)
	}
	if class != "" {
		out = reIconClassAt.ReplaceAllString(out, ` class="`+class+` `)
	}
	rendered := template.HTML(out)

	iconMu.Lock()
	if iconCacheCount >= iconCacheLimit {
		iconCache.Clear()
		iconCacheCount = 0
	}
	iconCacheCount++
	iconCache.Store(key, rendered)
	iconMu.Unlock()
	return rendered, false
}

// IconSizeAttr renders a bare `width`/`height` attribute pair, for the rare
// places that need an icon as a CSS background instead of inline markup.
func IconSizeAttr(size int) template.HTMLAttr {
	if size <= 0 {
		size = defaultIconSize
	}
	return template.HTMLAttr(`width="` + strconv.Itoa(size) + `" height="` + strconv.Itoa(size) + `"`)
}
