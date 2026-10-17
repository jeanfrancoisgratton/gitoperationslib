package contracttest

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// PullRequests runs the pull request checks in a throwaway repository. All
// pull requests are opened by the authenticated user, so approving reviews
// (not allowed on one's own pull request) are left to backend-specific
// tests.
func PullRequests(t *testing.T, env Env) {
	if env.CreateBranch == nil {
		t.Skip("no CreateBranch helper for this backend")
	}
	ctx := testContext(t)
	created, repo := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner := env.Org
	prs := env.Client.PullRequests()
	isForgejo := env.Client.Backend() == gitoperationslib.BackendForgejo
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	base := created.DefaultBranch
	if base == "" {
		got, err := env.Client.Repos().Get(ctx, owner, *repo)
		check(err)
		base = got.DefaultBranch
	}
	branch := func(name string) {
		t.Helper()
		check(retryConflict(ctx, t, func() error {
			return env.CreateBranch(ctx, owner, *repo, name, name+".txt", "content of "+name+"\n")
		}))
	}
	open := func(head string, opts gitoperationslib.CreatePROptions) *gitoperationslib.PullRequest {
		t.Helper()
		branch(head)
		opts.Head, opts.Base = head, base
		pr, err := prs.Create(ctx, owner, *repo, opts)
		check(err)
		return pr
	}

	// Open, read, list, edit.
	pr := open("feature-1", gitoperationslib.CreatePROptions{Title: "Feature 1", Body: "First feature"})
	if pr.Number == 0 || pr.State != gitoperationslib.PRStateOpen || pr.Draft || pr.Merged ||
		pr.Head.Ref != "feature-1" || pr.Base.Ref != base || pr.Head.SHA == "" || !strings.EqualFold(pr.Author, env.Login) {
		t.Errorf("created = %+v", pr)
	}
	got, err := prs.Get(ctx, owner, *repo, pr.Number)
	check(err)
	if got.Title != "Feature 1" || got.Body != "First feature" || got.HTMLURL == "" {
		t.Errorf("Get = %+v", got)
	}
	if !eventually(ctx, func() bool {
		page, err := prs.List(ctx, owner, *repo, gitoperationslib.ListPROptions{})
		check(err)
		return len(page.Items) == 1 && page.Items[0].Number == pr.Number
	}) {
		t.Error("List(open) did not return the pull request")
	}
	edited, err := prs.Edit(ctx, owner, *repo, pr.Number, gitoperationslib.EditPROptions{
		Title: gitoperationslib.Ptr("Feature 1, edited"), Body: gitoperationslib.Ptr("Edited body"),
	})
	check(err)
	if edited.Title != "Feature 1, edited" || edited.Body != "Edited body" {
		t.Errorf("edited = %+v", edited)
	}
	if merged, err := prs.IsMerged(ctx, owner, *repo, pr.Number); err != nil || merged {
		t.Errorf("IsMerged(open) = %v, %v", merged, err)
	}

	// A comment review by the author is allowed; approving one's own is not.
	review, err := prs.CreateReview(ctx, owner, *repo, pr.Number, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventComment, Body: "Looks fine"})
	check(err)
	if review.State != gitoperationslib.ReviewCommented || !strings.EqualFold(review.Reviewer, env.Login) {
		t.Errorf("review = %+v", review)
	}
	reviews, err := prs.ListReviews(ctx, owner, *repo, pr.Number, gitoperationslib.ListOptions{})
	check(err)
	if len(reviews.Items) == 0 {
		t.Error("ListReviews returned nothing")
	}
	if _, err := prs.CreateReview(ctx, owner, *repo, pr.Number, gitoperationslib.CreateReviewOptions{Event: gitoperationslib.ReviewEventApprove}); err == nil {
		t.Error("approving one's own pull request succeeded")
	} else {
		t.Logf("self-approval: %v", err)
	}

	// Drafts cannot be merged.
	draft := open("feature-2", gitoperationslib.CreatePROptions{Title: "Feature 2", Draft: true})
	if !draft.Draft {
		t.Errorf("draft = %+v", draft)
	}
	if isForgejo && draft.Title != "WIP: Feature 2" {
		t.Errorf("Forgejo draft title = %q, want the raw prefixed title", draft.Title)
	}
	if err := prs.Merge(ctx, owner, *repo, draft.Number, gitoperationslib.MergeOptions{}); err == nil {
		t.Error("merging a draft succeeded")
	}
	ready, err := prs.Edit(ctx, owner, *repo, draft.Number, gitoperationslib.EditPROptions{Draft: gitoperationslib.Ptr(false)})
	switch {
	case isForgejo:
		check(err)
		if ready.Draft || ready.Title != "Feature 2" {
			t.Errorf("ready = %+v", ready)
		}
	case !errors.Is(err, gitoperationslib.ErrNotSupported):
		t.Errorf("Draft edit on %s: err = %v, want ErrNotSupported", env.Client.Backend(), err)
	}

	// Close and reopen.
	closed, err := prs.Edit(ctx, owner, *repo, draft.Number, gitoperationslib.EditPROptions{State: gitoperationslib.Ptr(gitoperationslib.PRStateClosed)})
	check(err)
	if closed.State != gitoperationslib.PRStateClosed || closed.Merged {
		t.Errorf("closed = %+v", closed)
	}
	reopened, err := prs.Edit(ctx, owner, *repo, draft.Number, gitoperationslib.EditPROptions{State: gitoperationslib.Ptr(gitoperationslib.PRStateOpen)})
	check(err)
	if reopened.State != gitoperationslib.PRStateOpen {
		t.Errorf("reopened = %+v", reopened)
	}
	_, err = prs.Edit(ctx, owner, *repo, draft.Number, gitoperationslib.EditPROptions{State: gitoperationslib.Ptr(gitoperationslib.PRStateClosed)})
	check(err)

	// Every merge method the backend supports, each on a fresh branch.
	methods := []gitoperationslib.MergeMethod{gitoperationslib.MergeMethodMerge, gitoperationslib.MergeMethodSquash, gitoperationslib.MergeMethodRebase}
	if isForgejo {
		methods = append(methods, gitoperationslib.MergeMethodRebaseMerge, gitoperationslib.MergeMethodFastForwardOnly)
	} else if err := prs.Merge(ctx, owner, *repo, pr.Number, gitoperationslib.MergeOptions{Method: gitoperationslib.MergeMethodFastForwardOnly}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("fast-forward-only on %s: err = %v, want ErrNotSupported", env.Client.Backend(), err)
	}
	for i, method := range methods {
		target := pr
		if i > 0 {
			target = open("merge-"+string(method), gitoperationslib.CreatePROptions{Title: "Merge with " + string(method)})
		}
		cur, err := prs.Get(ctx, owner, *repo, target.Number)
		check(err)
		err = retryConflict(ctx, t, func() error {
			return prs.Merge(ctx, owner, *repo, target.Number, gitoperationslib.MergeOptions{
				Method: method, HeadSHA: cur.Head.SHA, DeleteBranch: method == gitoperationslib.MergeMethodSquash,
				Title: "Merged with " + string(method),
			})
		})
		if err != nil {
			t.Errorf("merge #%d with %s: %v", target.Number, method, err)
			continue
		}
		merged, err := prs.Get(ctx, owner, *repo, target.Number)
		check(err)
		if !merged.Merged || merged.State != gitoperationslib.PRStateClosed || merged.MergedAt.IsZero() ||
			!strings.EqualFold(merged.MergedBy, env.Login) {
			t.Errorf("%s: after merge = %+v", method, merged)
		}
		if ok, err := prs.IsMerged(ctx, owner, *repo, target.Number); err != nil || !ok {
			t.Errorf("%s: IsMerged = %v, %v", method, ok, err)
		}
	}

	if !eventually(ctx, func() bool {
		page, err := prs.List(ctx, owner, *repo, gitoperationslib.ListPROptions{State: gitoperationslib.PRStateClosed})
		check(err)
		return len(page.Items) == len(methods)+1
	}) {
		t.Errorf("List(closed) did not return %d pull requests", len(methods)+1)
	}
}
