//go:build integration

package forgejo

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// TestIntegrationAdminOrgLabels covers OrgLabels, and how an issue uses them.
func TestIntegrationAdminOrgLabels(t *testing.T) {
	ctx, c := adminEnv(t)
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	org := contracttest.RepoPrefix + "orglabels"
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: org}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Orgs().Delete(context.Background(), org) })

	empty, err := c.ListOrgLabels(ctx, org, gitoperationslib.ListOptions{})
	if err != nil || len(empty.Items) != 0 {
		t.Errorf("no labels yet: %+v, %v", empty, err)
	}
	bug, err := c.CreateOrgLabel(ctx, org, gitoperationslib.Label{Name: "bug", Color: "#D73A4A", Description: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	if bug.ID == 0 || bug.Color != "d73a4a" || bug.Description != "broken" {
		t.Errorf("created = %+v", bug)
	}
	if _, err := c.CreateOrgLabel(ctx, org, gitoperationslib.Label{Name: "bug"}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
		t.Errorf("name in use: %v", err)
	}
	other, err := c.CreateOrgLabel(ctx, org, gitoperationslib.Label{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if other.Color != "ededed" {
		t.Errorf("default colour = %+v", other)
	}
	if got, err := c.GetOrgLabel(ctx, org, bug.ID); err != nil || got.Name != "bug" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := c.GetOrgLabel(ctx, org, 99999); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(missing): %v", err)
	}
	if _, err := c.EditOrgLabel(ctx, org, other.ID, gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("bug")}); !errors.Is(err, gitoperationslib.ErrNameTaken) {
		t.Errorf("rename onto a name in use: %v", err)
	}
	edited, err := c.EditOrgLabel(ctx, org, other.ID, gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("other"), Color: gitoperationslib.Ptr("FBCA04")})
	if err != nil || edited.Color != "fbca04" {
		t.Errorf("Edit keeping its name = %+v, %v", edited, err)
	}
	seen := 0
	for _, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
		return c.ListOrgLabels(ctx, org, o)
	}) {
		if err != nil {
			t.Fatal(err)
		}
		seen++
	}
	if seen != 2 {
		t.Errorf("All saw %d labels", seen)
	}

	// A user has none; a name that is neither is a 404.
	if page, err := c.ListOrgLabels(ctx, admin, gitoperationslib.ListOptions{}); err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Errorf("a user: %+v, %v", page, err)
	}
	if _, err := c.ListOrgLabels(ctx, "no-such-owner-at-all", gitoperationslib.ListOptions{}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("nobody: %v", err)
	}

	// Issues use them by name, and a filter finds them.
	repo := contracttest.RepoPrefix + "orglabelrepo"
	t.Cleanup(func() { _ = c.Repos().Delete(context.Background(), org, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: org, Name: repo}); err != nil {
		t.Fatal(err)
	}
	with, err := c.Issues().Create(ctx, org, repo, gitoperationslib.CreateIssueOptions{Title: "with", Labels: []string{"bug"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(with.Labels, []string{"bug"}) {
		t.Errorf("issue = %+v", with)
	}
	without, err := c.Issues().Create(ctx, org, repo, gitoperationslib.CreateIssueOptions{Title: "without", Labels: []string{"other"}})
	if err != nil {
		t.Fatal(err)
	}
	filtered := func(names ...string) []int {
		t.Helper()
		page, err := c.Issues().List(ctx, org, repo, gitoperationslib.ListIssuesOptions{Labels: names})
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, i := range page.Items {
			out = append(out, i.Number)
		}
		return out
	}
	if got := filtered("bug"); !slices.Equal(got, []int{with.Number}) {
		t.Errorf("filter on bug = %v, want %d, not %d", got, with.Number, without.Number)
	}
	if got := filtered("bug", "no-such"); len(got) != 0 {
		t.Errorf("filter with an unknown name = %v", got)
	}
	if got := filtered("bug", "other"); len(got) != 0 {
		t.Errorf("filter on both, which no issue carries = %v", got)
	}

	if err := c.DeleteOrgLabel(ctx, org, other.ID); err != nil {
		t.Error(err)
	}
	if err := c.DeleteOrgLabel(ctx, org, other.ID); !gitoperationslib.IsNotFound(err) {
		t.Errorf("delete a missing label: %v", err)
	}
}

// TestIntegrationAdminDuplicateLabels makes the duplicates only the raw API can,
// and repairs them through LabelsByID.
func TestIntegrationAdminDuplicateLabels(t *testing.T) {
	ctx, c := adminEnv(t)
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + "duplabels"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), admin, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for range 2 {
		var out labelJSON
		if _, err := c.api.Do(ctx, http.MethodPost, repoPath(admin, repo, "labels"), nil, map[string]string{"name": "dup", "color": "#ededed"}, &out); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, out.ID)
	}
	labels := c.Labels()
	if _, err := labels.Get(ctx, admin, repo, "dup"); !errors.Is(err, gitoperationslib.ErrAmbiguousName) {
		t.Errorf("Get: %v", err)
	}
	if _, err := labels.Edit(ctx, admin, repo, "dup", gitoperationslib.EditLabelOptions{Color: gitoperationslib.Ptr("000000")}); !errors.Is(err, gitoperationslib.ErrAmbiguousName) {
		t.Errorf("Edit: %v", err)
	}
	if err := labels.Delete(ctx, admin, repo, "dup"); !errors.Is(err, gitoperationslib.ErrAmbiguousName) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := c.Issues().Create(ctx, admin, repo, gitoperationslib.CreateIssueOptions{Title: "x", Labels: []string{"dup"}}); !errors.Is(err, gitoperationslib.ErrAmbiguousName) {
		t.Errorf("Issues.Create: %v", err)
	}

	// Repairing: rename one, delete the other.
	if _, err := c.EditLabelByID(ctx, admin, repo, ids[0], gitoperationslib.EditLabelOptions{Name: gitoperationslib.Ptr("kept")}); err != nil {
		t.Fatal(err)
	}
	if got, err := labels.Get(ctx, admin, repo, "dup"); err != nil || got.ID != ids[1] {
		t.Errorf("after the rename: %+v, %v", got, err)
	}
	if err := c.DeleteLabelByID(ctx, admin, repo, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteLabelByID(ctx, admin, repo, ids[1]); !gitoperationslib.IsNotFound(err) {
		t.Errorf("delete a missing label: %v", err)
	}
	if _, err := c.EditLabelByID(ctx, admin, repo, ids[0], gitoperationslib.EditLabelOptions{Color: gitoperationslib.Ptr("000000")}); err != nil {
		t.Errorf("edit without renaming: %v", err)
	}
}
