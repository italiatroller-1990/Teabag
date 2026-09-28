// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package htmxui

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"time"

	"gitea.dev/modules/httpcache"
	"gitea.dev/modules/util"

	"github.com/go-chi/chi/v5"
)

// contentTypes is a tiny allow-list. Serving an unexpected content type for an
// embeddable asset would be a security problem, so nothing else is accepted.
var contentTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
	".svg": "image/svg+xml",
	".txt": "text/plain; charset=utf-8",
	".map": "application/json",
}

// AssetHandler serves the embedded experimental assets.
//
// It is meant to be mounted on a "/*" route; the wildcard is the asset name, so
// the same handler works under any prefix (e.g. "/_x/asset/*"). The embedded
// tree is immutable for a given binary, so the ETag is the asset name and no
// file watcher or modtime is involved.
func AssetHandler() http.HandlerFunc {
	fsys := Assets()
	// the zero time tells http.ServeContent to omit Last-Modified, which is
	// correct for embedded data that has no meaningful build time per asset
	modTime := time.Time{}
	return func(resp http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			resp.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		name := chi.URLParam(req, "*")
		contentType, ok := contentTypes[path.Ext(name)]
		// PathJoinRelX both cleans the path and rejects anything escaping the root
		if !ok || name != util.PathJoinRelX(name) {
			resp.WriteHeader(http.StatusNotFound)
			return
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			resp.WriteHeader(http.StatusNotFound)
			return
		}

		resp.Header().Set("Content-Type", contentType)
		resp.Header().Set("X-Content-Type-Options", "nosniff")
		resp.Header().Set("ETag", `"`+name+`"`)
		httpcache.SetCacheControlInHeader(resp.Header(), httpcache.CacheControlForPublicStatic())
		// ServeContent answers conditional requests (If-None-Match) with a 304
		http.ServeContent(resp, req, path.Base(name), modTime, bytes.NewReader(data))
	}
}
