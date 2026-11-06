package contracttest

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Diffs checks commit ranges, the author filter, raw diffs and what a pull
// request changes. A branch with two commits, one adding a file and one
// editing a file that exists on the default branch, is opened as a pull
// request.
func Diffs(t *testing.T, env Env) {
	h, isHistory := env.Client.(gitoperationslib.History)
	cd, isCommitDiffs := env.Client.(gitoperationslib.CommitDiffs)
	pc, isPRContents := env.Client.(gitoperationslib.PRContents)
	rc, isReviewComments := env.Client.(gitoperationslib.ReviewComments)
	caps := gitoperationslib.CapCommitDiffs | gitoperationslib.CapPRContents | gitoperationslib.CapReviewComments
	if !isHistory || !isCommitDiffs || !isPRContents || !isReviewComments || !env.Client.Capabilities().Has(caps) {
		t.Fatalf("%s does not implement the diff interfaces", env.Client.Backend())
	}
	if env.CreateBranch == nil || env.WriteFile == nil {
		t.Skip("no way to create commits")
	}
	ctx := testContext(t)
	backend := env.Client.Backend()
	repo, _ := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner, name, base := repo.Owner, repo.Name, repo.DefaultBranch
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const branch, added, edited = "feature/diff", "added.txt", "edited.txt"
	check(retryConflict(ctx, t, func() error { return env.WriteFile(ctx, owner, name, base, edited, "one\ntwo\nthree\n") }))
	check(env.CreateBranch(ctx, owner, name, branch, added, "new\n"))
	check(env.WriteFile(ctx, owner, name, branch, edited, "one\nTWO\nthree\nfour\n"))
	commits := h.Commits()

	var head *gitoperationslib.Commit
	if !eventually(ctx, func() bool {
		var err error
		head, err = commits.Get(ctx, owner, name, branch)
		return err == nil && len(head.Files) == 1 && head.Files[0].Path == edited
	}) {
		t.Fatalf("Get(%s) = %+v", branch, head)
	}
	first := head.Parents[0] // the commit adding a file

	t.Run("exclude", func(t *testing.T) {
		list := func(opts gitoperationslib.ListCommitsOptions) []string {
			t.Helper()
			page, err := commits.List(ctx, owner, name, opts)
			if err != nil {
				t.Fatalf("List(%+v): %v", opts, err)
			}
			shas := make([]string, len(page.Items))
			for i, c := range page.Items {
				shas[i] = c.SHA
			}
			return shas
		}
		want := []string{head.SHA, first}
		if got := list(gitoperationslib.ListCommitsOptions{Ref: branch, Exclude: base}); !slices.Equal(got, want) {
			t.Errorf("%s..%s = %v, want %v", base, branch, got, want)
		}
		if got := list(gitoperationslib.ListCommitsOptions{Ref: branch, Exclude: first}); !slices.Equal(got, want[:1]) {
			t.Errorf("exclude a commit = %v, want %v", got, want[:1])
		}
		// The default branch is empty relative to the branch that contains it.
		if got := list(gitoperationslib.ListCommitsOptions{Exclude: branch}); len(got) != 0 {
			t.Errorf("empty range = %v", got)
		}
		// Every page, newest first.
		var paged []string
		for c, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
			return commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{Ref: branch, Exclude: base, ListOptions: o})
		}) {
			check(err)
			paged = append(paged, c.SHA)
		}
		if !slices.Equal(paged, want) {
			t.Errorf("one commit per page = %v, want %v", paged, want)
		}
		withPath := gitoperationslib.ListCommitsOptions{Ref: branch, Exclude: base, Path: edited}
		if backend == gitoperationslib.BackendGitHub {
			if _, err := commits.List(ctx, owner, name, withPath); !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("Exclude with Path: err = %v, want ErrNotSupported", err)
			}
		} else if got := list(withPath); !slices.Equal(got, want[:1]) {
			t.Errorf("Exclude with Path = %v, want %v", got, want[:1])
		}
	})

	t.Run("author", func(t *testing.T) {
		opts := gitoperationslib.ListCommitsOptions{Ref: branch, Author: env.Login}
		page, err := commits.List(ctx, owner, name, opts)
		if backend != gitoperationslib.BackendGitHub {
			if !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("Author: err = %v, want ErrNotSupported", err)
			}
			return
		}
		check(err)
		if !slices.ContainsFunc(page.Items, func(c gitoperationslib.Commit) bool { return c.SHA == head.SHA }) {
			t.Errorf("Author %s = %+v, want %s among them", env.Login, page.Items, head.SHA)
		}
		opts.Author = "nobody@example.invalid"
		if page, err := commits.List(ctx, owner, name, opts); err != nil || len(page.Items) != 0 {
			t.Errorf("unknown author = %+v, %v", page.Items, err)
		}
	})

	read := func(t *testing.T, stream io.ReadCloser, err error) string {
		t.Helper()
		check(err)
		defer stream.Close()
		data, err := io.ReadAll(stream)
		check(err)
		return string(data)
	}

	t.Run("commit diff", func(t *testing.T) {
		for _, ref := range []string{head.SHA, branch} {
			stream, err := cd.CommitDiff(ctx, owner, name, ref, gitoperationslib.DiffUnified)
			diff := read(t, stream, err)
			if !strings.Contains(diff, "b/"+edited) || !strings.Contains(diff, "+TWO") || !strings.Contains(diff, "-two") || strings.Contains(diff, added) {
				t.Errorf("diff of %s = %q", ref, diff)
			}
		}
		stream, err := cd.CommitDiff(ctx, owner, name, head.SHA, gitoperationslib.DiffPatch)
		if patch := read(t, stream, err); !strings.HasPrefix(patch, "From ") || !strings.Contains(patch, "+TWO") {
			t.Errorf("patch = %q", patch)
		}
		if _, err := cd.CommitDiff(ctx, owner, name, "0123456789abcdef0123456789abcdef01234567", gitoperationslib.DiffUnified); !gitoperationslib.IsNotFound(err) {
			t.Errorf("unknown commit: err = %v, want 404", err)
		}
		if _, err := cd.CommitDiff(ctx, owner, name, head.SHA, "nope"); err == nil {
			t.Error("invalid format accepted")
		}
	})

	pr, err := env.Client.PullRequests().Create(ctx, owner, name, gitoperationslib.CreatePROptions{Title: "Diffs", Head: branch, Base: base})
	check(err)

	t.Run("pull request contents", func(t *testing.T) {
		stream, err := pc.PullRequestDiff(ctx, owner, name, pr.Number, gitoperationslib.DiffUnified)
		if diff := read(t, stream, err); !strings.Contains(diff, "b/"+added) || !strings.Contains(diff, "+TWO") {
			t.Errorf("diff = %q", diff)
		}
		stream, err = pc.PullRequestDiff(ctx, owner, name, pr.Number, gitoperationslib.DiffPatch)
		if patch := read(t, stream, err); !strings.HasPrefix(patch, "From ") {
			t.Errorf("patch = %q", patch)
		}
		if _, err := pc.PullRequestDiff(ctx, owner, name, pr.Number, "nope"); err == nil {
			t.Error("invalid format accepted")
		}
		if _, err := pc.PullRequestDiff(ctx, owner, name, 99999, gitoperationslib.DiffUnified); !gitoperationslib.IsNotFound(err) {
			t.Errorf("unknown pull request: err = %v, want 404", err)
		}

		files := map[string]gitoperationslib.ChangedFile{}
		for f, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: 1}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ChangedFile], error) {
			return pc.PullRequestFiles(ctx, owner, name, pr.Number, o)
		}) {
			check(err)
			files[f.Path] = f
		}
		if f := files[added]; len(files) != 2 || f.Status != gitoperationslib.FileAdded || f.Additions != 1 {
			t.Errorf("files = %+v", files)
		}
		if f := files[edited]; f.Status != gitoperationslib.FileModified || f.Additions != 2 || f.Deletions != 1 {
			t.Errorf("edited file = %+v, want modified, 2 additions, 1 deletion", f)
		}

		want := []string{head.SHA, first}
		for _, perPage := range []int{0, 1} {
			var got []string
			for c, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{PerPage: perPage}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Commit], error) {
				return pc.PullRequestCommits(ctx, owner, name, pr.Number, o)
			}) {
				check(err)
				if c.Message == "" || c.Files != nil {
					t.Errorf("commit = %+v", c)
				}
				got = append(got, c.SHA)
			}
			if !slices.Equal(got, want) {
				t.Errorf("commits with PerPage %d = %v, want %v", perPage, got, want)
			}
		}
	})

	t.Run("comment on several lines", func(t *testing.T) {
		c, err := rc.CreateReviewComment(ctx, owner, name, pr.Number, gitoperationslib.CreateReviewCommentOptions{Path: edited, Body: "on two lines", Line: 2, ExtraLines: 1})
		if errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Skip(err)
		}
		check(err)
		t.Cleanup(func() { _ = rc.DeleteReviewComment(context.Background(), owner, name, pr.Number, c.ID) })
		if c.Line != 2 || c.ExtraLines != 1 {
			t.Errorf("created = %+v", c)
		}
		page, err := rc.ListReviewComments(ctx, owner, name, pr.Number, gitoperationslib.ListOptions{})
		check(err)
		i := slices.IndexFunc(page.Items, func(l gitoperationslib.ReviewComment) bool { return l.ID == c.ID })
		if i < 0 || page.Items[i].Line != 2 || page.Items[i].ExtraLines != 1 {
			t.Errorf("listed = %+v", page.Items)
		}
		check(rc.DeleteReviewComment(ctx, owner, name, pr.Number, c.ID))
	})

	t.Run("review comments", func(t *testing.T) {
		list := func() []gitoperationslib.ReviewComment {
			t.Helper()
			var all []gitoperationslib.ReviewComment
			for c, err := range gitoperationslib.All(ctx, gitoperationslib.ListOptions{}, func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.ReviewComment], error) {
				return rc.ListReviewComments(ctx, owner, name, pr.Number, o)
			}) {
				check(err)
				all = append(all, c)
			}
			return all
		}
		if got := list(); len(got) != 0 {
			t.Errorf("before any comment: %+v", got)
		}
		for _, bad := range []gitoperationslib.CreateReviewCommentOptions{
			{Body: "b", Line: 1}, {Path: edited, Line: 1}, {Path: edited, Body: "b"}, {Path: edited, Body: "b", Line: 1, OldLine: 1},
		} {
			if _, err := rc.CreateReviewComment(ctx, owner, name, pr.Number, bad); err == nil {
				t.Errorf("Create(%+v) accepted", bad)
			}
		}

		onNew, err := rc.CreateReviewComment(ctx, owner, name, pr.Number, gitoperationslib.CreateReviewCommentOptions{Path: edited, Body: "on the new line", Line: 2})
		check(err)
		if onNew.ID == 0 || onNew.Path != edited || onNew.Line != 2 || onNew.OldLine != 0 || onNew.Body != "on the new line" || !strings.EqualFold(onNew.Author, env.Login) {
			t.Errorf("comment on a new line = %+v", onNew)
		}
		onOld, err := rc.CreateReviewComment(ctx, owner, name, pr.Number, gitoperationslib.CreateReviewCommentOptions{Path: edited, Body: "on the old line", OldLine: 2})
		check(err)
		if onOld.OldLine != 2 || onOld.Line != 0 {
			t.Errorf("comment on an old line = %+v", onOld)
		}

		byID := map[int64]gitoperationslib.ReviewComment{}
		for _, c := range list() {
			byID[c.ID] = c
		}
		if c := byID[onNew.ID]; len(byID) != 2 || c.Line != 2 || c.Path != edited || c.HTMLURL == "" || c.CreatedAt.IsZero() {
			t.Errorf("listed = %+v", byID)
		}
		if c := byID[onOld.ID]; c.OldLine != 2 || c.Line != 0 {
			t.Errorf("listed old-line comment = %+v", c)
		}

		commitBound := gitoperationslib.CreateReviewCommentOptions{Path: edited, Body: "on a commit", Line: 2, CommitID: head.SHA}
		c, err := rc.CreateReviewComment(ctx, owner, name, pr.Number, commitBound)
		if isBitbucket(env) {
			if !errors.Is(err, gitoperationslib.ErrNotSupported) {
				t.Errorf("CommitID: err = %v, want ErrNotSupported", err)
			}
		} else {
			check(err)
			if c.CommitID == "" {
				t.Errorf("comment on a commit = %+v", c)
			}
			check(rc.DeleteReviewComment(ctx, owner, name, pr.Number, c.ID))
		}

		check(rc.DeleteReviewComment(ctx, owner, name, pr.Number, onNew.ID))
		check(rc.DeleteReviewComment(ctx, owner, name, pr.Number, onOld.ID))
		if got := list(); len(got) != 0 {
			t.Errorf("after deleting: %+v", got)
		}
		if err := rc.DeleteReviewComment(ctx, owner, name, pr.Number, 99999999); !gitoperationslib.IsNotFound(err) {
			t.Errorf("delete an unknown comment: err = %v, want 404", err)
		}
	})
}
