package contracttest

import (
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/branchinfo"
)

// BranchInfo reads every server part of branchinfo on a new repository with
// two branches at the default branch's commit, one with a status.
func BranchInfo(t *testing.T, env Env) {
	ctx := testContext(t)
	r, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	for _, b := range [][2]string{{"bi-a", r.DefaultBranch}, {"bi-b", "bi-a"}} {
		if _, err := env.Client.Branches().Create(ctx, env.Org, *repo, gitoperationslib.CreateBranchOptions{Name: b[0], From: b[1]}); err != nil {
			t.Fatal(err)
		}
	}
	head, err := env.Client.Branches().Get(ctx, env.Org, *repo, r.DefaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	s, statuses := env.Client.Commits(), env.Client.Capabilities().Has(gitoperationslib.CapCommitStatuses)
	if statuses {
		if err := s.SetStatus(ctx, env.Org, *repo, head.SHA, gitoperationslib.CommitStatus{
			Context: "gitopslib/bi", State: gitoperationslib.StatusSuccess, TargetURL: "https://example.com/build/1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := branchinfo.Collect(ctx, branchinfo.Options{
		Client: env.Client, Owner: env.Org, Repo: *repo,
		AheadBehind: branchinfo.Server, LastCommit: branchinfo.Server, Parent: branchinfo.Server,
		PullRequests: true, Statuses: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Incomplete != nil {
		t.Logf("incomplete: %v", res.Incomplete)
	}
	if res.Default != r.DefaultBranch || len(res.Branches) != 3 {
		t.Fatalf("result = %+v", res)
	}
	for _, b := range res.Branches {
		if b.Server == nil || b.Local != nil || b.ServerAhead == nil || b.ServerAhead.Ahead != 0 || !*b.ServerMerged ||
			b.Server.LastCommit == nil || len(b.PullRequests) != 0 || b.Default != (b.Name == r.DefaultBranch) {
			t.Errorf("%s: %+v", b.Name, b)
		}
		if statuses && (b.Status == nil || b.Status.State != gitoperationslib.StatusSuccess) {
			t.Errorf("%s: status %+v", b.Name, b.Status)
		}
		// Three branches at one commit: each has the other two as equal guesses.
		if p := b.ServerParent; p == nil || p.Exact || len(p.Others) != 1 || p.Branch == b.Name {
			t.Errorf("%s: parent %+v", b.Name, p)
		}
	}
}
