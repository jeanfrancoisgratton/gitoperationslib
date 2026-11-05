package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const bbPull = "/2.0/repositories/acme/widget/pullrequests/5"

func TestCommitsExcludeAndAuthorBitbucket(t *testing.T) {
	var query string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("exclude")
		io.WriteString(w, `{"values": []}`)
	})
	ctx := context.Background()
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "topic", Exclude: "main"}); err != nil || query != "main" {
		t.Errorf("exclude = %q, %v", query, err)
	}
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "topic", Author: "jf"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Author: err = %v, want ErrNotSupported", err)
	}
}

func TestRawDiffsBitbucket(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath())
		switch r.URL.EscapedPath() {
		case "/2.0/repositories/acme/widget/commit/topic":
			io.WriteString(w, bitbucketCommit)
		case "/2.0/repositories/acme/widget/commit/gone":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"type": "error", "error": {"message": "Commit not found"}}`)
		case "/2.0/repositories/acme/widget/commits/gone":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"type": "error", "error": {"message": "not found"}}`)
		default:
			if r.Header.Get("Accept") != "text/plain" {
				t.Errorf("Accept = %q", r.Header.Get("Accept"))
			}
			io.WriteString(w, "diff --git")
		}
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
	read(c.CommitDiff(ctx, "acme", "widget", "topic", gitoperationslib.DiffUnified))
	read(c.CommitDiff(ctx, "acme", "widget", "topic", gitoperationslib.DiffPatch))
	read(c.PullRequestDiff(ctx, "acme", "widget", 5, gitoperationslib.DiffUnified))
	read(c.PullRequestDiff(ctx, "acme", "widget", 5, gitoperationslib.DiffPatch))
	want := []string{
		"/2.0/repositories/acme/widget/commit/topic", "/2.0/repositories/acme/widget/diff/024004fe",
		"/2.0/repositories/acme/widget/commit/topic", "/2.0/repositories/acme/widget/patch/024004fe",
		bbPull + "/diff", bbPull + "/patch",
	}
	if !slices.Equal(requests, want) {
		t.Errorf("requests = %q, want %q", requests, want)
	}

	requests = nil
	if _, err := c.CommitDiff(ctx, "acme", "widget", "", gitoperationslib.DiffUnified); err == nil {
		t.Error("empty ref accepted")
	}
	if _, err := c.CommitDiff(ctx, "acme", "widget", "a", "html"); err == nil {
		t.Error("invalid format accepted")
	}
	if _, err := c.PullRequestDiff(ctx, "acme", "widget", 5, "html"); err == nil {
		t.Error("invalid format accepted")
	}
	if len(requests) != 0 {
		t.Errorf("requests sent: %q", requests)
	}
	if _, err := c.CommitDiff(ctx, "acme", "widget", "gone", gitoperationslib.DiffUnified); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown commit: err = %v, want 404", err)
	}
}

func TestPullRequestFilesAndCommitsBitbucket(t *testing.T) {
	var pages []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case bbPull + "/diffstat":
			io.WriteString(w, `{"values": [{"status": "renamed", "lines_added": 1, "lines_removed": 2, "old": {"path": "old.txt"}, "new": {"path": "new.txt"}}],
				"next": "https://api.bitbucket.org/2.0/x?page=2"}`)
		case bbPull + "/commits":
			// Page tokens, not page numbers: only the next link gets further.
			pages = append(pages, r.URL.Query().Get("page"))
			if r.URL.Query().Get("page") == "" {
				io.WriteString(w, `{"values": [`+bitbucketCommit+`], "next": "https://api.bitbucket.org/2.0/x?pagelen=100&page=67Fg"}`)
				return
			}
			io.WriteString(w, `{"values": [`+bitbucketCommit+`]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	files, err := c.PullRequestFiles(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{})
	want := []gitoperationslib.ChangedFile{{Path: "new.txt", PreviousPath: "old.txt", Status: gitoperationslib.FileRenamed, Additions: 1, Deletions: 2}}
	if err != nil || files.NextPage != 2 || !reflect.DeepEqual(files.Items, want) {
		t.Errorf("files = %+v, %v", files, err)
	}
	page, err := c.PullRequestCommits(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{PerPage: 1})
	if err != nil || len(page.Items) != 2 || page.NextPage != 0 || page.Items[0].SHA != "024004fe" {
		t.Errorf("commits = %+v, %v", page, err)
	}
	if !slices.Equal(pages, []string{"", "67Fg"}) {
		t.Errorf("pages = %q", pages)
	}
}

func TestReviewCommentsBitbucket(t *testing.T) {
	var created map[string]any
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET " + bbPull + "/comments":
			io.WriteString(w, `{"values": [
				{"id": 1, "content": {"raw": "general"}, "user": {"nickname": "jf"}},
				{"id": 2, "content": {"raw": "new"}, "user": {"nickname": "jf"}, "inline": {"path": "a.txt", "from": null, "to": 2},
				 "created_on": "2026-11-05T10:00:00+00:00", "links": {"html": {"href": "https://h/2"}}},
				{"id": 3, "content": {"raw": "old"}, "user": {"nickname": "jf"}, "inline": {"path": "a.txt", "from": 3, "to": null}},
				{"id": 4, "content": {"raw": ""}, "deleted": true, "inline": {"path": "a.txt", "to": 9}}]}`)
		case "POST " + bbPull + "/comments":
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id": 9, "content": {"raw": "b"}, "user": {"nickname": "jf"}, "inline": {"path": "a.txt", "from": 3}}`)
		case "DELETE " + bbPull + "/comments/9":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	page, err := c.ListReviewComments(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{PerPage: 1})
	if err != nil || len(page.Items) != 2 || page.NextPage != 0 {
		t.Fatalf("list = %+v, %v; want the live line comments only", page, err)
	}
	if got := page.Items[0]; got.ID != 2 || got.Line != 2 || got.OldLine != 0 || got.Author != "jf" || got.Path != "a.txt" || got.HTMLURL == "" || got.CreatedAt.IsZero() {
		t.Errorf("new line = %+v", got)
	}
	if got := page.Items[1]; got.ID != 3 || got.OldLine != 3 || got.Line != 0 {
		t.Errorf("old line = %+v", got)
	}

	rc, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "b", OldLine: 3})
	if err != nil || rc.ID != 9 || rc.OldLine != 3 {
		t.Fatalf("create = %+v, %v", rc, err)
	}
	want := map[string]any{"content": map[string]any{"raw": "b"}, "inline": map[string]any{"path": "a.txt", "from": float64(3)}}
	if !reflect.DeepEqual(created, want) {
		t.Errorf("body = %v, want %v", created, want)
	}
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "b", Line: 2}); err != nil {
		t.Fatal(err)
	}
	if inline := created["inline"].(map[string]any); inline["to"] != float64(2) || inline["from"] != nil {
		t.Errorf("new line: inline = %v", inline)
	}

	requests = nil
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "b", Line: 2, CommitID: "c"}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("CommitID: err = %v, want ErrNotSupported", err)
	}
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt"}); err == nil {
		t.Error("invalid options accepted")
	}
	if len(requests) != 0 {
		t.Errorf("requests sent: %q", requests)
	}
	if err := c.DeleteReviewComment(ctx, "acme", "widget", 5, 9); err != nil {
		t.Error(err)
	}
}
