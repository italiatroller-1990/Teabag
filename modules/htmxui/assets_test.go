// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package htmxui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// mount mimics how the router exposes a "/*" pattern to the handler.
func mount(pattern, asset string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	rctx := chi.NewRouteContext()
	if pattern != "" {
		rctx.URLParams.Add(pattern, asset)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestAssetHandler(t *testing.T) {
	handler := AssetHandler()

	t.Run("ServesCSS", func(t *testing.T) {
		resp := httptest.NewRecorder()
		handler(resp, mount("*", "app.css"))
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, "text/css; charset=utf-8", resp.Header().Get("Content-Type"))
		assert.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))
		assert.NotEmpty(t, resp.Body.Bytes())
	})

	t.Run("AnswersConditionalGet", func(t *testing.T) {
		req := mount("*", "app.css")
		req.Header.Set("If-None-Match", `"app.css"`)
		resp := httptest.NewRecorder()
		handler(resp, req)
		assert.Equal(t, http.StatusNotModified, resp.Code)
	})

	t.Run("RejectsPathTraversal", func(t *testing.T) {
		for _, name := range []string{"../htmxui.go", "%2e%2e/app.css", "../../etc/passwd", "/etc/passwd"} {
			resp := httptest.NewRecorder()
			handler(resp, mount("*", name))
			assert.Equal(t, http.StatusNotFound, resp.Code, "path %q must not be served", name)
		}
	})

	t.Run("RejectsUnknownExtension", func(t *testing.T) {
		resp := httptest.NewRecorder()
		handler(resp, mount("*", "../../go.mod"))
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("RejectsNonReadMethod", func(t *testing.T) {
		req := mount("*", "app.css")
		req.Method = http.MethodPost
		resp := httptest.NewRecorder()
		handler(resp, req)
		assert.Equal(t, http.StatusMethodNotAllowed, resp.Code)
	})
}
