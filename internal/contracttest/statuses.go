package contracttest

import (
	"errors"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// CommitStatuses reports statuses on a new repository's first commit, and reads
// them back combined.
func CommitStatuses(t *testing.T, env Env) {
	if !env.Client.Capabilities().Has(gitoperationslib.CapCommitStatuses) {
		t.Skip("commit statuses are not supported")
	}
	s := env.Client.Commits()
	ctx := testContext(t)
	r, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	b, err := env.Client.Branches().Get(ctx, env.Org, *repo, r.DefaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	read := func(want gitoperationslib.StatusState, n int) *gitoperationslib.CombinedStatus {
		t.Helper()
		// GitHub's combined status can lag a second or two behind a write.
		for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(time.Second) {
			cs, err := s.Status(ctx, env.Org, *repo, r.DefaultBranch) // by branch name
			if err != nil {
				t.Fatal(err)
			}
			if cs.State == want && len(cs.Statuses) == n && (n == 0 || cs.SHA == b.SHA) {
				return cs
			}
			if time.Now().After(deadline) {
				t.Fatalf("combined = %+v, want %q with %d statuses", cs, want, n)
			}
		}
	}
	set := func(context string, state gitoperationslib.StatusState) {
		t.Helper()
		err := s.SetStatus(ctx, env.Org, *repo, b.SHA, gitoperationslib.CommitStatus{
			Context: context, State: state, Description: "gitopslib " + string(state), TargetURL: "https://example.com/build/1",
		})
		if err != nil {
			t.Fatalf("set %s %s: %v", context, state, err)
		}
	}

	read("", 0)
	set("gitopslib/a", gitoperationslib.StatusPending)
	set("gitopslib/b", gitoperationslib.StatusSuccess)
	read(gitoperationslib.StatusPending, 2)
	set("gitopslib/a", gitoperationslib.StatusSuccess) // replaces the pending one
	cs := read(gitoperationslib.StatusSuccess, 2)
	for _, st := range cs.Statuses {
		if st.Context == "gitopslib/a" && (st.State != gitoperationslib.StatusSuccess || st.TargetURL == "" || st.Description != "gitopslib success") {
			t.Errorf("status = %+v", st)
		}
	}
	set("gitopslib/b", gitoperationslib.StatusFailure)
	read(gitoperationslib.StatusFailure, 2)

	err = s.SetStatus(ctx, env.Org, *repo, b.SHA, gitoperationslib.CommitStatus{
		Context: "gitopslib/w", State: gitoperationslib.StatusWarning, TargetURL: "https://example.com/build/1",
	})
	if forgejo := env.Client.Backend() == gitoperationslib.BackendForgejo; forgejo != (err == nil) ||
		!forgejo && !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("a warning: err = %v", err)
	}
	if _, err := s.Status(ctx, env.Org, *repo, "no-such-branch"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("a missing ref: err = %v, want a 404", err)
	}
}
