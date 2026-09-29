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
	asymkey_service "gitea.dev/services/asymkey"
	"gitea.dev/services/context"
	"gitea.dev/services/gitdiff"
)

// XBase is the path prefix the experimental frontend is mounted at, relative to
// the site root. Templates build their own links from XBasePath instead, which
// also carries the instance sub-path, so the experiment keeps working under a
// ROOT_URL with a path and not only at the domain root.
//
// Every URL the experiment renders is XBasePath() plus a repository-relative
// path, so the instance sub-path is carried exactly once.
const XBase = "/_x"

// XBasePath returns the externally visible prefix of the experimental frontend.
func XBasePath() string {
	return setting.AppSubURL + XBase
}

// xRepoPath returns the "/owner/repo" part every repository URL of the
// experiment starts with. It is built from the raw path parameters, which are
// escaped the way the router escaped them in the request, so an owner or a
// repository name that needs escaping produces a link the router can route
// back. The unescaped PathParam must never be pasted into a URL.
func xRepoPath(ctx *context.Context) string {
	return "/" + ctx.PathParamRaw("username") + "/" + ctx.PathParamRaw("reponame")
}

const (
	tplXRepoHome        templates.TplName = "x/repo/home"
	tplXRepoDir         templates.TplName = "x/repo/dir"
	tplXRepoFile        templates.TplName = "x/repo/view"
	tplXFileList        templates.TplName = "x/repo/file_list"
	tplXTreeNodes       templates.TplName = "x/repo/tree_nodes"
	tplXCommits         templates.TplName = "x/repo/commits"
	tplXCommitsFragment templates.TplName = "x/repo/commits_fragment"
	tplXCommit          templates.TplName = "x/repo/commit"
	tplXBlame           templates.TplName = "x/repo/blame"
	tplXIssues          templates.TplName = "x/repo/issues"
	tplXIssuesFragment  templates.TplName = "x/repo/issues_fragment"
	tplXIssue           templates.TplName = "x/repo/issue"
	tplXEmpty           templates.TplName = "x/repo/empty"
	tplXMigrating       templates.TplName = "x/repo/migrating"
)

// xSetCommon fills the fields every experimental repository page needs.
func xSetCommon(ctx *context.Context, title any) {
	ctx.Data["Title"] = title
	xSetBase(ctx)
	prepareRepoViewContent(ctx, ctx.Repo.RefTypeNameSubURL())
	// prepareRepoViewContent derives the tree links from the production repo
	// link, which is built for the classic frontend. The experiment builds its
	// own from its own base, so a single click never leaves the experiment.
	refTypeNameSubURL := ctx.Repo.RefTypeNameSubURL()
	ctx.Data["XBranchLink"] = XBasePath() + xRepoPath(ctx) + "/src/" + refTypeNameSubURL
	ctx.Data["XTreeLink"] = xPageLink(ctx, "src")
	// the fragment endpoints of the two pages that have one, so the page and
	// the fragment it upgrades can never disagree about the directory they are
	// about
	ctx.Data["XFileListLink"] = xPageLink(ctx, "files")
	ctx.Data["XCommitsLink"] = xPageLink(ctx, "commits")
	// the sidebar tree is always rooted at the repository root, so it has no
	// tree path of its own
	ctx.Data["XTreeNodesLink"] = XBasePath() + xRepoPath(ctx) + "/tree-nodes/" + refTypeNameSubURL
}

// xPageLink builds the URL of a page of the current repository, at the current
// reference and tree path. The tree path is escaped the way the router escapes
// it, so the link resolves to the same directory the page is showing.
func xPageLink(ctx *context.Context, page string) string {
	link := XBasePath() + xRepoPath(ctx) + "/" + page + "/" + ctx.Repo.RefTypeNameSubURL()
	if ctx.Repo.TreePath != "" {
		link += "/" + util.PathEscapeSegments(ctx.Repo.TreePath)
	}
	return link
}

// xSetBase fills the values every experimental repository template needs to
// build a link, without doing any of the heavier view preparation. The
// production templates link through ctx.Repo.RepoLink, which points at the
// classic UI; the experimental pages need their own base so a single click
// never leaves the experiment.
func xSetBase(ctx *context.Context) {
	ctx.Data["XBase"] = XBasePath()
	if ctx.Repo.Repository != nil {
		ctx.Data["XRepoLink"] = XBasePath() + xRepoPath(ctx)
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

// xRenderMigrating renders the experimental page for a repository whose import
// is still running, instead of letting checkHomeCodeViewable answer with the
// classic page. The task state comes from the same helper the classic page
// uses; the actions that change it stay in the classic UI, as everywhere else
// in the experiment.
func xRenderMigrating(ctx *context.Context) {
	setMigratingData(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Title"] = ctx.Repo.Repository.Owner.Name + "/" + ctx.Repo.Repository.Name
	xSetBase(ctx)
	ctx.HTML(http.StatusOK, tplXMigrating)
}

// XHome renders the experimental repository home, directory or file view. The
// three cases share one handler because the underlying preparation is the same
// and only the template differs.
func XHome(ctx *context.Context) {
	context.CheckRepoScopedToken(ctx, ctx.Repo.Repository, auth_model.Read)
	if ctx.Written() {
		return
	}
	// the migrating state is the one state of the repository home the
	// experiment has a page of its own for: checkHomeCodeViewable would answer
	// with the classic migrating page, whose JavaScript it neither ships nor
	// needs
	if ctx.Repo.Permission.HasUnits() && ctx.Repo.Repository.IsBeingCreated() {
		xRenderMigrating(ctx)
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

// XFileList renders the directory listing on its own, as the HTML fragment the
// filter form swaps in. The full page and the fragment share one preparation
// path, so they cannot drift apart, and the fragment is reachable without
// JavaScript as a plain GET.
func XFileList(ctx *context.Context) {
	context.CheckRepoScopedToken(ctx, ctx.Repo.Repository, auth_model.Read)
	if ctx.Written() {
		return
	}
	checkHomeCodeViewable(ctx)
	if ctx.Written() || ctx.Repo.Commit == nil {
		return
	}
	entry, err := ctx.Repo.Commit.GetTreeEntryByPath(ctx, ctx.Repo.GitRepo, ctx.Repo.TreePath)
	if err != nil {
		HandleGitError(ctx, "XFileList: GetTreeEntryByPath", err)
		return
	}
	if !entry.IsDir() {
		ctx.NotFound(nil)
		return
	}
	// the fragment renders the same links as the full page, so it needs the
	// same preparation: without it every href in it would be built from an
	// empty link base
	xSetCommon(ctx, ctx.Repo.Repository.Owner.Name+"/"+ctx.Repo.Repository.Name)
	if ctx.Written() {
		return
	}
	prepareToRenderDirectory(ctx)
	if ctx.Written() {
		return
	}
	xApplyFileFilter(ctx)
	ctx.HTML(http.StatusOK, tplXFileList)
}

// xTreeNode is one row of the lazy sidebar tree. Directories carry the URL that
// renders their children, so expanding a folder is an ordinary HTMX GET.
type xTreeNode struct {
	Name string
	// Path is the node path relative to the directory the tree is rooted at.
	Path string
	// Link is the path of the node inside the repository, always absolute, so
	// the same template works for a tree rooted anywhere in the repository.
	Link string
	// ChildrenID is the DOM id of this node's (empty) child list. It is derived
	// from the node path, so it is unique in the document: an index would be
	// reused by every directory level and hx-target would resolve to the wrong
	// subtree.
	ChildrenID  string
	IsDir       bool
	ChildrenURL string
}

// XTreeNodes lists a single directory level as an HTML fragment. One level per
// request is what keeps the sidebar cheap on large repositories, and it means
// there is no JSON tree API to maintain.
func XTreeNodes(ctx *context.Context) {
	context.CheckRepoScopedToken(ctx, ctx.Repo.Repository, auth_model.Read)
	if ctx.Written() {
		return
	}
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

	repoPath := xRepoPath(ctx)
	// RefTypeNameSubURL is "branch/<name>", "tag/<name>" or "commit/<sha>", so
	// a link built from it is valid for every kind of reference. Node paths are
	// absolute, so the tree can be rooted anywhere in the repository.
	refPath := ctx.Repo.RefTypeNameSubURL()
	xBase := XBasePath() + repoPath

	nodes := make([]*xTreeNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		fullPath := path.Join(treePath, name)
		node := &xTreeNode{
			Name:       name,
			IsDir:      entry.IsDir(),
			Path:       fullPath,
			Link:       xBase + "/src/" + refPath + "/" + util.PathEscapeSegments(fullPath),
			ChildrenID: "x-tree-" + xTreeNodeID(fullPath),
		}
		if entry.IsDir() {
			node.ChildrenURL = xBase + "/tree-nodes/" + refPath + "/" + util.PathEscapeSegments(fullPath)
		}
		nodes = append(nodes, node)
	}

	ctx.Data["XTreePath"] = treePath
	ctx.Data["XTreeNodes"] = nodes
	ctx.HTML(http.StatusOK, tplXTreeNodes)
}

// xTreeNodeID turns a repository path into a DOM-id-safe fragment. The id has
// to be stable across requests (it is a swap target) and unique inside one
// document, which a repository path satisfies by construction.
func xTreeNodeID(nodePath string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, nodePath)
}

// XCommits renders the experimental commit list. Pagination is a plain link
// when JavaScript is off, and an HTMX fragment swap when it is on.
func XCommits(ctx *context.Context) {
	if !xPrepareCommits(ctx) {
		return
	}
	ctx.HTML(http.StatusOK, tplXCommits)
}

// XCommitsList renders the commit rows and the pager as the HTML fragment the
// pager swaps in: the list directly, the pager out-of-band into #x-pager.
func XCommitsList(ctx *context.Context) {
	if !xPrepareCommits(ctx) {
		return
	}
	ctx.HTML(http.StatusOK, tplXCommitsFragment)
}

// xCommitsListLink is the URL of the commit-list fragment for the current ref,
// so the pager and the list can never disagree about which repository, ref and
// path they are about.
func xCommitsListLink(ctx *context.Context) string {
	return xPageLink(ctx, "commits/list")
}

func xPrepareCommits(ctx *context.Context) bool {
	ctx.Data["PageIsViewCode"] = true
	if ctx.Repo.Commit == nil {
		ctx.NotFound(nil)
		return false
	}
	xSetCommon(ctx, ctx.Tr("repo.commits"))

	page := max(ctx.FormInt("page"), 1)
	// the page size is a query parameter so the pager can keep it across pages;
	// a request must not be able to ask for more commits than the site-wide
	// maximum, or one URL would make the server walk the whole history
	pageSize := ctx.FormInt("limit")
	if pageSize <= 0 || pageSize > setting.Git.CommitsRangeSize {
		pageSize = setting.Git.CommitsRangeSize
	}

	commits, err := ctx.Repo.Commit.CommitsByRange(ctx, ctx.Repo.GitRepo, page, pageSize, "", "", "")
	if err != nil {
		ctx.ServerError("XCommits: CommitsByRange", err)
		return false
	}
	processed, err := processGitCommits(ctx, commits)
	if err != nil {
		ctx.ServerError("XCommits: processGitCommits", err)
		return false
	}

	ctx.Data["XCommits"] = processed
	ctx.Data["XCommitCount"] = ctx.Repo.CommitsCount
	ctx.Data["XPage"] = page
	ctx.Data["XPageSize"] = pageSize
	ctx.Data["XHasPrev"] = page > 1
	ctx.Data["XHasNext"] = int64(page*pageSize) < ctx.Repo.CommitsCount
	// the page count has to round up: 7 commits in pages of 5 are 2 pages
	ctx.Data["XPageCount"] = int((ctx.Repo.CommitsCount + int64(pageSize) - 1) / int64(pageSize))
	// the pager swaps this fragment instead of the full page
	ctx.Data["XCommitsListLink"] = xCommitsListLink(ctx)
	return true
}

// XCommit renders a single commit: who wrote it, when, the full message, its
// parents and the files it changed. The experiment deliberately renders the
// changed-file list rather than a diff, because every file in it links to a
// page the experiment already serves ("browse at this commit").
func XCommit(ctx *context.Context) {
	context.CheckRepoScopedToken(ctx, ctx.Repo.Repository, auth_model.Read)
	if ctx.Written() {
		return
	}
	commit, err := ctx.Repo.GitRepo.GetCommit(ctx, ctx.PathParam("sha"))
	if err != nil {
		HandleGitError(ctx, "XCommit: GetCommit", err)
		return
	}
	commitID := commit.ID.String()

	xSetBase(ctx)
	prepareRepoViewContent(ctx, "commit/"+util.PathEscapeSegments(commitID))
	if ctx.Written() {
		return
	}

	parents := make([]string, 0, commit.ParentCount())
	for i := range commit.ParentCount() {
		id, err := commit.ParentID(i)
		if err != nil {
			ctx.ServerError("XCommit: ParentID", err)
			return
		}
		parents = append(parents, id.String())
	}

	// an empty base is what makes git diff-tree list the files of a root commit
	diffTree, err := gitdiff.GetDiffTree(ctx, ctx.Repo.GitRepo, false, "", commitID)
	if err != nil {
		ctx.ServerError("XCommit: GetDiffTree", err)
		return
	}

	ctx.Data["Title"] = commit.MessageTitle() + " · " + base.ShortSha(commitID)
	ctx.Data["XCommitID"] = commitID
	ctx.Data["XCommit"] = commit
	ctx.Data["XCommitParents"] = parents
	ctx.Data["XCommitFiles"] = xCommitFiles(ctx, commitID, diffTree)
	ctx.Data["XVerification"] = asymkey_service.ParseCommitWithSignature(ctx, commit)
	ctx.HTML(http.StatusOK, tplXCommit)
}

// xCommitFile is one changed file of a commit page.
type xCommitFile struct {
	Status   string
	HeadPath string
	BasePath string
	IsRename bool
	// Link points at the file inside the experiment, at this commit, so the
	// commit page can be browsed without leaving it.
	Link string
}

// xCommitFiles adapts git diff-tree records to what the template renders.
func xCommitFiles(ctx *context.Context, commitID string, tree *gitdiff.DiffTree) []*xCommitFile {
	repoLink, _ := ctx.Data["XRepoLink"].(string)
	commitPath := repoLink + "/src/commit/" + util.PathEscapeSegments(commitID)
	files := make([]*xCommitFile, 0, len(tree.Files))
	for _, record := range tree.Files {
		file := &xCommitFile{
			Status:   record.Status,
			HeadPath: record.HeadPath,
			BasePath: record.BasePath,
			IsRename: record.Status == "renamed" || record.Status == "copied",
			Link:     commitPath + "/" + util.PathEscapeSegments(record.HeadPath),
		}
		files = append(files, file)
	}
	return files
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
	ctx.Data["FileTreePath"] = ctx.Repo.TreePath
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
	if ctx.Written() {
		return
	}
	rows, ok := ctx.Data["BlameRows"].([]*blameRow)
	if !ok {
		ctx.ServerError("XBlame: renderBlame did not set BlameRows", nil)
		return
	}
	// renderBlame links to the classic commit page; the experiment serves that
	// page too, so the link is rebuilt from the experiment's own base. The row
	// carries the sha exactly because the link is rebuilt here.
	xBase := XBasePath() + xRepoPath(ctx)
	for _, row := range rows {
		if row.CommitURL != "" {
			row.CommitURL = xBase + "/commit/" + row.CommitSha
		}
	}

	ctx.HTML(http.StatusOK, tplXBlame)
}

// XIssues renders the experimental issue list. Search, state, assignee and
// label are ordinary query parameters, so every filter is a shareable URL.
func XIssues(ctx *context.Context) {
	xPrepareIssueList(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Title"] = ctx.Tr("repo.issues")
	ctx.HTML(http.StatusOK, tplXIssues)
}

// XIssueList renders the issue rows and the pager as the HTML fragment the
// filter form and the pager swap in: the list directly, the pager out-of-band
// into #x-pager. It is the same preparation and the same partials as the
// full page, so the two cannot disagree.
func XIssueList(ctx *context.Context) {
	xPrepareIssueList(ctx)
	if ctx.Written() {
		return
	}
	ctx.HTML(http.StatusOK, tplXIssuesFragment)
}

func xPrepareIssueList(ctx *context.Context) {
	MustEnableIssues(ctx)
	if ctx.Written() {
		return
	}
	xSetBase(ctx)
	prepareIssueFilterAndList(ctx, ctx.FormInt64("milestone"), nil, optional.None[bool]())
}

// XViewIssue renders a read-only issue page. Editing, commenting and every
// other write stays in the classic UI, where the production forms and their
// permission checks already live; the experiment never re-implements them.
func XViewIssue(ctx *context.Context) {
	handleViewIssueRedirectExternal(ctx)
	if ctx.Written() {
		return
	}
	issue := prepareIssueViewLoad(ctx)
	if ctx.Written() {
		return
	}
	if issue.IsPull {
		// the experiment renders issues, not pull requests: as in the
		// production code, a pull request under the issues route is redirected
		// to its own view instead of being shown as an issue
		ctx.Redirect(issue.Link())
		return
	}
	MustEnableIssues(ctx)
	if ctx.Written() {
		return
	}

	xSetBase(ctx)
	if err := issue.LoadAttributes(ctx); err != nil {
		ctx.ServerError("XViewIssue: LoadAttributes", err)
		return
	}
	if err := filterXRefComments(ctx, issue); err != nil {
		ctx.ServerError("XViewIssue: filterXRefComments", err)
		return
	}
	combineXRefComments(issue)

	prepareIssueViewContent(ctx, issue)
	if ctx.Written() {
		return
	}
	prepareIssueViewCommentsAndSidebarParticipants(ctx, issue)
	if ctx.Written() {
		return
	}

	ctx.Data["Title"] = issue.Title
	ctx.Data["XIssue"] = issue
	ctx.Data["XIssueLabels"] = issue.Labels
	ctx.Data["XIssueAssignees"] = issue.Assignees
	ctx.Data["HasIssuesOrPullsWritePermission"] = ctx.Repo.Permission.CanWriteIssuesOrPulls(issue.IsPull)
	ctx.HTML(http.StatusOK, tplXIssue)
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
