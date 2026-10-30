package contracttest

import (
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Protections runs the branch and tag protection checks in a throwaway
// repository, including enforcement: protected branches cannot be deleted,
// and (on GitHub) protected tag patterns cannot be created.
func Protections(t *testing.T, env Env) {
	ctx := testContext(t)
	vis := gitoperationslib.VisibilityPrivate
	if env.PublicProtectionRepo {
		vis = gitoperationslib.VisibilityPublic
		t.Log("using a short-lived public repository (README only)")
	}
	created, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true, Visibility: vis})
	owner, def := env.Org, created.DefaultBranch
	bp, tp := env.Client.BranchProtections(), env.Client.TagProtections()
	isForgejo := env.Client.Backend() == gitoperationslib.BackendForgejo
	bb := isBitbucket(env)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// A rule on the default branch, and a glob rule.
	// Bitbucket (free plan): no named checks, no up-to-date requirement, and
	// resetting stale approvals needs Premium.
	want := gitoperationslib.BranchProtection{
		Pattern: def, RequiredApprovals: 1, DismissStaleApprovals: true,
		RequiredStatusChecks: []string{"ci/test"}, RequireUpToDate: true, ApplyToAdmins: true,
	}
	if bb {
		want.DismissStaleApprovals, want.RequiredStatusChecks, want.RequireUpToDate = false, nil, false
	}
	mainRule, err := bp.Create(ctx, owner, *repo, want)
	check(err)
	if mainRule.Pattern != def || mainRule.RequiredApprovals != 1 || mainRule.DismissStaleApprovals != want.DismissStaleApprovals ||
		!slices.Equal(mainRule.RequiredStatusChecks, want.RequiredStatusChecks) || mainRule.RequireUpToDate != want.RequireUpToDate ||
		!mainRule.ApplyToAdmins || mainRule.AllowDeletion || mainRule.AllowForcePush {
		t.Errorf("created = %+v", mainRule)
	}
	release, err := bp.Create(ctx, owner, *repo, gitoperationslib.BranchProtection{Pattern: "release/*", RequiredApprovals: 2, ApplyToAdmins: true})
	check(err)
	if release.Pattern != "release/*" || release.RequiredApprovals != 2 {
		t.Errorf("glob rule = %+v", release)
	}

	if bb {
		// Push and merge allowlists (Bitbucket stores users as UUIDs).
		hotfix, err := bp.Create(ctx, owner, *repo, gitoperationslib.BranchProtection{
			Pattern: "hotfix/*", PushAllowlist: &gitoperationslib.Allowlist{Users: []string{env.Login}},
			MergeAllowlist: &gitoperationslib.Allowlist{Users: []string{env.Login}},
		})
		check(err)
		if hotfix.PushAllowlist == nil || !slices.Equal(hotfix.PushAllowlist.Users, []string{env.Login}) ||
			hotfix.MergeAllowlist == nil || !slices.Equal(hotfix.MergeAllowlist.Users, []string{env.Login}) {
			t.Errorf("hotfix rule = %+v (allowlists %+v %+v)", hotfix, hotfix.PushAllowlist, hotfix.MergeAllowlist)
		}
	} else {
		// Exempting administrators (GitHub: a bypass for the admin role).
		hotfix, err := bp.Create(ctx, owner, *repo, gitoperationslib.BranchProtection{Pattern: "hotfix/*", RequireSignedCommits: true})
		check(err)
		if hotfix.ApplyToAdmins || !hotfix.RequireSignedCommits {
			t.Errorf("hotfix rule = %+v", hotfix)
		}
	}
	check(bp.Delete(ctx, owner, *repo, "hotfix/*"))

	got, err := bp.Get(ctx, owner, *repo, def)
	check(err)
	if got.RequiredApprovals != 1 || !slices.Equal(got.RequiredStatusChecks, want.RequiredStatusChecks) {
		t.Errorf("Get = %+v", got)
	}
	rules, err := bp.List(ctx, owner, *repo)
	check(err)
	var patterns []string
	for _, r := range rules {
		patterns = append(patterns, r.Pattern)
	}
	if !slices.Contains(patterns, def) || !slices.Contains(patterns, "release/*") {
		t.Errorf("List patterns = %v", patterns)
	}

	// Update replaces the rule.
	got.RequiredApprovals, got.RequiredStatusChecks, got.RequireUpToDate = 2, nil, false
	updated, err := bp.Update(ctx, owner, *repo, def, *got)
	check(err)
	if updated.RequiredApprovals != 2 || len(updated.RequiredStatusChecks) != 0 || updated.RequireUpToDate {
		t.Errorf("updated = %+v", updated)
	}

	// Enforcement: a branch matching the glob cannot be deleted.
	_, err = env.Client.Branches().Create(ctx, owner, *repo, gitoperationslib.CreateBranchOptions{Name: "release/1.0"})
	check(err)
	if err := env.Client.Branches().Delete(ctx, owner, *repo, "release/1.0"); err == nil {
		t.Error("deleting a protected branch succeeded")
	} else if !errors.Is(err, gitoperationslib.ErrBranchProtected) {
		t.Errorf("protected branch deletion refused, but not recognised as ErrBranchProtected: %v", err)
	}

	// Backend-specific options are reported.
	extra := gitoperationslib.BranchProtection{Pattern: "extra", AllowForcePush: true}
	if bb {
		extra = gitoperationslib.BranchProtection{Pattern: "extra", RequireSignedCommits: true}
	} else if !isForgejo {
		extra = gitoperationslib.BranchProtection{Pattern: "extra", BlockOnRejectedReviews: true}
	}
	if _, err := bp.Create(ctx, owner, *repo, extra); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("backend-specific option: err = %v, want ErrNotSupported", err)
	}

	check(bp.Delete(ctx, owner, *repo, "release/*"))
	if _, err := bp.Get(ctx, owner, *repo, "release/*"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get after delete: err = %v, want 404", err)
	}
	check(env.Client.Branches().Delete(ctx, owner, *repo, "release/1.0"))
	check(bp.Delete(ctx, owner, *repo, def))

	if unsupported(t, env, gitoperationslib.CapTagProtection, func() error {
		_, err := tp.Create(ctx, owner, *repo, gitoperationslib.TagProtection{Pattern: "v*"})
		return err
	}) {
		_, err = env.Client.Tags().Create(ctx, owner, *repo, gitoperationslib.CreateTagOptions{Name: "v9.9.9"})
		check(err)
		return
	}

	// Tag protection. Forgejo needs someone on the allowlist; GitHub cannot
	// list users, and an empty allowlist there blocks everyone.
	allow := gitoperationslib.Allowlist{}
	if isForgejo {
		allow.Users = []string{env.Login}
	}
	tag, err := tp.Create(ctx, owner, *repo, gitoperationslib.TagProtection{Pattern: "v*", Allowlist: allow})
	check(err)
	if tag.Pattern != "v*" || !slices.Equal(tag.Allowlist.Users, allow.Users) && len(allow.Users) > 0 {
		t.Errorf("tag rule = %+v", tag)
	}
	if got, err := tp.Get(ctx, owner, *repo, "v*"); err != nil || got.Pattern != "v*" {
		t.Errorf("Get tag rule = %+v, %v", got, err)
	}
	if list, err := tp.List(ctx, owner, *repo); err != nil || len(list) != 1 {
		t.Errorf("List tag rules = %+v, %v", list, err)
	}
	_, err = env.Client.Tags().Create(ctx, owner, *repo, gitoperationslib.CreateTagOptions{Name: "v9.9.9"})
	switch {
	case err != nil:
		t.Logf("protected tag creation refused: %v", err)
	case isForgejo: // the test user is on the allowlist
		check(env.Client.Tags().Delete(ctx, owner, *repo, "v9.9.9"))
	default:
		t.Error("creating a protected tag succeeded")
	}

	moved, err := tp.Update(ctx, owner, *repo, "v*", gitoperationslib.TagProtection{Pattern: "release-*", Allowlist: allow})
	check(err)
	if moved.Pattern != "release-*" {
		t.Errorf("updated tag rule = %+v", moved)
	}
	check(tp.Delete(ctx, owner, *repo, "release-*"))
	if _, err := tp.Get(ctx, owner, *repo, "release-*"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get tag rule after delete: err = %v, want 404", err)
	}
	_, err = env.Client.Tags().Create(ctx, owner, *repo, gitoperationslib.CreateTagOptions{Name: "v9.9.9"})
	check(err)
}
