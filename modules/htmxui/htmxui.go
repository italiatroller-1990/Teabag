// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package htmxui holds the assets of the experimental server-rendered frontend.
//
// Everything the experiment needs at runtime is embedded into the binary, so the
// experimental pages build and run without Node, pnpm, Vite, Tailwind or PostCSS.
// The only third-party runtime dependency is htmx (0BSD), vendored as-is.
package htmxui

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assetsFS embed.FS

// Assets returns the embedded asset tree, rooted at the "assets" directory.
func Assets() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("htmxui: assets directory is not embeddable: " + err.Error())
	}
	return sub
}
