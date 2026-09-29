// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package xhtmx is the routing and non-repository part of the experimental
// server-rendered frontend (branch: experiment/htmx-frontend).
//
// It exists to answer one question: can Teabag's pages be Go templates, HTMX,
// SVG and a little vanilla JavaScript, without the Vue/Vite/Tailwind build?
// Everything here is additive and mounted under /_x, so the production
// frontend keeps working unchanged while the experiment is measured.
package xhtmx

import (
	"net/http"
	"strings"

	"gitea.dev/models/db"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/container"
	"gitea.dev/modules/git"
	"gitea.dev/modules/htmxui"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/structs"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web"
	"gitea.dev/routers/web/explore"
	"gitea.dev/routers/web/repo"
	"gitea.dev/services/context"
)

const (
	tplHome templates.TplName = "x/home"
	// each list page has a matching "_list" fragment for HTMX; both render the
	// same partial, so the only difference is the surrounding layout
	tplRepos     templates.TplName = "x/explore/repos"
	tplReposList templates.TplName = "x/explore/repos_list"
	tplUsers     templates.TplName = "x/explore/users"
	tplUsersList templates.TplName = "x/explore/users_list"
	tplXError    templates.TplName = "x/base/error"
)

// RegisterRoutes mounts the experimental frontend under /_x. The router it is
// given already carries the session/auth middleware, so the experiment inherits
// exactly the same authentication and permission handling as production routes.
func RegisterRoutes(m *web.Router, optSignIn func(*context.Context)) {
	m.Group(repo.XBase, func() {
		m.Get("", optSignIn, Home)
		m.Get("/explore/repos", optSignIn, Repos)
		m.Get("/explore/repos/list", optSignIn, ReposList)
		m.Get("/explore/users", optSignIn, Users)
		m.Get("/explore/users/list", optSignIn, UsersList)

		repoRoutes(m, optSignIn)
	}, xFrontend)
	// Assets live outside the group: they are immutable, public and cached
	// forever, so they should not pay for session and repository lookups.
	m.Methods("GET, HEAD", repo.XBase+"/asset/*", htmxui.AssetHandler())
}

// xFrontend marks a request as one the experiment serves. The templates link
// through the experiment's own base, and the shared repository assignment needs
// the same information: an alternative frontend prefixes every URL it renders,
// so the repository home cannot be recognised by comparing links.
//
// It also makes the experiment render its own 404/500 pages, so an error inside
// /_x never falls back to the classic layout and its whole JS bundle. The
// template is claimed here too, because the failure can happen in a middleware
// before the handler had a chance to set it.
func xFrontend(ctx *context.Context) {
	ctx.Data[context.FrontendBasePathKey] = repo.XBasePath()
	ctx.Data[context.ErrorTplNameKey] = string(tplXError)
	ctx.Data["XBase"] = repo.XBasePath()
}

// MarkErrorPage is the not-found path equivalent of xFrontend: the global
// not-found handler matches no route at all, so the experiment has to claim
// its own URLs there too, or a mistyped /_x link would answer with a page of
// the classic frontend.
//
// The request path has already had the instance sub-path stripped by the
// router, so it is matched against XBase; XBasePath() is what the experiment
// renders links with, and comparing the two would miss every URL under a
// ROOT_URL with a path.
func MarkErrorPage(ctx *context.Context) {
	if !strings.HasPrefix(ctx.Req.URL.Path, repo.XBase+"/") && ctx.Req.URL.Path != repo.XBase {
		return
	}
	xFrontend(ctx)
}

// xRefRoutes are the ref-typed routes of a repository. They mirror the classic
// {branch,tag,commit} shape so a link produced for one ref kind never 404s on
// the others: a commit page renders a tree, and that tree expands folders.
//
// Every route repeats the same three middlewares, so they are built here
// instead of being spelled out eighteen times.
func xRefRoutes(m *web.Router, optSignIn, readCode func(*context.Context)) {
	for _, refType := range []git.RefType{git.RefTypeBranch, git.RefTypeTag, git.RefTypeCommit} {
		for _, page := range []struct {
			path    string
			handler func(*context.Context)
		}{
			{"src", repo.XHome},
			{"tree-nodes", repo.XTreeNodes},
			{"files", repo.XFileList},
			{"commits", repo.XCommits},
			{"commits/list", repo.XCommitsList},
			{"blame", repo.XBlame},
		} {
			m.Get("/"+page.path+"/"+string(refType)+"/*", optSignIn, context.RepoAssignment, context.RepoRefByType(refType), readCode, page.handler)
		}
	}
}

func repoRoutes(m *web.Router, optSignIn func(*context.Context)) {
	readCode := context.RequireUnitReader(unit.TypeCode)
	readIssues := context.RequireUnitReader(unit.TypeIssues, unit.TypeExternalTracker, unit.TypePullRequests)

	// The experimental frontend mirrors the production URL shape (src/branch/…,
	// commits/branch/…, blame/branch/…) so links stay recognisable and the
	// context middlewares can be reused unchanged.
	m.Group("/{username}/{reponame}", func() {
		// the repository root is the one code route that does not enforce the
		// code unit: the production handler redirects to the first unit a
		// visitor may read, and XHome calls the very same check
		m.Get("", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), repo.XHome)
		xRefRoutes(m, optSignIn, readCode)
		m.Get("/commit/{sha:([a-f0-9]{7,64})}", optSignIn, context.RepoAssignment, readCode, repo.XCommit)
		m.Get("/issues", optSignIn, context.RepoAssignment, readIssues, repo.XIssues)
		m.Get("/issues/list", optSignIn, context.RepoAssignment, readIssues, repo.XIssueList)
		m.Get("/issues/{index}", optSignIn, context.RepoAssignment, readIssues, repo.XViewIssue)
	}, reqRepoRead)
}

// reqRepoRead keeps the experimental repository pages read-only. Every route in
// the group is a GET today; this makes adding a write route a deliberate act,
// because writes have to go through the production handlers to keep their
// permission and cross-origin checks.
func reqRepoRead(ctx *context.Context) {
	if ctx.Req.Method != http.MethodGet && ctx.Req.Method != http.MethodHead {
		ctx.HTTPError(http.StatusMethodNotAllowed)
	}
}

// Home renders the experimental landing page. Signed-in users see their own
// repositories, anonymous visitors see the public ones; both use the same
// search path the explore page uses.
func Home(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("x.home")
	ctx.Data["XBase"] = repo.XBasePath()
	explore.RenderRepoSearch(ctx, &explore.RepoSearchOptions{
		PageSize:         setting.UI.ExplorePagingNum,
		OwnerID:          xHomeOwnerID(ctx),
		Private:          ctx.Doer != nil,
		TplName:          tplHome,
		OnlyShowRelevant: setting.UI.OnlyShowRelevantRepos,
	})
}

func xHomeOwnerID(ctx *context.Context) int64 {
	if ctx.Doer == nil || ctx.Doer.IsAdmin {
		return 0
	}
	return ctx.Doer.ID
}

// Repos renders the experimental repository search page.
func Repos(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("explore")
	ctx.Data["XBase"] = repo.XBasePath()
	ctx.Data["XCurrentPage"] = "repos"
	xRenderRepoSearch(ctx, tplRepos)
}

// ReposList is the HTMX fragment behind the search box: same query, rows only.
func ReposList(ctx *context.Context) {
	xRenderRepoSearch(ctx, tplReposList)
}

func xRenderRepoSearch(ctx *context.Context, tpl templates.TplName) {
	var ownerID int64
	if ctx.Doer != nil && !ctx.Doer.IsAdmin {
		ownerID = ctx.Doer.ID
	}
	explore.RenderRepoSearch(ctx, &explore.RepoSearchOptions{
		PageSize:         setting.UI.ExplorePagingNum,
		OwnerID:          ownerID,
		Private:          ctx.Doer != nil,
		TplName:          tpl,
		OnlyShowRelevant: setting.UI.OnlyShowRelevantRepos,
	})
}

// Users renders the experimental user search page.
func Users(ctx *context.Context) {
	if !usersPageEnabled() {
		ctx.Redirect(repo.XBasePath() + "/explore/repos")
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("explore")
	ctx.Data["XBase"] = repo.XBasePath()
	ctx.Data["XCurrentPage"] = "users"
	xRenderUserSearch(ctx, tplUsers)
}

// UsersList is the HTMX fragment behind the user search box.
func UsersList(ctx *context.Context) {
	// the fragment is a request of its own, so it has to enforce the setting
	// itself instead of relying on the full page having done it
	if !usersPageEnabled() {
		ctx.NotFound(nil)
		return
	}
	xRenderUserSearch(ctx, tplUsersList)
}

func usersPageEnabled() bool {
	return !setting.Service.Explore.DisableUsersPage
}

func xRenderUserSearch(ctx *context.Context, tpl templates.TplName) {
	supportedSortOrders := container.SetOf("newest", "oldest", "alphabetically", "reversealphabetically")
	explore.RenderUserSearch(ctx, user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Types:       []user_model.UserType{user_model.UserTypeIndividual},
		ListOptions: db.ListOptions{PageSize: setting.UI.ExplorePagingNum},
		IsActive:    optional.Some(true),
		Visible:     []structs.VisibleType{structs.VisibleTypePublic, structs.VisibleTypeLimited, structs.VisibleTypePrivate},
		OrderBy:     db.SearchOrderBy(util.IfZero(ctx.FormString("sort"), "newest")),

		SupportedSortOrders: supportedSortOrders,
	}, tpl)
}
