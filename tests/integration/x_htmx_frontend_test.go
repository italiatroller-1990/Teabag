// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
)

// The experimental frontend must never depend on a JSON API or on a bundler:
// these tests assert the pages render from Go templates, that the htmx
// attributes point at real HTML fragment endpoints, and that the embedded
// assets are served without a frontend build step.

func TestXHtmxHome(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/"), http.StatusOK)
	body := resp.Body.String()

	assert.Contains(t, body, `href="/_x/asset/app.css"`)
	assert.Contains(t, body, `src="/_x/asset/htmx.min.js"`)
	// htmx is configured through a meta tag so the page needs no inline script
	assert.Contains(t, body, `<meta name="htmx-config"`)
	assert.Contains(t, body, `href="/_x/explore/repos"`)
}

func TestXHtmxExploreReposAndFragment(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/explore/repos?q=TheKeyword"), http.StatusOK)
	body := resp.Body.String()
	assert.Contains(t, body, `name="q" value="TheKeyword"`)
	// the search form works as a plain GET form; hx-get only adds the in-place update
	assert.Contains(t, body, `method="get" action="/_x/explore/repos"`)
	assert.Contains(t, body, `hx-get="/_x/explore/repos/list"`)
	// the list is already in the first response, so it works without JavaScript
	assert.Contains(t, body, `id="x-list"`)

	// the HTMX endpoint returns the same fragment on its own
	fragment := MakeRequest(t, NewRequest(t, "GET", "/_x/explore/repos/list?q=TheKeyword"), http.StatusOK)
	assert.Equal(t, "text/html; charset=utf-8", fragment.Header().Get("Content-Type"))
	assert.Contains(t, fragment.Body.String(), `id="x-list"`)
}

func TestXHtmxExploreUsers(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/explore/users"), http.StatusOK)
	assert.Contains(t, resp.Body.String(), `id="x-list"`)
}

func TestXHtmxRepoHomeAndTree(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1"), http.StatusOK)
	body := resp.Body.String()
	assert.Contains(t, body, `user2 / repo1`)
	// directory listing, breadcrumb and the filter form are all server-rendered
	assert.Contains(t, body, `id="x-file-list"`)
	assert.Contains(t, body, `name="filter"`)
	// the filter is a plain GET form; hx-select keeps one handler for both paths
	assert.Contains(t, body, `method="get" action="/_x/user2/repo1/src/branch/master"`)
	assert.Contains(t, body, `hx-select="#x-file-list"`)
	// the sidebar tree loads its first level on load and the rest on expand
	assert.Contains(t, body, `hx-get="/_x/user2/repo1/tree-nodes/branch/master"`)

	// the tree fragment endpoint returns HTML, not JSON
	fragment := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1/tree-nodes/branch/master"), http.StatusOK)
	fragmentBody := fragment.Body.String()
	assert.NotContains(t, fragmentBody, `{"`)
	assert.Contains(t, fragmentBody, `<ul class="x-tree"`)
	assert.Contains(t, fragmentBody, `href="/_x/user2/repo1/src/branch/master/README.md"`)
}

func TestXHtmxRepoFileAndBlame(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	file := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1/src/branch/master/README.md"), http.StatusOK)
	assert.Contains(t, file.Body.String(), `README.md`)

	blame := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1/blame/branch/master/README.md"), http.StatusOK)
	assert.Contains(t, blame.Body.String(), `class="x-blame-row"`)
}

func TestXHtmxRepoCommits(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1/commits/branch/master"), http.StatusOK)
	body := resp.Body.String()
	assert.Contains(t, body, `id="x-list"`)
	assert.Contains(t, body, `href="/_x/user2/repo1/commit/`)
	// the pager is always real markup; with a single page it is simply disabled
	assert.Contains(t, body, `aria-label="Pagination"`)
	assert.Contains(t, body, `aria-disabled="true"`)
}

func TestXHtmxRepoIssues(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "GET", "/_x/user2/repo1/issues"), http.StatusOK)
	body := resp.Body.String()
	assert.Contains(t, body, `id="x-issue-list"`)
	assert.Contains(t, body, `hx-select="#x-issue-list"`)
}

func TestXHtmxAssetsAreServed(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	for name, contentType := range map[string]string{
		"app.css":     "text/css; charset=utf-8",
		"app.js":      "text/javascript; charset=utf-8",
		"htmx.min.js": "text/javascript; charset=utf-8",
	} {
		resp := MakeRequest(t, NewRequest(t, "GET", "/_x/asset/"+name), http.StatusOK)
		assert.Equal(t, contentType, resp.Header().Get("Content-Type"))
		assert.NotEmpty(t, resp.Body.String())
	}
}

func TestXHtmxAssetsRejectTraversal(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	MakeRequest(t, NewRequest(t, "GET", "/_x/asset/../app.js"), http.StatusNotFound)
	MakeRequest(t, NewRequest(t, "GET", "/_x/asset/htmxui.go"), http.StatusNotFound)
}
