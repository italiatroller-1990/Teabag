// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/routers/web/repo"
	"gitea.dev/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The experimental frontend must never depend on a JSON API or on a bundler,
// and every hx-* attribute may only upgrade markup that already works on its
// own. These tests pin that down: the pages render from Go templates, the htmx
// endpoints are reachable as plain HTML, the assets are served without a
// frontend build, and nothing links to a URL the experiment does not serve.

const xRepo1Commit = "65f1bf27bc3bf70f64657658635e66094edbcb4d"

// xResponse is a response plus its parsed document, so a test can assert on
// the markup and on the parsed structure with the same request.
type xResponse struct {
	*httptest.ResponseRecorder
	body string
	doc  *goquery.Document
}

func (r *xResponse) HTML() string { return r.body }

// xGetStatus performs a browser-like request: browsers ask for HTML, and the
// experimental error pages are only rendered for them.
func xGetStatus(t *testing.T, urlStr string, expectedStatus int) *xResponse {
	t.Helper()
	recorder := MakeRequest(t, NewRequest(t, "GET", urlStr).SetHeader("Accept", "text/html"), expectedStatus)
	// the parser reads from the buffer, which would drain it
	body := recorder.Body.String()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	return &xResponse{ResponseRecorder: recorder, body: body, doc: doc}
}

func xGet(t *testing.T, urlStr string) *xResponse {
	t.Helper()
	return xGetStatus(t, urlStr, http.StatusOK)
}

func TestXHtmxHome(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	body := xGet(t, "/_x/").HTML()
	assert.Contains(t, body, `href="/_x/asset/app.css"`)
	assert.Contains(t, body, `src="/_x/asset/htmx.min.js"`)
	// htmx is configured through a meta tag so the page needs no inline script
	assert.Contains(t, body, `<meta name="htmx-config"`)
	assert.Contains(t, body, `href="/_x/explore/repos"`)
	// the skip link is the first focusable element and points at the main region
	assert.Contains(t, body, `<a class="x-skip-link" href="#x-main">`)
}

// TestXHtmxSignOut exercises the sign-out of the navbar instead of only
// looking at it: /user/logout is a GET route, so the experiment has to link to
// it. A POST form, which is what the navbar used to render, is a 404.
func TestXHtmxSignOut(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	page := session.MakeRequest(t, NewRequest(t, "GET", "/_x/").SetHeader("Accept", "text/html"), http.StatusOK)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page.Body.String()))
	require.NoError(t, err)
	require.Equal(t, 1, doc.Find("nav.x-navbar a[href='/user/logout']").Length(), "the navbar has to link to the production sign-out route")
	assert.Zero(t, doc.Find("nav.x-navbar form[action='/user/logout']").Length(),
		"/user/logout is a GET route: a POST form for it never signs out")

	// and following it really ends the session
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user/logout"), http.StatusSeeOther)
	assert.Equal(t, "/", resp.Header().Get("Location"))
	session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo2"), http.StatusNotFound)
}

func TestXHtmxExploreReposAndFragment(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	body := xGet(t, "/_x/explore/repos?q=TheKeyword").HTML()
	assert.Contains(t, body, `name="q" value="TheKeyword"`)
	// the search form works as a plain GET form; hx-get only adds the in-place update
	assert.Contains(t, body, `method="get" action="/_x/explore/repos"`)
	assert.Contains(t, body, `hx-get="/_x/explore/repos/list"`)
	// the list is already in the first response, so it works without JavaScript
	assert.Contains(t, body, `id="x-list"`)
	// the swap replaces the element instead of nesting a second one inside it
	assert.Contains(t, body, `hx-target="#x-list"`)
	assert.Contains(t, body, `hx-swap="outerHTML"`)

	// the HTMX endpoint returns the same fragment on its own
	fragment := xGet(t, "/_x/explore/repos/list?q=TheKeyword")
	assert.Equal(t, "text/html; charset=utf-8", fragment.Header().Get("Content-Type"))
	assert.Contains(t, fragment.HTML(), `id="x-list"`)
}

func TestXHtmxExploreUsers(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	assert.Contains(t, xGet(t, "/_x/explore/users").HTML(), `id="x-list"`)
	assert.Contains(t, xGet(t, "/_x/explore/users/list").HTML(), `id="x-list"`)
}

// TestXHtmxExploreUsersPageDisabled makes sure the fragment cannot be used to
// reach a page the instance disabled: a fragment request is a request of its
// own and has to enforce the setting itself.
func TestXHtmxExploreUsersPageDisabled(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.Explore.DisableUsersPage, true)()

	resp := xGetStatus(t, "/_x/explore/users", http.StatusSeeOther)
	assert.Equal(t, "/_x/explore/repos", resp.Header().Get("Location"))
	assert.Equal(t, http.StatusNotFound, xGetStatus(t, "/_x/explore/users/list", http.StatusNotFound).Code)
}

// TestXHtmxExplorePagination checks the pager, which is the one control whose
// broken form (a link to the classic frontend, a swap into an element that is
// not on the page) is invisible in a single-page smoke test.
func TestXHtmxExplorePagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.UI.ExplorePagingNum, 1)()

	body := xGet(t, "/_x/explore/repos").HTML()
	assert.Contains(t, body, `rel="next"`)
	assert.Contains(t, body, `href="/_x/explore/repos?page=2"`)
	// the same link, upgraded to a fragment swap of the element it is for
	assert.Contains(t, body, `hx-get="/_x/explore/repos/list?page=2"`)
	assert.Contains(t, body, `hx-target="#x-list"`)
	// the pager has a stable id so the fragment can re-render it too
	assert.Contains(t, body, `id="x-pager"`)
	// and the fragment really serves page 2, with a pager that says so
	fragment := xGet(t, "/_x/explore/repos/list?page=2").HTML()
	assert.Contains(t, fragment, `id="x-list"`)
	assert.Contains(t, fragment, `hx-swap-oob="outerHTML"`)
	assert.Contains(t, fragment, `Page 2 of`)
}

func TestXHtmxRepoHomeAndTree(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	body := xGet(t, "/_x/user2/repo1").HTML()
	assert.Contains(t, body, `user2 / repo1`)
	// directory listing, breadcrumb and the filter form are all server-rendered
	assert.Contains(t, body, `id="x-file-list"`)
	assert.Contains(t, body, `name="filter"`)
	// the filter is a plain GET form; the fragment endpoint upgrades it
	assert.Contains(t, body, `method="get" action="/_x/user2/repo1/src/branch/master"`)
	assert.Contains(t, body, `hx-get="/_x/user2/repo1/files/branch/master"`)
	assert.NotContains(t, body, `hx-select`)
	// the sidebar tree loads its first level on load and the rest on expand
	assert.Contains(t, body, `hx-get="/_x/user2/repo1/tree-nodes/branch/master"`)

	// the tree fragment endpoint returns HTML, not JSON
	fragment := xGet(t, "/_x/user2/repo1/tree-nodes/branch/master").HTML()
	assert.NotContains(t, fragment, `{"`)
	assert.Contains(t, fragment, `<ul class="x-tree"`)
	assert.Contains(t, fragment, `href="/_x/user2/repo1/src/branch/master/README.md"`)
}

// TestXHtmxFileFilterStaysInTheDirectory covers the filter form of a
// subdirectory: its HTMX URL used to be built from the repository root, so
// typing in a filter jumped out of the directory that was on screen.
func TestXHtmxFileFilterStaysInTheDirectory(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	page := xGet(t, "/_x/user2/glob/src/branch/master/x")
	form := page.doc.Find("form.x-toolbar")
	require.Equal(t, 1, form.Length())
	// both URLs of the form point at the directory that is on screen
	assert.Equal(t, "/_x/user2/glob/src/branch/master/x", form.AttrOr("action", ""))
	assert.Equal(t, "/_x/user2/glob/files/branch/master/x", form.AttrOr("hx-get", ""))

	// the fragment the filter asks for is the listing of that same directory
	fragment := xGet(t, "/_x/user2/glob/files/branch/master/x?filter=y")
	require.Equal(t, http.StatusOK, fragment.Code)
	assert.Equal(t, 1, fragment.doc.Find("a.x-tree-name[href$='/x/y']").Length())
	assert.Equal(t, 0, fragment.doc.Find("a.x-tree-name[href$='/x/b.txt']").Length())

	// the no-JavaScript path filters the same directory
	rendered := xGet(t, "/_x/user2/glob/src/branch/master/x?filter=y")
	assert.Equal(t, 1, rendered.doc.Find("#x-file-list a.x-tree-name[href$='/x/y']").Length())
	assert.Equal(t, 0, rendered.doc.Find("#x-file-list a.x-tree-name[href$='/x/b.txt']").Length())
}

// TestXHtmxTreeNodeTargetsAreUniqueAndResolvable covers the sidebar bug where
// every directory level reused the same swap target: the first one in the
// document wins, so expanding a nested folder filled the wrong subtree. It
// also covers the links being absolute, which they only are when the tree is
// rooted below the repository root.
func TestXHtmxTreeNodeTargetsAreUniqueAndResolvable(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	root := xGet(t, "/_x/user2/glob/tree-nodes/branch/master")
	dir := root.doc.Find("button.x-tree-toggle").First()
	require.Equal(t, 1, dir.Length())
	childrenURL, ok := dir.Attr("hx-get")
	require.True(t, ok)
	target, ok := dir.Attr("hx-target")
	require.True(t, ok)
	require.Equal(t, 1, root.doc.Find(target).Length(), "the swap target must exist in the same fragment")

	// expanding must reach the same repository path, not the tree root again
	level := xGet(t, childrenURL)
	require.Equal(t, http.StatusOK, level.Code)
	assert.Contains(t, level.HTML(), `href="/_x/user2/glob/src/branch/master/x/y"`)
	assert.Contains(t, level.HTML(), `hx-get="/_x/user2/glob/tree-nodes/branch/master/x/y"`)
	assert.NotContains(t, level.HTML(), "master/x/y/x/y")

	// a tree rooted in a subdirectory keeps absolute links, and its own targets
	// never collide with the ones already rendered above it
	rootIDs := map[string]bool{}
	root.doc.Find("[id]").Each(func(_ int, s *goquery.Selection) {
		if id, ok := s.Attr("id"); ok && strings.HasPrefix(id, "x-tree-") {
			rootIDs[id] = true
		}
	})
	level.doc.Find("[id]").Each(func(_ int, s *goquery.Selection) {
		if id, ok := s.Attr("id"); ok && strings.HasPrefix(id, "x-tree-") {
			assert.False(t, rootIDs[id], "tree target %q is rendered twice", id)
		}
	})
}

func TestXHtmxRepoFileAndBlame(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	file := xGet(t, "/_x/user2/repo1/src/branch/master/README.md")
	assert.Contains(t, file.HTML(), `README.md`)
	// the blame link of a file view stays on the ref being viewed, not on the
	// default branch, so a tag or a commit view links to its own blame page
	assert.Contains(t, file.HTML(), `href="/_x/user2/repo1/blame/branch/master/README.md"`)
	commitView := xGet(t, "/_x/user2/repo1/src/commit/"+xRepo1Commit+"/README.md")
	assert.Contains(t, commitView.HTML(), `href="/_x/user2/repo1/blame/commit/`+xRepo1Commit+`/README.md"`)

	blame := xGet(t, "/_x/user2/repo1/blame/branch/master/README.md")
	assert.Contains(t, blame.HTML(), `<table class="x-blame">`)
	// the commit cell shows the short sha, not the beginning of the commit URL
	assert.Contains(t, blame.HTML(), `href="/_x/user2/repo1/commit/`+xRepo1Commit+`"`)
	assert.NotContains(t, blame.HTML(), `<span class="mono"></span>`)
	// a row that continues a run has no commit of its own and must not link to
	// the repository root: an empty link is still a link
	assert.NotContains(t, blame.HTML(), `href="/_x"`)
	// the file name is part of the heading, and the breadcrumb ends on it
	assert.Contains(t, blame.doc.Find("h1.x-page-title").Text(), "README.md")
	assert.Equal(t, 1, blame.doc.Find(".x-breadcrumb [aria-current='page']").Length())
}

// TestXHtmxBlameWithoutPathMatchesClassic covers a request the classic UI
// answers with a 404 as well.
func TestXHtmxBlameWithoutPath(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	assert.Equal(t, http.StatusNotFound, xGetStatus(t, "/_x/user2/repo1/blame/branch/master", http.StatusNotFound).Code)
}

func TestXHtmxRepoCommits(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	body := xGet(t, "/_x/user2/repo1/commits/branch/master").HTML()
	assert.Contains(t, body, `id="x-list"`)
	assert.Contains(t, body, `href="/_x/user2/repo1/commit/`)
	// the pager is always real markup; with a single page it is simply disabled
	assert.Contains(t, body, `aria-label="Pagination"`)
	assert.Contains(t, body, `aria-disabled="true"`)
}

// TestXHtmxRepoCommitsPagination covers the two pager bugs of the commit list:
// a page count that truncated (7 commits in pages of 5 were "1" page) and a
// swap into the fragment of the list.
func TestXHtmxRepoCommitsPagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Git.CommitsRangeSize, 2)()

	body := xGet(t, "/_x/user2/commits_search_test/commits/branch/master").HTML()
	assert.Contains(t, body, `rel="next"`)
	// the page size is part of the link, so page 2 still shows two commits
	assert.Contains(t, body, `href="/_x/user2/commits_search_test/commits/branch/master?page=2&amp;limit=2"`)
	assert.Contains(t, body, `hx-get="/_x/user2/commits_search_test/commits/list/branch/master?page=2&amp;limit=2"`)
	// the page count rounds up: 5 commits in pages of 2 are 3 pages, not 2
	assert.Contains(t, body, `Page 1 of 3`)
	assert.Contains(t, body, `id="x-pager"`)
	assert.Equal(t, 2, xGet(t, "/_x/user2/commits_search_test/commits/branch/master").doc.Find("#x-list li").Length())
	fragment := xGet(t, "/_x/user2/commits_search_test/commits/list/branch/master?page=2")
	assert.Equal(t, http.StatusOK, fragment.Code)
	assert.Contains(t, fragment.HTML(), `id="x-list"`)
	// the fragment re-renders the pager out-of-band, so it cannot stay stale
	assert.Contains(t, fragment.HTML(), `hx-swap-oob="outerHTML"`)
	assert.Contains(t, fragment.HTML(), `Page 2 of 3`)
	assert.NotContains(t, fragment.HTML(), "<html")

	// a page size a visitor asks for is honoured up to the site-wide maximum,
	// and never above it: the list is a page, not the whole history
	one := xGet(t, "/_x/user2/commits_search_test/commits/branch/master?limit=1")
	assert.Equal(t, 1, one.doc.Find("#x-list li").Length())
	assert.Contains(t, one.HTML(), `href="/_x/user2/commits_search_test/commits/branch/master?page=2&amp;limit=1"`)
	tooBig := xGet(t, "/_x/user2/commits_search_test/commits/branch/master?limit=10000")
	assert.Equal(t, 2, tooBig.doc.Find("#x-list li").Length())
}

// TestXHtmxRepoCommit is the page the commit list and the directory listing
// link to. Without it every one of those links was a 404.
func TestXHtmxRepoCommit(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := xGet(t, "/_x/user2/repo1/commit/"+xRepo1Commit)
	assert.Contains(t, resp.HTML(), xRepo1Commit)
	assert.Contains(t, resp.HTML(), `href="/_x/user2/repo1/src/commit/`+xRepo1Commit+`/README.md"`)
	assert.Contains(t, resp.doc.Find(".x-commit-meta").Text(), "Author")

	// the changed files link into the experiment, and the full diff is labelled
	// as living in the classic UI instead of pretending it is here
	assert.Equal(t, 1, resp.doc.Find(`a.x-tree-name[href^="/_x/user2/repo1/src/commit/"]`).Length())
	assert.Equal(t, "/user2/repo1/commit/"+xRepo1Commit, resp.doc.Find(`a.x-btn[href^="/user2/repo1/commit/"]`).First().AttrOr("href", ""))

	// a commit that does not exist is a 404, not a server error
	assert.Equal(t, http.StatusNotFound, xGetStatus(t, "/_x/user2/repo1/commit/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", http.StatusNotFound).Code)
}

func TestXHtmxRepoIssues(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	body := xGet(t, "/_x/user2/repo1/issues").HTML()
	assert.Contains(t, body, `id="x-issue-list"`)
	assert.Contains(t, body, `hx-get="/_x/user2/repo1/issues/list"`)
	assert.Contains(t, body, `hx-target="#x-issue-list"`)
	// the filters are plain query parameters, so they are shareable URLs
	assert.Contains(t, body, `name="state"`)
	assert.Contains(t, body, `name="assignee"`)
	assert.Contains(t, body, `name="poster"`)
	assert.Contains(t, body, `name="sort"`)
	// ... and they keep their value, which the poster filter used to lose
	assert.Contains(t, xGet(t, "/_x/user2/repo1/issues?poster=user2").HTML(), `id="x-poster" name="poster" value="user2"`)

	fragment := xGet(t, "/_x/user2/repo1/issues/list?state=closed")
	assert.Equal(t, http.StatusOK, fragment.Code)
	assert.Contains(t, fragment.HTML(), `id="x-issue-list"`)
	assert.NotContains(t, fragment.HTML(), "<html")
}

// TestXHtmxRepoIssueLabelFilterKeepsTheFilters covers the label links, which
// are built from the bare request path and used to drop every other filter:
// clicking a label from a filtered list started a new one.
func TestXHtmxRepoIssueLabelFilterKeepsTheFilters(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	page := xGet(t, "/_x/user2/repo1/issues?state=closed&q=issue&poster=user2")
	labels := page.doc.Find("nav.x-label-list a.x-label")
	require.Equal(t, 2, labels.Length(), "repo1 has two labels")

	href, ok := labels.First().Attr("href")
	require.True(t, ok)
	parsed, err := url.Parse(href)
	require.NoError(t, err)
	query := parsed.Query()
	// the link is a full page of the same repository
	assert.Equal(t, "/_x/user2/repo1/issues", parsed.Path)
	// the filters that were active stay active, and the label is added to them
	assert.Equal(t, "closed", query.Get("state"))
	assert.Equal(t, "issue", query.Get("q"))
	assert.Equal(t, "user2", query.Get("poster"))
	assert.Equal(t, "1", query.Get("labels"))

	// a label of an already filtered selection toggles into the same query
	// instead of replacing it
	secondHref, ok := xGet(t, "/_x/user2/repo1/issues?state=closed&labels=1").doc.
		Find("nav.x-label-list a.x-label[href*='labels=1%2C2'], nav.x-label-list a.x-label[href*='labels=1,2']").Attr("href")
	require.True(t, ok, "the link of label2 has to keep the selected label1")
	second, err := url.Parse(secondHref)
	require.NoError(t, err)
	assert.Equal(t, "closed", second.Query().Get("state"))
	assert.Equal(t, "1,2", second.Query().Get("labels"))

	// ... and clearing the labels keeps the rest of the query
	clearHref, ok := page.doc.Find("nav.x-label-list a.x-btn").Attr("href")
	require.True(t, ok)
	cleared, err := url.Parse(clearHref)
	require.NoError(t, err)
	assert.Equal(t, "closed", cleared.Query().Get("state"))
	assert.Empty(t, cleared.Query().Get("labels"))
}

// TestXHtmxRepoIssuesPagination covers the pager of the issue list, whose swap
// target was the one the list page does not render.
func TestXHtmxRepoIssuesPagination(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.UI.IssuePagingNum, 1)()

	body := xGet(t, "/_x/user2/repo1/issues").HTML()
	assert.Contains(t, body, `rel="next"`)
	assert.Contains(t, body, `href="/_x/user2/repo1/issues?page=2"`)
	assert.Contains(t, body, `hx-get="/_x/user2/repo1/issues/list?page=2"`)
	assert.Contains(t, body, `hx-target="#x-issue-list"`)
	assert.Contains(t, body, `id="x-pager"`)
	// the fragment carries the pager for the page it swapped in, out-of-band
	fragment := xGet(t, "/_x/user2/repo1/issues/list?page=2").HTML()
	assert.Contains(t, fragment, `hx-swap-oob="outerHTML"`)
	assert.Contains(t, fragment, `Page 2 of`)
}

// TestXHtmxRepoIssue is the page the issue list links to, which used to be a
// 404 for every issue of every repository.
func TestXHtmxRepoIssue(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := xGet(t, "/_x/user2/repo1/issues/1")
	require.Equal(t, 1, resp.doc.Find("h1.x-page-title").Length())
	assert.Contains(t, resp.doc.Find("h1.x-page-title").Text(), "issue1")
	// writing stays in the classic UI, and the link says so
	assert.Equal(t, 1, resp.doc.Find(`a[href="/user2/repo1/issues/1"]`).Length())

	assert.Equal(t, http.StatusNotFound, xGetStatus(t, "/_x/user2/repo1/issues/9999", http.StatusNotFound).Code)

	// repo1's second issue is a pull request, and the experiment renders
	// issues only: as in the production code, it is redirected to its own view
	// instead of being shown as an issue
	pr := xGetStatus(t, "/_x/user2/repo1/issues/2", http.StatusSeeOther)
	assert.Equal(t, "/user2/repo1/pulls/2", pr.Header().Get("Location"))
}

func TestXHtmxRepoFileListFragment(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// the fragment renders the same links as the page it is a fragment of
	full := xGet(t, "/_x/user2/glob/src/branch/master/x").doc.Find("#x-file-list")
	fragment := xGet(t, "/_x/user2/glob/files/branch/master/x")
	require.Equal(t, http.StatusOK, fragment.Code)
	assert.Equal(t, 1, fragment.doc.Find("#x-file-list").Length())
	assert.Equal(t, full.Find("a.x-tree-name").First().AttrOr("href", ""),
		fragment.doc.Find("a.x-tree-name").First().AttrOr("href", ""))
	// ... and it links inside the experiment, which it did not before the
	// fragment was given the same preparation as the page
	assert.True(t, strings.HasPrefix(fragment.doc.Find("a.x-tree-name").First().AttrOr("href", ""), "/_x/user2/glob/"))

	// the filter narrows the listing, server-side
	filtered := xGet(t, "/_x/user2/glob/files/branch/master/x?filter=y")
	assert.Equal(t, 1, filtered.doc.Find("a.x-tree-name[href$='/x/y']").Length())
	assert.Equal(t, 0, filtered.doc.Find("a.x-tree-name[href$='/x/b.txt']").Length())

	// a file is not a directory: the fragment of a file view is a 404
	assert.Equal(t, http.StatusNotFound, xGetStatus(t, "/_x/user2/glob/files/branch/master/x/b.txt", http.StatusNotFound).Code)
}

func TestXHtmxAssetsAreServed(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	for name, contentType := range map[string]string{
		"app.css":     "text/css; charset=utf-8",
		"app.js":      "text/javascript; charset=utf-8",
		"htmx.min.js": "text/javascript; charset=utf-8",
	} {
		resp := xGet(t, "/_x/asset/"+name)
		assert.Equal(t, contentType, resp.Header().Get("Content-Type"))
		assert.NotEmpty(t, resp.HTML())
	}
}

func TestXHtmxAssetsRejectTraversal(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	xGetStatus(t, "/_x/asset/../app.js", http.StatusNotFound)
	xGetStatus(t, "/_x/asset/htmxui.go", http.StatusNotFound)
}

// TestXHtmxErrorsRenderTheExperimentalPage keeps a failure inside /_x from
// falling back to the classic layout, which drags in the whole JavaScript
// bundle of a frontend the visitor never asked for.
func TestXHtmxErrorsRenderTheExperimentalPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := xGetStatus(t, "/_x/user2/repo1/src/branch/master/does-not-exist", http.StatusNotFound)
	assert.Contains(t, resp.HTML(), `class="x-card x-card-body x-error"`)
	assert.Contains(t, resp.HTML(), "Page Not Found")
	assert.Contains(t, resp.HTML(), `href="/_x/"`)
	// the classic layout would load the classic bundle
	assert.NotContains(t, resp.HTML(), "/assets/js/")

	// an unknown repository is a 404 too, not a panic
	xGetStatus(t, "/_x/no-such-owner/no-such-repo", http.StatusNotFound)
	// ... and so is a URL that matches no experiment route at all
	unknown := xGetStatus(t, "/_x/user2/repo1/no-such-page", http.StatusNotFound)
	assert.Contains(t, unknown.HTML(), `class="x-card x-card-body x-error"`)

	// the request path is matched against the mount point, which the router has
	// already stripped of the instance sub-path: comparing it against the
	// sub-path-carrying base made every unknown URL fall back to the classic
	// page under a ROOT_URL with a path
	defer test.MockVariableValue(&setting.AppSubURL, "/sub")()
	sub := xGetStatus(t, "/_x/user2/repo1/no-such-page", http.StatusNotFound)
	assert.Contains(t, sub.HTML(), `class="x-card x-card-body x-error"`)
	assert.NotContains(t, sub.HTML(), "/assets/js/")
}

// TestXHtmxRedirectsStayInsideTheExperiment covers the redirects the shared
// repository assignment issues: a renamed repository or a renamed user used to
// send the visitor back to the classic frontend.
func TestXHtmxRedirectsStayInsideTheExperiment(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := xGetStatus(t, "/_x/user2/oldrepo1", http.StatusMovedPermanently)
	assert.Equal(t, "/_x/user2/repo1", repo.Header().Get("Location"))
	user := xGetStatus(t, "/_x/olduser2/repo1", http.StatusTemporaryRedirect)
	assert.Equal(t, "/_x/user2/repo1", user.Header().Get("Location"))
}

// TestXHtmxPermissionsMatchClassic compares the experiment with the production
// pages it mirrors: a private repository is a 404 for an anonymous visitor in
// both, and a repository without a code unit is a 404 in both.
func TestXHtmxPermissionsMatchClassic(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// a private repository is hidden from anonymous visitors in both
	xGetStatus(t, "/user2/private_repo", http.StatusNotFound)
	xGetStatus(t, "/_x/user2/private_repo", http.StatusNotFound)
	// so is a repository without a code unit
	xGetStatus(t, "/org26/repo_external_tracker", http.StatusSeeOther)
	xGetStatus(t, "/_x/org26/repo_external_tracker", http.StatusSeeOther)
}

// TestXHtmxReadOnly makes sure no experiment route accepts a write: the
// experiment renders, the production handlers write.
func TestXHtmxReadOnly(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	resp := MakeRequest(t, NewRequest(t, "POST", "/_x/user2/repo1/issues"), http.StatusMethodNotAllowed)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.Code)
}

// TestXHtmxEveryLinkResolves is the regression test for the whole class of
// "the page renders, but half of it is a 404": it walks every /_x link the
// experiment renders, including the ones inside its HTMX fragments, and
// requests it.
func TestXHtmxEveryLinkResolves(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	seeds := []string{
		"/_x/",
		"/_x/explore/repos",
		"/_x/explore/users",
		"/_x/user2/glob",
		"/_x/user2/glob/src/branch/master",
		"/_x/user2/glob/src/branch/master/x",
		"/_x/user2/glob/src/branch/master/x/y/a.txt",
		"/_x/user2/glob/src/tag/v1.1",
		"/_x/user2/glob/src/commit/" + xRepo1Commit,
		"/_x/user2/glob/commits/branch/master",
		"/_x/user2/commits_search_test/commits/branch/master",
		"/_x/user2/commits_search_test/commit/9800fe78cabf4fe774fcf376f97fa2a0ed06987b",
		"/_x/user2/commits_search_test/commit/58e97d1a24fb9e1599d8a467ec409430f3d3569e",
		"/_x/user2/glob/blame/branch/master/x/b.txt", "/_x/user2/glob/issues",
		"/_x/user2/glob/issues/1",
		"/_x/user2/repo1/commit/" + xRepo1Commit,
		"/_x/user2/repo1/issues/1",
		"/_x/user2/repo1/issues/5",
	}
	fragments := []string{
		"/_x/explore/repos/list",
		"/_x/explore/users/list",
		"/_x/user2/glob/tree-nodes/branch/master",
		"/_x/user2/glob/tree-nodes/branch/master/x",
		"/_x/user2/glob/tree-nodes/branch/master/x/y",
		"/_x/user2/glob/files/branch/master",
		"/_x/user2/glob/files/branch/master/x",
		"/_x/user2/glob/commits/list/branch/master",
		"/_x/user2/glob/issues/list",
		"/_x/user2/repo1/files/branch/master",
		"/_x/user2/repo1/commits/list/branch/master",
		"/_x/user2/commits_search_test/commits/list/branch/master",
	}

	seen := map[string]struct{}{}
	check := func(t *testing.T, from string, doc *goquery.Document) {
		doc.Find("a[href], form[action], [hx-get], [hx-post], [hx-target], img[src]").Each(func(_ int, s *goquery.Selection) {
			for _, attr := range []string{"href", "action", "hx-get", "hx-post", "src"} {
				value, ok := s.Attr(attr)
				if !ok || !strings.HasPrefix(value, "/_x") {
					continue
				}
				if _, duplicate := seen[value]; duplicate {
					continue
				}
				seen[value] = struct{}{}
				resp := xGetStatus(t, value, NoExpectedStatus)
				if resp.Code >= 400 {
					// the fixture instance has repositories whose git data is
					// missing; a link only counts as broken when the production
					// frontend cannot serve the same repository either
					classic := xGetStatus(t, strings.Replace(value, repo.XBasePath(), "", 1), NoExpectedStatus)
					assert.Equal(t, classic.Code, resp.Code, "%s links to %s", from, value)
				}
			}
			// every swap target has to exist on the page that triggers it
			if target, ok := s.Attr("hx-target"); ok {
				assert.Equal(t, 1, doc.Find(target).Length(),
					"%s swaps into %s, which the page does not render", from, target)
			}
		})
	}
	for _, seed := range seeds {
		resp := xGetStatus(t, seed, NoExpectedStatus)
		if resp.Code != http.StatusOK {
			// the explore lists point at every repository of the instance, and
			// the fixture instance has repositories without data on disk; they
			// are not links the experiment generates wrongly
			t.Logf("seed %s => %d, links not followed", seed, resp.Code)
			continue
		}
		check(t, seed, resp.doc)
	}
	for _, fragment := range fragments {
		resp := xGetStatus(t, fragment, NoExpectedStatus)
		assert.Equal(t, http.StatusOK, resp.Code, "fragment %s", fragment)
		if resp.Code == http.StatusOK {
			check(t, fragment, resp.doc)
		}
	}
}

// TestXHtmxNoJSONEndpoints documents the property the experiment is built on:
// the fragments the HTMX attributes ask for are HTML fragments, not JSON.
func TestXHtmxNoJSONEndpoints(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	for _, fragment := range []string{
		"/_x/user2/glob/tree-nodes/branch/master",
		"/_x/user2/glob/files/branch/master",
		"/_x/user2/glob/commits/list/branch/master",
		"/_x/user2/glob/issues/list",
		"/_x/explore/repos/list",
		"/_x/explore/users/list",
	} {
		resp := xGet(t, fragment)
		assert.NotContains(t, resp.HTML(), `{"`, "%s must not answer with JSON", fragment)
		assert.NotContains(t, resp.HTML(), "<html", "%s must be a fragment, not a page", fragment)
	}
}

// TestXHtmxSubURLKeepsLinksInsideTheInstance makes sure the experiment is not
// only reachable at the domain root: every link it builds has to carry the
// instance sub-path.
func TestXHtmxSubURLKeepsLinksInsideTheInstance(t *testing.T) {
	assert.Equal(t, "/_x", repo.XBasePath())
	defer test.MockVariableValue(&setting.AppSubURL, "/sub")()
	assert.Equal(t, "/sub/_x", repo.XBasePath())
}

// TestXHtmxSubURLBuildsEveryRepositoryURL renders the pages under a non-root
// AppSubURL and compares the URLs they link to with the one invariant the
// experiment has: AppSubURL + /_x + repository-relative path. The instance
// sub-path is easy to lose (a link built from the production repo link) and
// easy to double (a link built from a production link that already carries it),
// and neither shows up at the domain root.
func TestXHtmxSubURLBuildsEveryRepositoryURL(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.AppSubURL, "/sub")()

	const base = "/sub/_x"
	// every URL of the experiment starts with the instance sub-path exactly once
	for _, pageURL := range []string{
		"/_x/", "/_x/explore/repos", "/_x/explore/users",
		"/_x/user2/repo1", "/_x/user2/glob/src/branch/master/x",
		"/_x/user2/glob/tree-nodes/branch/master", "/_x/user2/glob/files/branch/master/x",
		"/_x/user2/repo1/issues?state=closed", "/_x/user2/commits_search_test/commits/branch/master?limit=1",
		"/_x/user2/repo1/commit/" + xRepo1Commit,
	} {
		page := xGet(t, pageURL)
		page.doc.Find("a[href], form[action], [hx-get], img[src]").Each(func(_ int, s *goquery.Selection) {
			for _, attr := range []string{"href", "action", "hx-get", "src"} {
				value, ok := s.Attr(attr)
				if !ok || !strings.Contains(value, "/_x") {
					continue
				}
				rest, found := strings.CutPrefix(value, base)
				assert.True(t, found && (rest == "" || strings.ContainsRune("/?&", rune(rest[0]))),
					"%s %s=%q is not under %s", pageURL, attr, value, base)
				// the sub-path once more right after the mount point is what a
				// link built by concatenating a production repo link looks like
				assert.False(t, rest == "/sub" || strings.HasPrefix(rest, "/sub/"),
					"%s %s=%q repeats the instance sub-path", pageURL, attr, value)
			}
		})
	}

	// the URLs themselves, page by page
	home := xGet(t, "/_x/user2/repo1")
	assert.Contains(t, home.HTML(), `href="`+base+`/user2/repo1/src/branch/master"`)
	assert.Contains(t, home.HTML(), `hx-get="`+base+`/user2/repo1/files/branch/master"`)
	assert.Contains(t, home.HTML(), `hx-get="`+base+`/user2/repo1/tree-nodes/branch/master"`)

	dir := xGet(t, "/_x/user2/glob/src/branch/master/x")
	assert.Contains(t, dir.HTML(), `action="`+base+`/user2/glob/src/branch/master/x"`)
	assert.Contains(t, dir.HTML(), `hx-get="`+base+`/user2/glob/files/branch/master/x"`)
	assert.Contains(t, dir.HTML(), `href="`+base+`/user2/glob/src/branch/master/x/y"`)
	assert.Contains(t, dir.HTML(), `href="`+base+`/user2/glob/commits/branch/master/x"`)

	tree := xGet(t, "/_x/user2/glob/tree-nodes/branch/master")
	assert.Contains(t, tree.HTML(), `href="`+base+`/user2/glob/src/branch/master/x"`)
	assert.Contains(t, tree.HTML(), `hx-get="`+base+`/user2/glob/tree-nodes/branch/master/x"`)

	files := xGet(t, "/_x/user2/glob/files/branch/master/x")
	assert.Equal(t, 1, files.doc.Find("a.x-tree-name[href='"+base+"/user2/glob/src/branch/master/x/y']").Length())

	issues := xGet(t, "/_x/user2/repo1/issues?state=closed")
	assert.Contains(t, issues.HTML(), `hx-get="`+base+`/user2/repo1/issues/list"`)
	assert.Contains(t, issues.HTML(), `href="`+base+`/user2/repo1/issues?state=closed&amp;labels=`)

	commits := xGet(t, "/_x/user2/commits_search_test/commits/branch/master?limit=1")
	assert.Contains(t, commits.HTML(), `href="`+base+`/user2/commits_search_test/commits/branch/master?page=2&amp;limit=1"`)
	assert.Contains(t, commits.HTML(), `hx-get="`+base+`/user2/commits_search_test/commits/list/branch/master?page=2&amp;limit=1"`)
	assert.Contains(t, commits.HTML(), `href="`+base+`/user2/commits_search_test/commit/`)

	commit := xGet(t, "/_x/user2/repo1/commit/"+xRepo1Commit)
	assert.Contains(t, commit.HTML(), `href="`+base+`/user2/repo1/src/commit/`+xRepo1Commit+`/README.md"`)
	// the diff itself is a classic page, so its link carries the sub-path once
	assert.Contains(t, commit.HTML(), `href="/sub/user2/repo1/commit/`+xRepo1Commit+`"`)

	// the navbar links out of the experiment, and those links carry it too
	session := loginUser(t, "user2")
	page := session.MakeRequest(t, NewRequest(t, "GET", "/_x/").SetHeader("Accept", "text/html"), http.StatusOK)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page.Body.String()))
	require.NoError(t, err)
	assert.Equal(t, "/sub/user/logout", doc.Find("nav.x-navbar a[href$='/user/logout']").AttrOr("href", ""))
	assert.Equal(t, 1, doc.Find("nav.x-navbar a[href$='/user/logout']").Length())
	assert.Equal(t, base+"/explore/repos", doc.Find("nav.x-navbar a[href$='/explore/repos']").AttrOr("href", ""))
}

func TestXHtmxURLEncoding(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// a filter value is echoed into the form, so it has to be escaped
	resp := xGetStatus(t, "/_x/user2/repo1/src/branch/master?filter="+url.QueryEscape("a b&c"), http.StatusOK)
	assert.Contains(t, resp.HTML(), `name="filter" value="a b&amp;c"`)
	// ... and a path segment with a slash cannot escape its directory
	xGetStatus(t, "/_x/user2/glob/tree-nodes/branch/master/../../../../etc/passwd", http.StatusNotFound)
}

// TestXHtmxRepoMigrating covers the repository home while an import is still
// running, which used to leak the classic page with its JavaScript progress
// widget into /_x.
func TestXHtmxRepoMigrating(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	migrating := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", Name: "repo1"})
	migrating.Status = repo_model.RepositoryBeingMigrated
	_, err := db.GetEngine(t.Context()).ID(migrating.ID).Cols("status").Update(migrating)
	require.NoError(t, err)

	page := xGet(t, "/_x/user2/repo1")
	require.Equal(t, 1, page.doc.Find("h1.x-page-title").Length())
	assert.NotContains(t, page.HTML(), `id="repo_migrating"`)
	// no migrating task exists for this repository, so the page reports failure
	assert.Contains(t, page.HTML(), "Migration failed.")
	// checking again is a plain link at the repository home: it re-renders this
	// page while the import runs and the real home afterwards
	assert.Equal(t, 1, page.doc.Find(`a.x-btn[href="/_x/user2/repo1/"]`).Length())
	// cancelling, retrying and deleting stay in the classic UI
	assert.Equal(t, 1, page.doc.Find(`a.x-btn[href="/user2/repo1"]`).Length())

	// a deeper path is redirected while the import runs, and the redirect
	// stays inside the experiment
	resp := xGetStatus(t, "/_x/user2/repo1/files/branch/master", http.StatusSeeOther)
	assert.Equal(t, "/_x/user2/repo1", resp.Header().Get("Location"))
}
