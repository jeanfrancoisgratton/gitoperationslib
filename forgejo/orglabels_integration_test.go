//go:build integration

package forgejo

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/forgejoapi"
)

// TestIntegrationOrgLabelsByName sets, replaces and removes organisation
// labels by name on an issue, on a real server.
func TestIntegrationOrgLabelsByName(t *testing.T) {
	ctx, c := adminEnv(t)
	org := testLogin(t)
	t.Cleanup(func() { c.Orgs().Delete(context.Background(), org) })
	if _, err := c.Orgs().Create(ctx, gitoperationslib.CreateOrgOptions{Name: org}); err != nil {
		t.Fatal(err)
	}
	const repo = "widget"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), org, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Owner: org, Name: repo}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"org-a", "org-b", "area/org", "shared"} {
		body := map[string]string{"name": name, "color": "00ff00"}
		if _, err := c.api.Do(ctx, http.MethodPost, forgejoapi.Path("orgs", org, "labels"), nil, body, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"repo-a", "shared"} {
		if _, err := c.Labels().Create(ctx, org, repo, gitoperationslib.Label{Name: name, Color: "ff0000"}); err != nil {
			t.Fatal(err)
		}
	}

	labelsOf := func(number int) []string {
		t.Helper()
		issue, err := c.Issues().Get(ctx, org, repo, number)
		if err != nil {
			t.Fatal(err)
		}
		names := slices.Clone(issue.Labels)
		slices.Sort(names)
		return names
	}
	check := func(number int, want ...string) {
		t.Helper()
		if got := labelsOf(number); !slices.Equal(got, want) {
			t.Errorf("labels = %v, want %v", got, want)
		}
	}

	issue, err := c.Issues().Create(ctx, org, repo, gitoperationslib.CreateIssueOptions{Title: "t", Labels: []string{"org-a", "repo-a"}})
	if err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "org-a", "repo-a")

	if err := c.Issues().AddLabels(ctx, org, repo, issue.Number, []string{"org-b"}); err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "org-a", "org-b", "repo-a")

	if err := c.Issues().RemoveLabel(ctx, org, repo, issue.Number, "org-a"); err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "org-b", "repo-a")

	if _, err := c.Issues().Edit(ctx, org, repo, issue.Number, gitoperationslib.EditIssueOptions{Labels: &[]string{"org-a"}}); err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "org-a")

	// A slash in the name, which the removal puts in the URL.
	if err := c.Issues().AddLabels(ctx, org, repo, issue.Number, []string{"area/org"}); err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "area/org", "org-a")
	if err := c.Issues().RemoveLabel(ctx, org, repo, issue.Number, "area/org"); err != nil {
		t.Errorf("remove a label with a slash: %v", err)
	}
	check(issue.Number, "org-a")

	// The repository's label wins over the organisation's of the same name.
	if err := c.Issues().AddLabels(ctx, org, repo, issue.Number, []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	check(issue.Number, "org-a", "shared")
	if err := c.Issues().RemoveLabel(ctx, org, repo, issue.Number, "shared"); err != nil {
		t.Errorf("remove a name used at both levels: %v", err)
	}
	check(issue.Number, "org-a")

	if err := c.Issues().AddLabels(ctx, org, repo, issue.Number, []string{"nope"}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown label: err = %v, want 404", err)
	}
	if err := c.Issues().RemoveLabel(ctx, org, repo, issue.Number, "nope"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("remove an unknown label: err = %v, want 404", err)
	}

	// On a user's repository, the failed organisation lookup is just "not found".
	login, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mine := "userrepo-" + org
	t.Cleanup(func() { c.Repos().Delete(context.Background(), login, mine) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: mine}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Issues().Create(ctx, login, mine, gitoperationslib.CreateIssueOptions{Title: "t", Labels: []string{"org-a"}}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("label on a user's repository: err = %v, want 404", err)
	}
}
