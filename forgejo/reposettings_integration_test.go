//go:build integration

package forgejo

import (
	"context"
	"errors"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// TestIntegrationAdminRepoSettings covers Forgejo's own settings, and the traps of
// its PATCH: a partial internal_tracker resets the rest, and has_issues or has_wiki
// set to true drops an external tracker or wiki.
func TestIntegrationAdminRepoSettings(t *testing.T) {
	ctx, c := adminEnv(t)
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := contracttest.RepoPrefix + "settings"
	t.Cleanup(func() { _ = c.Repos().Delete(context.Background(), admin, name) })
	created, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	is := func(p *bool, want bool) bool { return p != nil && *p == want }
	if tr := created.IssueTracker; !is(tr.TimeTracking, true) || !is(tr.Dependencies, true) || tr.External == nil || tr.External.URL != "" {
		t.Errorf("a new repository's tracker = %+v", tr)
	}
	if w := created.Wiki; w.ExternalURL == nil || *w.ExternalURL != "" || !is(w.EditableByAll, false) {
		t.Errorf("a new repository's wiki = %+v", w)
	}
	repos := c.Repos()
	edit := func(opts gitoperationslib.EditRepoOptions) *gitoperationslib.Repo {
		t.Helper()
		repo, err := repos.Edit(ctx, admin, name, opts)
		if err != nil {
			t.Fatal(err)
		}
		return repo
	}

	// One tracker setting leaves the others.
	repo := edit(gitoperationslib.EditRepoOptions{IssueTracker: gitoperationslib.IssueTrackerSettings{TimeTracking: gitoperationslib.Ptr(false)}})
	if tr := repo.IssueTracker; !is(tr.TimeTracking, false) || !is(tr.Dependencies, true) || !is(tr.TimeTrackingContributorsOnly, true) {
		t.Errorf("after one setting = %+v", tr)
	}
	// Issues on, which the server takes as "back to the defaults", keeps them too.
	repo = edit(gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Issues: gitoperationslib.Ptr(true)}})
	if !is(repo.IssueTracker.TimeTracking, false) {
		t.Errorf("after issues on = %+v", repo.IssueTracker)
	}

	external := &gitoperationslib.ExternalTracker{URL: "https://tracker.example.com", Format: "https://tracker.example.com/{user}/{repo}/{index}"}
	repo = edit(gitoperationslib.EditRepoOptions{IssueTracker: gitoperationslib.IssueTrackerSettings{External: external}})
	if ext := repo.IssueTracker.External; ext == nil || ext.URL != external.URL || ext.Style != gitoperationslib.ExternalIssueNumeric {
		t.Errorf("external tracker = %+v", ext)
	}
	repo = edit(gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Issues: gitoperationslib.Ptr(true)}, Description: gitoperationslib.Ptr("x")})
	if ext := repo.IssueTracker.External; ext == nil || ext.URL != external.URL {
		t.Errorf("issues on dropped the external tracker: %+v", ext)
	}
	// Forgejo does not keep the built-in tracker's settings meanwhile: back to it,
	// they are its defaults.
	repo = edit(gitoperationslib.EditRepoOptions{IssueTracker: gitoperationslib.IssueTrackerSettings{External: &gitoperationslib.ExternalTracker{}}})
	if ext := repo.IssueTracker.External; ext == nil || ext.URL != "" || !is(repo.IssueTracker.TimeTracking, true) {
		t.Errorf("back to the built-in tracker = %+v", repo.IssueTracker)
	}
	if _, err := repos.Edit(ctx, admin, name, gitoperationslib.EditRepoOptions{IssueTracker: gitoperationslib.IssueTrackerSettings{External: &gitoperationslib.ExternalTracker{URL: "not a url"}}}); gitoperationslib.StatusCode(err) != 422 {
		t.Errorf("an invalid tracker URL: %v", err)
	}

	repo = edit(gitoperationslib.EditRepoOptions{Wiki: gitoperationslib.WikiSettings{EditableByAll: gitoperationslib.Ptr(true)}})
	if w := repo.Wiki; !is(w.EditableByAll, true) {
		t.Errorf("editable by all = %+v", w)
	}
	repo = edit(gitoperationslib.EditRepoOptions{Wiki: gitoperationslib.WikiSettings{ExternalURL: gitoperationslib.Ptr("https://wiki.example.com")}})
	if w := repo.Wiki; w.ExternalURL == nil || *w.ExternalURL != "https://wiki.example.com" {
		t.Errorf("external wiki = %+v", w)
	}
	repo = edit(gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Wiki: gitoperationslib.Ptr(true)}})
	if w := repo.Wiki; w.ExternalURL == nil || *w.ExternalURL != "https://wiki.example.com" {
		t.Errorf("wiki on dropped the external wiki: %+v", w)
	}
	repo = edit(gitoperationslib.EditRepoOptions{Wiki: gitoperationslib.WikiSettings{ExternalURL: gitoperationslib.Ptr("")}})
	if w := repo.Wiki; w.ExternalURL == nil || *w.ExternalURL != "" {
		t.Errorf("back to the built-in wiki = %+v", w)
	}

	// Settings that need their feature on.
	edit(gitoperationslib.EditRepoOptions{Features: gitoperationslib.RepoFeatures{Issues: gitoperationslib.Ptr(false)}})
	if _, err := repos.Edit(ctx, admin, name, gitoperationslib.EditRepoOptions{IssueTracker: gitoperationslib.IssueTrackerSettings{Dependencies: gitoperationslib.Ptr(false)}}); err == nil {
		t.Error("tracker settings with issues off accepted")
	}

	squash := gitoperationslib.MergeMethodSquash
	repo = edit(gitoperationslib.EditRepoOptions{Merge: gitoperationslib.MergeSettings{DefaultMethod: &squash, FastForwardOnly: gitoperationslib.Ptr(false), RebaseUpdate: gitoperationslib.Ptr(false)}})
	if m := repo.Merge; m.DefaultMethod == nil || *m.DefaultMethod != squash || !is(m.FastForwardOnly, false) || !is(m.RebaseUpdate, false) {
		t.Errorf("merge = %+v", m)
	}
	bogus := gitoperationslib.MergeMethod("bogus")
	if _, err := repos.Edit(ctx, admin, name, gitoperationslib.EditRepoOptions{Merge: gitoperationslib.MergeSettings{DefaultMethod: &bogus}}); gitoperationslib.StatusCode(err) != 422 {
		t.Errorf("an unknown merge method: %v", err)
	}
	if _, err := repos.Edit(ctx, admin, name, gitoperationslib.EditRepoOptions{Actions: gitoperationslib.ActionsSettings{Allowed: gitoperationslib.Ptr(gitoperationslib.ActionsAllowedAll)}}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Actions settings: %v", err)
	}
}
