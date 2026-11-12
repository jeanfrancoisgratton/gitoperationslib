//go:build integration

package forgejo

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
	"github.com/jeanfrancoisgratton/gitoperationslib/internal/contracttest"
)

// TestIntegrationGitHooks installs a hook that refuses pushes on a real server,
// and checks it does so. Only the disposable instance allows editing git hooks.
func TestIntegrationGitHooks(t *testing.T) {
	ctx, c := adminEnv(t)
	me, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + "githooks"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), me, repo) })
	created, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo, AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}
	const hook = "pre-receive"
	push := func(file string) error {
		body := map[string]string{"content": base64.StdEncoding.EncodeToString([]byte("x\n")), "message": "Add " + file, "branch": created.DefaultBranch}
		_, err := c.api.Do(ctx, http.MethodPost, repoPath(me, repo, "contents", file), nil, body, nil)
		return err
	}

	list, err := c.GitHooks().List(ctx, me, repo)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(list))
	for i, h := range list {
		names[i] = h.Name
		if h.Active {
			t.Errorf("%s in use in a new repository", h.Name)
		}
	}
	if !slices.Contains(names, hook) {
		t.Errorf("listed %v", names)
	}
	if _, err := c.GitHooks().Get(ctx, me, repo, "nosuch"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(nosuch) = %v", err)
	}

	const script = "#!/bin/sh\necho refused by the test hook >&2\nexit 1\n"
	set, err := c.GitHooks().Set(ctx, me, repo, hook, script)
	if err != nil {
		t.Fatal(err)
	}
	if !set.Active || set.Content != script {
		t.Errorf("Set = %+v", set)
	}
	if got, err := c.GitHooks().Get(ctx, me, repo, hook); err != nil || !got.Active || got.Content != script {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if err := push("refused.txt"); err == nil || !strings.Contains(err.Error(), "refused by the test hook") {
		t.Errorf("push with the hook in use = %v", err)
	}

	for range 2 { // deleting is idempotent
		if err := c.GitHooks().Delete(ctx, me, repo, hook); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := c.GitHooks().Get(ctx, me, repo, hook); err != nil || got.Active || got.Content != "" {
		t.Errorf("Get after delete = %+v, %v", got, err)
	}
	if err := push("allowed.txt"); err != nil {
		t.Errorf("push after delete: %v", err)
	}
}

// TestIntegrationAdminTypedDeliveries subscribes to everything and reads each
// new typed payload from a real delivery. A second user reviews, as authors
// cannot approve their own pull requests.
func TestIntegrationAdminTypedDeliveries(t *testing.T) {
	ctx, c := adminEnv(t)
	secret := []byte("typed-secret")
	recv := newHookReceiver(t, map[string][]byte{"/all": secret})
	admin, err := c.currentLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := testLogin(t)
	cleanupUser(t, c, &reviewer)
	if _, err := c.Users().Create(ctx, gitoperationslib.CreateUserOptions{Login: reviewer, Email: reviewer + "@example.invalid", Password: "Passw0rd-long-1", MustChangePassword: gitoperationslib.Ptr(false)}); err != nil {
		t.Fatal(err)
	}
	repo := contracttest.RepoPrefix + "typedhooks"
	t.Cleanup(func() { c.Repos().Delete(context.Background(), admin, repo) })
	created, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: repo, AutoInit: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collaborators().Add(ctx, admin, repo, reviewer, gitoperationslib.CollaboratorWrite); err != nil {
		t.Fatal(err)
	}
	scope := gitoperationslib.RepoHooks(admin, repo)
	hook, err := c.Webhooks().Create(ctx, scope, gitoperationslib.CreateWebhookOptions{
		URL: recv.srv.URL + "/all", Secret: string(secret), Events: []gitoperationslib.HookEvent{gitoperationslib.HookEventAll},
	})
	if err != nil {
		t.Fatalf("subscribing to everything: %v", err)
	}
	if len(hook.Events) < 10 {
		t.Errorf("subscribed to %v", hook.Events)
	}

	issue, err := c.Issues().Create(ctx, admin, repo, gitoperationslib.CreateIssueOptions{Title: "Typed", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	issues, err := recv.wait(t, "/all", "issues").Issues()
	if err != nil {
		t.Fatal(err)
	}
	if issues.Action != "opened" || issues.Issue.Number != issue.Number || issues.Issue.Title != "Typed" || issues.Issue.Author != admin {
		t.Errorf("issues = %+v", issues)
	}

	comment, err := c.Issues().CreateComment(ctx, admin, repo, issue.Number, "said")
	if err != nil {
		t.Fatal(err)
	}
	said, err := recv.wait(t, "/all", "issue_comment").IssueComment()
	if err != nil {
		t.Fatal(err)
	}
	if said.Action != "created" || said.IsPullRequest || said.Comment.ID != comment.ID || said.Comment.Body != "said" || said.Issue.Number != issue.Number {
		t.Errorf("comment = %+v", said)
	}

	if err := c.createBranch(ctx, admin, repo, "typed", "typed.txt", "x\n"); err != nil {
		t.Fatal(err)
	}
	made, err := recv.wait(t, "/all", "create").Create()
	if err != nil {
		t.Fatal(err)
	}
	if made.RefType != "branch" || made.Ref != "typed" || made.SHA == "" {
		t.Errorf("create = %+v", made)
	}

	pr, err := c.PullRequests().Create(ctx, admin, repo, gitoperationslib.CreatePROptions{Title: "Typed PR", Head: "typed", Base: created.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	asReviewer, err := New(ctx, os.Getenv("GITOPSLIB_TESTENV_URL"), Token(os.Getenv("GITOPSLIB_TESTENV_ADMIN_TOKEN")), WithSudo(reviewer))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		event gitoperationslib.ReviewEvent
		state gitoperationslib.ReviewState
	}{
		{gitoperationslib.ReviewEventApprove, gitoperationslib.ReviewApproved},
		{gitoperationslib.ReviewEventRequestChanges, gitoperationslib.ReviewChangesRequested},
		{gitoperationslib.ReviewEventComment, gitoperationslib.ReviewCommented},
	} {
		if _, err := asReviewer.PullRequests().CreateReview(ctx, admin, repo, pr.Number, gitoperationslib.CreateReviewOptions{Event: tt.event, Body: string(tt.state)}); err != nil {
			t.Fatal(err)
		}
		ev := recv.wait(t, "/all", "pull_request_review")
		review, err := ev.Review()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Action != "submitted" || review.Number != pr.Number || review.Review.State != tt.state || review.Review.Reviewer != reviewer || review.Review.Body != string(tt.state) {
			t.Errorf("review = %+v", review)
		}
	}

	if err := c.Branches().Delete(ctx, admin, repo, "typed"); err != nil {
		t.Fatal(err)
	}
	gone, err := recv.wait(t, "/all", "delete").Delete()
	if err != nil {
		t.Fatal(err)
	}
	if gone.RefType != "branch" || gone.Ref != "typed" {
		t.Errorf("delete = %+v", gone)
	}
}
