// Copyright 2026 The Teabag Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

// Experimental server-rendered frontend (branch: experiment/htmx-frontend).
//
// These handlers reuse the unexported preparation helpers of this package rather
// than re-implementing git access, so the experiment only replaces the
// presentation layer: the same data reaches a Go template instead of a Vue
// component. Removing this file plus the "xhtmx" route group removes the
// experiment; no other file in this package has to change.

import (
	"net/http"
	"path"
	"strings"

	auth_model "gitea.dev/models/auth"
	"gitea.dev/modules/base"
	"gitea.dev/modules/git"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/util"
	"gitea.dev/services/context"
)

const (
	tplXRepoHome  templates.TplName = "x/repo/home"
	tplXRepoDir   templates.TplName = "x/repo/dir"
	tplXRepoFile  templates.TplName = "x/repo/view"
	tplXTreeNodes templates.TplName = "x/repo/tree_nodes"
	tplXCommits   templates.TplName = "x/repo/commits"
	tplXBlame     templates.TplName = "x/repo/blame"
	tplXIssues    templates.TplName = "x/repo/issues"
	tplXEmpty     templates.TplName = "x/repo/empty"
)

// xSetCommon fills the fields every experimental repository page needs.
func xSetCommon(ctx *context.Context, title any) {
	ctx.Data["Title"] = title
	xSetBase(ctx)
	// The production templates link through ctx.Repo.RepoLink, which points at
	// the classic UI. The experimental pages need their own base so a single
	// click never leaves the experiment.
	ctx.Data["XRepoLink"] = XBase + ctx.Repo.RepoLink
	ctx.Data["XCloneAddr"] = xCloneAddr(ctx)
	prepareRepoViewContent(ctx, ctx.Repo.RefTypeNameSubURL())
	// prepareRepoViewContent derives the tree links from the production repo
	// link, so they are rewritten to stay inside the experiment
	if treeLink, ok := ctx.Data["TreeLink"].(string); ok {
		ctx.Data["TreeLink"] = XBase + treeLink
	}
	if branchLink, ok := ctx.Data["BranchLink"].(string); ok {
		ctx.Data["BranchLink"] = XBase + branchLink
	}
}

// xSetBase fills the two values every experimental repository template needs to
// build a link, without doing any of the heavier view preparation.
func xSetBase(ctx *context.Context) {
	ctx.Data["XBase"] = XBase
	if ctx.Repo.Repository != nil {
		ctx.Data["XRepoLink"] = XBase + ctx.Repo.RepoLink
		ctx.Data["XCloneAddr"] = xCloneAddr(ctx)
	}
}

// xCloneAddr returns the HTTPS clone URL, or an empty string when there is none.
func xCloneAddr(ctx *context.Context) string {
	cloneLink := ctx.Repo.Repository.CloneLink(ctx, ctx.Doer)
	if !cloneLink.SupportHTTPS {
		return ""
	}
	return cloneLink.HTTPS
}

// XBase is the path prefix the experimental frontend is mounted at. Templates
// build every link from it, so the experiment can move without touching markup.
const XBase = "/_x"

// XHome renders the experimental repository home, directory or file view. The
// three cases share one handler because the underlying preparation is the same
// and only the template differs.
func XHome(ctx *context.Context) {
	context.CheckRepoScopedToken(ctx, ctx.Repo.Repository, auth_model.Read)
	if ctx.Written() {
		return
	}
	checkHomeCodeViewable(ctx)
	if ctx.Written() {
		return
	}

	xSetCommon(ctx, ctx.Repo.Repository.Owner.Name+"/"+ctx.Repo.Repository.Name)

	if ctx.Repo.Commit == nil || ctx.Repo.Repository.IsEmpty || ctx.Repo.Repository.IsBroken() {
		ctx.HTML(http.StatusOK, tplXEmpty)
		return
	}

	entry, err := ctx.Repo.Commit.GetTreeEntryByPath(ctx, ctx.Repo.GitRepo, ctx.Repo.TreePath)
	if err != nil {
		HandleGitError(ctx, "XHome: GetTreeEntryByPath", err)
		return
	}

	prepareToRenderDirOrFile(entry)(ctx)
	if ctx.Written() {
		return
	}
	xApplyFileFilter(ctx)

	switch {
	case entry.IsDir() && ctx.Repo.TreePath == "":
		ctx.HTML(http.StatusOK, tplXRepoHome)
	case entry.IsDir():
		ctx.HTML(http.StatusOK, tplXRepoDir)
	default:
		ctx.HTML(http.StatusOK, tplXRepoFile)
	}
}

// xTreeNode is one row of the lazy sidebar tree. Directories carry the URL that
// renders their children, so expanding a folder is an ordinary HTMX GET.
type xTreeNode struct {
	Name        string
	Path        string
	IsDir       bool
	ChildrenURL string
}

// XTreeNodes lists a single directory level as an HTML fragment. One level per
// request is what keeps the sidebar cheap on large repositories, and it means
// there is no JSON tree API to maintain.
func XTreeNodes(ctx *context.Context) {
	xSetBase(ctx)
	if ctx.Repo.Commit == nil {
		ctx.NotFound(nil)
		return
	}
	// RepoRefByType has already split the wildcard into a ref name and a path
	treePath := strings.Trim(path.Clean("/"+ctx.Repo.TreePath), "/")

	tree, err := ctx.Repo.Commit.SubTree(ctx, ctx.Repo.GitRepo, treePath)
	if err != nil {
		HandleGitError(ctx, "XTreeNodes: SubTree", err)
		return
	}
	entries, err := tree.ListEntries(ctx, ctx.Repo.GitRepo)
	if err != nil {
		ctx.ServerError("XTreeNodes: ListEntries", err)
		return
	}
	entries.CustomSort(base.NaturalSortCompare)

	nodes := make([]*xTreeNode, 0, len(entries))
	for _, entry := range entries {
		node := &xTreeNode{Name: entry.Name(), IsDir: entry.IsDir()}
		node.Path = path.Join(treePath, entry.Name())
		if entry.IsDir() {
			node.ChildrenURL = XBase + "/" + ctx.PathParam("username") + "/" + ctx.PathParam("reponame") +
				"/tree-nodes/branch/" + util.PathEscapeSegments(ctx.Repo.RefFullName.ShortName()) +
				"/" + util.PathEscapeSegments(node.Path)
		}
		nodes = append(nodes, node)
	}

	ctx.Data["XTreePath"] = treePath
	ctx.Data["XTreeNodes"] = nodes
	ctx.HTML(http.StatusOK, tplXTreeNodes)
}

// XCommits renders the experimental commit list. Pagination is a plain link
// when JavaScript is off, and an HTMX fragment swap when it is on.
func XCommits(ctx *context.Context) {
	ctx.Data["PageIsViewCode"] = true
	if ctx.Repo.Commit == nil {
		ctx.NotFound(nil)
		return
	}
	xSetCommon(ctx, ctx.Tr("repo.commits"))

	page := max(ctx.FormInt("page"), 1)
	pageSize := ctx.FormInt("limit")
	if pageSize <= 0 {
		pageSize = setting.Git.CommitsRangeSize
	}

	commits, err := ctx.Repo.Commit.CommitsByRange(ctx, ctx.Repo.GitRepo, page, pageSize, "", "", "")
	if err != nil {
		ctx.ServerError("XCommits: CommitsByRange", err)
		return
	}
	processed, err := processGitCommits(ctx, commits)
	if err != nil {
		ctx.ServerError("XCommits: processGitCommits", err)
		return
	}

	ctx.Data["XCommits"] = processed
	ctx.Data["XCommitCount"] = ctx.Repo.CommitsCount
	ctx.Data["XPage"] = page
	ctx.Data["XPageSize"] = pageSize
	ctx.Data["XHasPrev"] = page > 1
	ctx.Data["XHasNext"] = int64(page*pageSize) < ctx.Repo.CommitsCount

	ctx.HTML(http.StatusOK, tplXCommits)
}

// XBlame renders the experimental blame view. The heavy lifting is entirely
// shared with the production page; only the template differs.
func XBlame(ctx *context.Context) {
	if ctx.Repo.TreePath == "" || ctx.Repo.Commit == nil {
		ctx.NotFound(nil)
		return
	}
	xSetCommon(ctx, ctx.Tr("repo.blame"))
	ctx.Data["IsBlame"] = true

	entry, err := ctx.Repo.Commit.GetTreeEntryByPath(ctx, ctx.Repo.GitRepo, ctx.Repo.TreePath)
	if err != nil {
		HandleGitError(ctx, "XBlame: GetTreeEntryByPath", err)
		return
	}
	blob := entry.Blob(ctx.Repo.GitRepo)
	ctx.Data["FileSize"] = blob.Size(ctx)

	if fileSize, ok := ctx.Data["FileSize"].(int64); ok && fileSize >= setting.UI.MaxDisplayFileSize {
		ctx.Data["IsFileTooLarge"] = true
		ctx.HTML(http.StatusOK, tplXBlame)
		return
	}

	result, err := performBlame(ctx, false)
	if err != nil {
		HandleGitError(ctx, "XBlame: performBlame", err)
		return
	}
	commitNames := processBlameParts(ctx, result.Parts)
	if ctx.Written() {
		return
	}
	renderBlame(ctx, result.Parts, commitNames)

	ctx.HTML(http.StatusOK, tplXBlame)
}

// XIssues renders the experimental issue list. Search, state and assignee are
// ordinary query parameters, so every filter is a shareable URL.
func XIssues(ctx *context.Context) {
	MustEnableIssues(ctx)
	if ctx.Written() {
		return
	}

	ctx.Data["Title"] = ctx.Tr("repo.issues")
	xSetBase(ctx)
	prepareIssueFilterAndList(ctx, ctx.FormInt64("milestone"), nil, optional.None[bool]())
	if ctx.Written() {
		return
	}

	ctx.Data["XQuery"] = ctx.Req.URL.RawQuery
	ctx.HTML(http.StatusOK, tplXIssues)
}

// xApplyFileFilter narrows the directory listing to entries whose name contains
// the "filter" query parameter. It is a plain GET filter: without JavaScript the
// form submits and the server renders the narrowed list, with JavaScript HTMX
// re-renders only that one element.
func xApplyFileFilter(ctx *context.Context) {
	files, ok := ctx.Data["Files"].([]git.CommitInfo)
	if !ok {
		return
	}
	filter := strings.ToLower(ctx.FormTrim("filter"))
	ctx.Data["XFileFilter"] = ctx.FormString("filter")
	if filter == "" {
		return
	}
	kept := make([]git.CommitInfo, 0, len(files))
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Entry.Name()), filter) {
			kept = append(kept, f)
		}
	}
	ctx.Data["Files"] = kept
}
