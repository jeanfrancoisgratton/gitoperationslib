package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// BranchesAndTags runs the branch and tag checks in a throwaway repository.
func BranchesAndTags(t *testing.T, env Env) {
	ctx := testContext(t)
	created, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner := env.Org
	branches, tags := env.Client.Branches(), env.Client.Tags()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// The default branch exists and is flagged.
	var def *gitoperationslib.Branch
	check(retryConflict(ctx, t, func() (err error) {
		def, err = branches.Get(ctx, owner, *repo, created.DefaultBranch)
		return err
	}))
	if !def.IsDefault || def.SHA == "" {
		t.Errorf("default branch = %+v", def)
	}

	// Create from the default branch, from a commit, and from a branch.
	feature, err := branches.Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "feature"})
	check(err)
	if feature.SHA != def.SHA {
		t.Errorf("feature = %+v, want SHA %s", feature, def.SHA)
	}
	fromSHA, err := branches.Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "from-sha", From: def.SHA})
	check(err)
	fromBranch, err := branches.Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "from-branch", From: "feature"})
	check(err)
	if fromSHA.SHA != def.SHA || fromBranch.SHA != def.SHA {
		t.Errorf("from-sha = %+v, from-branch = %+v", fromSHA, fromBranch)
	}
	if _, err := branches.Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "x", From: "no-such-ref"}); err == nil {
		t.Error("creating from an unknown ref succeeded")
	}

	listed := map[string]gitoperationslib.Branch{}
	if !eventually(ctx, func() bool {
		for br, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 2}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Branch], error) {
			return branches.List(ctx, owner, *repo, o)
		}) {
			check(err)
			listed[br.Name] = br
		}
		return len(listed) == 4
	}) {
		t.Errorf("List = %v, want 4 branches", listed)
	}
	if !listed[created.DefaultBranch].IsDefault || listed["feature"].IsDefault {
		t.Errorf("IsDefault wrong in List: %v", listed)
	}

	featureName := "feature-renamed"
	renamed, err := branches.Rename(ctx, owner, *repo, "feature", featureName)
	switch {
	case isBitbucket(env) && errors.Is(err, gitoperationslib.ErrNotSupported):
		featureName = "feature" // Bitbucket cannot rename branches
	case err != nil:
		t.Fatal(err)
	case renamed.Name != featureName || renamed.SHA != def.SHA:
		t.Errorf("renamed = %+v", renamed)
	}
	check(branches.Delete(ctx, owner, *repo, "from-sha"))
	if _, err := branches.Get(ctx, owner, *repo, "from-sha"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}

	// Tags: lightweight on the default branch, annotated on a branch.
	light, err := tags.Create(ctx, owner, *repo, gitoperationslib.CreateTagOptions{Name: "v0.1.0"})
	check(err)
	if light.Name != "v0.1.0" || light.SHA != def.SHA || light.Message != "" {
		t.Errorf("lightweight tag = %+v", light)
	}
	annotated, err := tags.Create(ctx, owner, *repo, gitoperationslib.CreateTagOptions{
		Name: "v0.2.0", Target: featureName, Message: "Release 0.2.0",
	})
	check(err)
	if annotated.SHA != def.SHA || annotated.Message == "" {
		t.Errorf("annotated tag = %+v", annotated)
	}
	got, err := tags.Get(ctx, owner, *repo, "v0.2.0")
	check(err)
	if got.SHA != def.SHA || got.Message == "" || got.TarballURL == "" {
		t.Errorf("Get annotated = %+v", got)
	}
	// A branch can start from a tag (annotated tags are peeled).
	fromTag, err := branches.Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "from-tag", From: "v0.2.0"})
	check(err)
	if fromTag.SHA != def.SHA {
		t.Errorf("from-tag = %+v", fromTag)
	}
	names := map[string]bool{}
	if !eventually(ctx, func() bool {
		page, err := tags.List(ctx, owner, *repo, gitoperationslib.ListOptions{})
		check(err)
		for _, tg := range page.Items {
			names[tg.Name] = tg.SHA == def.SHA
		}
		return names["v0.1.0"] && names["v0.2.0"]
	}) {
		t.Errorf("List tags = %v", names)
	}

	// An existing tag can be released, and the tag outlives the release.
	if env.Client.Capabilities().Has(gitoperationslib.CapReleases) {
		rel, err := env.Client.Releases().Create(ctx, owner, *repo, gitoperationslib.CreateReleaseOptions{TagName: "v0.2.0", Name: "0.2.0"})
		check(err)
		check(env.Client.Releases().Delete(ctx, owner, *repo, rel.ID))
		if _, err := tags.Get(ctx, owner, *repo, "v0.2.0"); err != nil {
			t.Errorf("tag gone after deleting its release: %v", err)
		}
	}

	check(tags.Delete(ctx, owner, *repo, "v0.1.0"))
	if _, err := tags.Get(ctx, owner, *repo, "v0.1.0"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get tag after delete: err = %v, want 404", err)
	}
}
