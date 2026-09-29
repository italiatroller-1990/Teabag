// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import (
	"testing"

	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/reqctx"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
)

func TestCompareHeadRef(t *testing.T) {
	defer test.MockVariableValue(&setting.Repository.AllowForkIntoSameOwner, false)()
	baseRepo := &repo_model.Repository{ID: 1, OwnerID: 100, OwnerName: "base-owner", Name: "base-repo"}
	sameRepo := baseRepo
	sameOwner := &repo_model.Repository{ID: 2, OwnerID: 100, OwnerName: "head-owner", Name: "head-repo"}
	diffOwner := &repo_model.Repository{ID: 2, OwnerID: 101, OwnerName: "head-owner", Name: "head-repo"}

	assert.Equal(t, "my-branch", CompareHeadRef(baseRepo, sameRepo, "my-branch"))
	assert.Equal(t, "head-owner/head-repo:my-branch", CompareHeadRef(baseRepo, sameOwner, "my-branch"))
	assert.Equal(t, "head-owner:my-branch", CompareHeadRef(baseRepo, diffOwner, "my-branch"))
	setting.Repository.AllowForkIntoSameOwner = true
	assert.Equal(t, "head-owner/head-repo:my-branch", CompareHeadRef(baseRepo, diffOwner, "my-branch"))
}

// TestRepoAssignmentIsHomeOrSettings covers the check that keeps a repository
// that is still being created, or is broken, on its home page instead of
// bouncing the visitor to a page that cannot be rendered. A frontend mounted
// under a sub-path (the /_x experiment) prefixes every URL it renders, so its
// repository home is the same link behind that prefix — and nothing else is:
// comparing links by suffix would hand every classic route that happens to end
// in the repository link to the alternative frontend.
func TestRepoAssignmentIsHomeOrSettings(t *testing.T) {
	data := &repoAssignmentPrepareDataStruct{repo: &repo_model.Repository{OwnerName: "user2", Name: "repo1"}}
	testCases := []struct {
		name     string
		link     string
		basePath string
		expected bool
	}{
		{name: "ClassicHome", link: "/user2/repo1", expected: true},
		{name: "ClassicSettings", link: "/user2/repo1/settings", expected: true},
		{name: "ClassicMigrateStatus", link: "/user2/repo1/-/migrate/status", expected: true},
		{name: "ClassicCode", link: "/user2/repo1/src/branch/master", expected: false},
		{name: "ClassicIssues", link: "/user2/repo1/issues/1", expected: false},
		{name: "ExperimentalHome", link: "/_x/user2/repo1", basePath: "/_x", expected: true},
		{name: "ExperimentalSettings", link: "/_x/user2/repo1/settings", basePath: "/_x", expected: true},
		{name: "ExperimentalMigrateStatus", link: "/_x/user2/repo1/-/migrate/status", basePath: "/_x", expected: true},
		{name: "ExperimentalCode", link: "/_x/user2/repo1/src/branch/master", basePath: "/_x", expected: false},
		// the link ends in the repository link, but it belongs to another
		// frontend: the suffix comparison this replaced called it the home
		{name: "AnotherFrontend", link: "/y/user2/repo1", basePath: "/_x", expected: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{Base: &Base{Data: reqctx.ContextData{}}, Link: tc.link}
			if tc.basePath != "" {
				ctx.Data[FrontendBasePathKey] = tc.basePath
			}
			assert.Equal(t, tc.expected, repoAssignmentIsHomeOrSettings(ctx, data))
		})
	}

	t.Run("SubURL", func(t *testing.T) {
		defer test.MockVariableValue(&setting.AppSubURL, "/sub")()
		// the instance sub-path is part of both sides, and must appear once
		ctx := &Context{
			Base: &Base{Data: reqctx.ContextData{FrontendBasePathKey: "/sub/_x"}},
			Link: "/sub/_x/user2/repo1",
		}
		assert.True(t, repoAssignmentIsHomeOrSettings(ctx, data))
		ctx.Link = "/sub/user2/repo1"
		delete(ctx.Data, FrontendBasePathKey)
		assert.True(t, repoAssignmentIsHomeOrSettings(ctx, data))
	})
}
