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
)

// RegisterRoutes mounts the experimental frontend under /_x. The router it is
// given already carries the session/auth middleware, so the experiment inherits
// exactly the same authentication and permission handling as production routes.
func RegisterRoutes(m *web.Router, optSignIn func(*context.Context)) {
	m.Group("/_x", func() {
		m.Get("", optSignIn, Home)
		m.Get("/explore/repos", optSignIn, Repos)
		m.Get("/explore/repos/list", optSignIn, ReposList)
		m.Get("/explore/users", optSignIn, Users)
		m.Get("/explore/users/list", optSignIn, UsersList)

		repoRoutes(m, optSignIn)
	})
	// Assets live outside the group: they are immutable, public and cached
	// forever, so they should not pay for session and repository lookups.
	m.Methods("GET, HEAD", "/_x/asset/*", htmxui.AssetHandler())
}

func repoRoutes(m *web.Router, optSignIn func(*context.Context)) {
	readCode := context.RequireUnitReader(unit.TypeCode)
	readIssues := context.RequireUnitReader(unit.TypeIssues, unit.TypeExternalTracker)

	// The experimental frontend mirrors the production URL shape (src/branch/…,
	// commits/branch/…, blame/branch/…) so links stay recognisable and the
	// context middlewares can be reused unchanged.
	m.Group("/{username}/{reponame}", func() {
		m.Get("", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), readCode, repo.XHome)
		m.Get("/src/branch/*", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), readCode, repo.XHome)
		m.Get("/src/commit/*", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeCommit), readCode, repo.XHome)
		m.Get("/tree-nodes/branch/*", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), readCode, repo.XTreeNodes)
		m.Get("/commits/branch/*", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), readCode, repo.XCommits)
		m.Get("/blame/branch/*", optSignIn, context.RepoAssignment, context.RepoRefByType(git.RefTypeBranch), readCode, repo.XBlame)
		m.Get("/issues", optSignIn, context.RepoAssignment, readIssues, repo.XIssues)
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
	ctx.Data["Title"] = ctx.Locale.TrString("home")
	ctx.Data["XBase"] = repo.XBase
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
	ctx.Data["XBase"] = repo.XBase
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
	if setting.Service.Explore.DisableUsersPage {
		ctx.Redirect(setting.AppSubURL + "/_x/explore/repos")
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("explore")
	ctx.Data["XBase"] = repo.XBase
	xRenderUserSearch(ctx, tplUsers)
}

// UsersList is the HTMX fragment behind the user search box.
func UsersList(ctx *context.Context) {
	xRenderUserSearch(ctx, tplUsersList)
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
