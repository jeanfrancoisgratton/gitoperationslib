//go:build integration

package forgejo

import (
	"context"
	"fmt"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// TestIntegrationAllWithoutLinkHeader checks on a real server that All walks
// past the first page of lists that send X-Total-Count but no Link header.
func TestIntegrationAllWithoutLinkHeader(t *testing.T) {
	ctx, c := adminEnv(t)
	login, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + testLogin(t)
	t.Cleanup(func() { c.Repos().Delete(context.Background(), login, repo) })
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo, AutoInit: true}); err != nil {
		t.Fatal(err)
	}

	const total = 3
	scope := gitoperationslib.RepoCI(login, repo)
	for i := range total {
		if _, err := c.Labels().Create(ctx, login, repo, gitoperationslib.Label{Name: fmt.Sprintf("l%d", i), Color: "ff0000"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Milestones().Create(ctx, login, repo, gitoperationslib.CreateMilestoneOptions{Title: fmt.Sprintf("m%d", i)}); err != nil {
			t.Fatal(err)
		}
		if err := c.Secrets().Set(ctx, scope, fmt.Sprintf("S%d", i), "v"); err != nil {
			t.Fatal(err)
		}
		if err := c.Variables().Set(ctx, scope, fmt.Sprintf("V%d", i), fmt.Sprintf("value%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	one := gitoperationslib.ListOptions{PerPage: 1}
	if got := collect(t, one, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Label], error) {
		return c.Labels().List(ctx, login, repo, o)
	}); got != total {
		t.Errorf("labels: All returned %d, want %d", got, total)
	}
	if got := collect(t, one, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Milestone], error) {
		return c.Milestones().List(ctx, login, repo, gitoperationslib.MilestoneStateOpen, o)
	}); got != total {
		t.Errorf("milestones: All returned %d, want %d", got, total)
	}
	if got := collect(t, one, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Secret], error) {
		return c.Secrets().List(ctx, scope, o)
	}); got != total {
		t.Errorf("secrets: All returned %d, want %d", got, total)
	}
	values := map[string]string{}
	for v, err := range gitoperationslib.All(ctx, one, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Variable], error) {
		return c.Variables().List(ctx, scope, o)
	}) {
		if err != nil {
			t.Fatal(err)
		}
		values[v.Name] = v.Value
	}
	for i := range total {
		if name, want := fmt.Sprintf("V%d", i), fmt.Sprintf("value%d", i); values[name] != want {
			t.Errorf("variables: %s = %q, want %q (all: %v)", name, values[name], want, values)
		}
	}

	// Label names resolve to IDs beyond the first page of the server's list.
	ids, err := c.labelIDs(ctx, login, repo, []string{"l0", "l1", "l2"})
	if err != nil || len(ids) != total {
		t.Errorf("labelIDs = %v, %v", ids, err)
	}
}
