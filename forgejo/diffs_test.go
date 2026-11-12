package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCommitsExcludeAndAuthor(t *testing.T) {
	var query string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("not") + "|" + r.URL.Query().Get("sha")
		io.WriteString(w, `[]`)
	})
	ctx := context.Background()
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "topic", Exclude: "main"}); err != nil {
		t.Fatal(err)
	}
	if query != "main|topic" {
		t.Errorf("not|sha = %q", query)
	}
	query = ""
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Author: "jf"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Author: err = %v, want ErrNotSupported", err)
	}
	if query != "" {
		t.Error("Author: a request was sent, and the server would ignore the filter")
	}
}

func TestRawDiffs(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath())
		if r.Header.Get("Accept") != "text/plain" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if strings.Contains(r.URL.Path, "gone") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, "diff --git")
	})
	ctx := context.Background()
	read := func(stream io.ReadCloser, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		if data, _ := io.ReadAll(stream); string(data) != "diff --git" {
			t.Errorf("body = %q", data)
		}
	}
	read(c.Commits().Diff(ctx, "acme", "widget", "abc123", gitoperationslib.DiffUnified))
	read(c.Commits().Diff(ctx, "acme", "widget", "feature/log", gitoperationslib.DiffPatch))
	read(c.PullRequests().Diff(ctx, "acme", "widget", 5, gitoperationslib.DiffUnified))
	read(c.PullRequests().Diff(ctx, "acme", "widget", 5, gitoperationslib.DiffPatch))
	want := []string{
		"/api/v1/repos/acme/widget/git/commits/abc123.diff",
		"/api/v1/repos/acme/widget/git/commits/feature%2Flog.patch",
		"/api/v1/repos/acme/widget/pulls/5.diff",
		"/api/v1/repos/acme/widget/pulls/5.patch",
	}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %q, want %q", requests, want)
	}

	requests = nil
	if _, err := c.Commits().Diff(ctx, "acme", "widget", "", gitoperationslib.DiffUnified); err == nil {
		t.Error("empty ref accepted")
	}
	if _, err := c.Commits().Diff(ctx, "acme", "widget", "a", "html"); err == nil {
		t.Error("invalid commit format accepted")
	}
	if _, err := c.PullRequests().Diff(ctx, "acme", "widget", 5, "html"); err == nil {
		t.Error("invalid pull request format accepted")
	}
	if len(requests) != 0 {
		t.Errorf("requests sent for invalid arguments: %q", requests)
	}
	if _, err := c.Commits().Diff(ctx, "acme", "widget", "gone", gitoperationslib.DiffUnified); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown commit: err = %v, want 404", err)
	}
}

func TestPullRequestFilesAndCommits(t *testing.T) {
	var commitsQuery string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/widget/pulls/5/files":
			w.Header().Set("X-Total-Count", "3")
			io.WriteString(w, `[
				{"filename": "a.txt", "status": "changed", "additions": 2, "deletions": 1},
				{"filename": "b.txt", "status": "added", "additions": 1},
				{"filename": "new.txt", "previous_filename": "old.txt", "status": "renamed"}]`)
		case "/api/v1/repos/acme/widget/pulls/5/commits":
			commitsQuery = r.URL.Query().Get("files") + "|" + r.URL.Query().Get("verification")
			w.Header().Set("X-Total-Count", "3")
			io.WriteString(w, `[{"sha": "c2", "commit": {"message": "two"}, "stats": {"additions": 9}, "files": [{"filename": "x"}]}]`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	files, err := c.PullRequests().Files(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.ChangedFile{
		{Path: "a.txt", Status: gitoperationslib.FileModified, Additions: 2, Deletions: 1},
		{Path: "b.txt", Status: gitoperationslib.FileAdded, Additions: 1},
		{Path: "new.txt", PreviousPath: "old.txt", Status: gitoperationslib.FileRenamed},
	}
	if !reflect.DeepEqual(files.Items, want) {
		t.Errorf("files = %+v, want %+v", files.Items, want)
	}
	page, err := c.PullRequests().Commits(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{PerPage: 1})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("commits = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.SHA != "c2" || got.Files != nil || got.Stats != nil {
		t.Errorf("commit = %+v, want no files or stats", got)
	}
	if commitsQuery != "false|false" {
		t.Errorf("files|verification = %q", commitsQuery)
	}
}

// reviewCommentServer has review 1 with comment 11 on a new line, review 2
// with comment 12 on an old line, and review 3 without comments.
func reviewCommentServer(t *testing.T) (*Client, *[]string, *map[string]any) {
	t.Helper()
	var writes []string
	var created map[string]any
	const pr = "/api/v1/repos/acme/widget/pulls/5/reviews"
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			writes = append(writes, r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == pr:
			writes = append(writes, r.Method+" "+r.URL.Path)
			json.NewDecoder(r.Body).Decode(&created)
			io.WriteString(w, `{"id": 2}`)
		case r.URL.Path == pr:
			w.Header().Set("X-Total-Count", "3")
			io.WriteString(w, `[{"id": 2, "comments_count": 1}, {"id": 1, "comments_count": 1}, {"id": 3, "comments_count": 0}]`)
		case r.URL.Path == pr+"/1/comments":
			io.WriteString(w, `[{"id": 11, "body": "new", "path": "a.txt", "position": 2, "commit_id": "c1", "pull_request_review_id": 1,
				"html_url": "https://h/c11", "user": {"login": "jf"}, "created_at": "2026-11-05T10:00:00Z"}]`)
		case r.URL.Path == pr+"/2/comments":
			io.WriteString(w, `[{"id": 12, "body": "old", "path": "a.txt", "original_position": 3, "pull_request_review_id": 2}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	return c, &writes, &created
}

func TestListReviewComments(t *testing.T) {
	c, _, _ := reviewCommentServer(t)
	page, err := c.PullRequests().ListReviewComments(context.Background(), "acme", "widget", 5, gitoperationslib.ListOptions{PerPage: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextPage != 0 {
		t.Fatalf("page = %+v, want both comments at once", page)
	}
	first, second := page.Items[0], page.Items[1]
	if first.ID != 11 || first.Line != 2 || first.OldLine != 0 || first.Author != "jf" || first.CommitID != "c1" || first.ReviewID != 1 || first.HTMLURL == "" || first.CreatedAt.IsZero() {
		t.Errorf("first = %+v", first)
	}
	if second.ID != 12 || second.OldLine != 3 || second.Line != 0 || second.ReviewID != 2 {
		t.Errorf("second = %+v", second)
	}
}

func TestCreateReviewComment(t *testing.T) {
	c, _, created := reviewCommentServer(t)
	ctx := context.Background()
	rc, err := c.PullRequests().CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "old", OldLine: 3, CommitID: "c9"})
	if err != nil || rc.ID != 12 || rc.OldLine != 3 {
		t.Fatalf("Create = %+v, %v", rc, err)
	}
	want := map[string]any{
		"event": "COMMENT", "commit_id": "c9",
		"comments": []any{map[string]any{"path": "a.txt", "body": "old", "old_position": float64(3)}},
	}
	if !reflect.DeepEqual(*created, want) {
		t.Errorf("body = %v, want %v", *created, want)
	}
	if _, err := c.PullRequests().CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt"}); err == nil {
		t.Error("invalid options accepted")
	}
}

func TestDeleteReviewComment(t *testing.T) {
	c, writes, _ := reviewCommentServer(t)
	ctx := context.Background()
	if err := c.PullRequests().DeleteReviewComment(ctx, "acme", "widget", 5, 11); err != nil {
		t.Fatal(err)
	}
	// Through its review: DELETE /issues/comments/{id} answers 204 and keeps it.
	if want := []string{"DELETE /api/v1/repos/acme/widget/pulls/5/reviews/1/comments/11"}; !slices.Equal(*writes, want) {
		t.Errorf("writes = %q, want %q", *writes, want)
	}
	*writes = nil
	if err := c.PullRequests().DeleteReviewComment(ctx, "acme", "widget", 5, 99); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown comment: err = %v, want 404", err)
	}
	if len(*writes) != 0 {
		t.Errorf("writes for an unknown comment: %q", *writes)
	}
}
