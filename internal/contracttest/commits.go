package contracttest

import (
	"errors"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Commits checks the commit history of a throwaway repository: a branch
// whose name needs escaping, with one commit adding a file on top of the
// initial one.
func Commits(t *testing.T, env Env) {
	h, ok := env.Client.(gitoperationslib.History)
	if !ok || !env.Client.Capabilities().Has(gitoperationslib.CapCommits) {
		t.Fatalf("%s does not implement History", env.Client.Backend())
	}
	if env.CreateBranch == nil {
		t.Skip("no way to create a commit")
	}
	ctx := testContext(t)
	repo, _ := create(t, ctx, env, gitoperationslib.CreateRepoOptions{Owner: env.Org, AutoInit: true})
	owner, name := repo.Owner, repo.Name
	const branch, file = "feature/log", "log.txt"
	if err := env.CreateBranch(ctx, owner, name, branch, file, "one\ntwo\n"); err != nil {
		t.Fatal(err)
	}
	commits := h.Commits()

	// The branch head, by branch name.
	var head *gitoperationslib.Commit
	if !eventually(ctx, func() bool {
		var err error
		head, err = commits.Get(ctx, owner, name, branch)
		return err == nil && len(head.Files) > 0
	}) {
		t.Fatalf("Get(%s) = %+v", branch, head)
	}
	if head.SHA == "" || head.Message == "" || len(head.Parents) != 1 || head.Author.Name == "" || head.Author.Date.IsZero() || head.HTMLURL == "" {
		t.Errorf("Get returned %+v", head)
	}
	if len(head.Files) != 1 || head.Files[0].Path != file || head.Files[0].Status != gitoperationslib.FileAdded {
		t.Errorf("Files = %+v, want %s added", head.Files, file)
	}
	if head.Stats == nil || head.Stats.Additions != 2 || head.Stats.Deletions != 0 {
		t.Errorf("Stats = %+v, want 2 additions", head.Stats)
	}
	// Forgejo reports no per-file line counts.
	if env.Client.Backend() != gitoperationslib.BackendForgejo && head.Files[0].Additions != 2 {
		t.Errorf("file additions = %d, want 2", head.Files[0].Additions)
	}

	// The same commit by SHA, and its parent.
	if got, err := commits.Get(ctx, owner, name, head.SHA); err != nil || got.SHA != head.SHA {
		t.Errorf("Get(%s) = %+v, %v", head.SHA, got, err)
	}
	if parent, err := commits.Get(ctx, owner, name, head.Parents[0]); err != nil || len(parent.Parents) != 0 {
		t.Errorf("initial commit = %+v, %v", parent, err)
	}

	// History of the branch: newest first, without files.
	page, err := commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{Ref: branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].SHA != head.SHA || page.Items[1].SHA != head.Parents[0] || page.Items[0].Files != nil {
		t.Errorf("List(%s) = %+v", branch, page.Items)
	}
	// The default branch lacks the new commit.
	if page, err := commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{}); err != nil || len(page.Items) != 1 || page.Items[0].SHA != head.Parents[0] {
		t.Errorf("List(default branch) = %+v, %v", page.Items, err)
	}
	if page, err := commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{Ref: branch, Path: file}); err != nil || len(page.Items) != 1 || page.Items[0].SHA != head.SHA {
		t.Errorf("List(path %s) = %+v, %v", file, page.Items, err)
	}
	// One commit per page.
	first, err := commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{Ref: branch, ListOptions: gitoperationslib.ListOptions{PerPage: 1}})
	if err != nil || len(first.Items) != 1 || first.NextPage == 0 {
		t.Fatalf("page 1 = %+v, %v", first, err)
	}
	second, err := commits.List(ctx, owner, name, gitoperationslib.ListCommitsOptions{Ref: branch, ListOptions: gitoperationslib.ListOptions{PerPage: 1, Page: first.NextPage}})
	if err != nil || len(second.Items) != 1 || second.Items[0].SHA != head.Parents[0] {
		t.Errorf("page 2 = %+v, %v", second, err)
	}

	if _, err := commits.Get(ctx, owner, name, "0123456789abcdef0123456789abcdef01234567"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(unknown SHA): err = %v, want 404", err)
	}
	since := gitoperationslib.ListCommitsOptions{Ref: branch, Since: time.Now().Add(time.Hour)}
	page, err = commits.List(ctx, owner, name, since)
	if env.Client.Backend() == gitoperationslib.BackendGitHub {
		if err != nil || len(page.Items) != 0 {
			t.Errorf("List(since in an hour) = %+v, %v; want nothing", page.Items, err)
		}
	} else if !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("List(Since): err = %v, want ErrNotSupported", err)
	}
}
