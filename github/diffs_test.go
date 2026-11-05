package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	gh "github.com/google/go-github/v93/github"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestNewestFirst(t *testing.T) {
	// Commits c1 (oldest) to c7, which the server pages oldest first.
	const total = 7
	fetches := 0
	fetch := func(lo gh.ListOptions) ([]*gh.RepositoryCommit, error) {
		fetches++
		var list []*gh.RepositoryCommit
		for i := (lo.Page - 1) * lo.PerPage; i < lo.Page*lo.PerPage && i < total; i++ {
			list = append(list, &gh.RepositoryCommit{SHA: gh.Ptr("c" + strconv.Itoa(i+1))})
		}
		return list, nil
	}
	shas := func(p gitoperationslib.Page[gitoperationslib.Commit]) string {
		var s []string
		for _, c := range p.Items {
			s = append(s, c.SHA)
		}
		return strings.Join(s, " ")
	}
	tests := []struct {
		opts     gitoperationslib.ListOptions
		want     string
		next     int
		requests int
	}{
		{gitoperationslib.ListOptions{}, "c7 c6 c5 c4 c3 c2 c1", 0, 1},
		{gitoperationslib.ListOptions{PerPage: 7}, "c7 c6 c5 c4 c3 c2 c1", 0, 1},
		{gitoperationslib.ListOptions{PerPage: 3}, "c7 c6 c5", 2, 2},
		{gitoperationslib.ListOptions{PerPage: 3, Page: 2}, "c4 c3 c2", 3, 2},
		{gitoperationslib.ListOptions{PerPage: 3, Page: 3}, "c1", 0, 1},
		{gitoperationslib.ListOptions{PerPage: 3, Page: 4}, "", 0, 0},
		{gitoperationslib.ListOptions{PerPage: 1, Page: 7}, "c1", 0, 1},
	}
	for _, tt := range tests {
		fetches = 0
		got, err := newestFirst(total, tt.opts, fetch)
		if err != nil {
			t.Fatal(err)
		}
		if s := shas(got); s != tt.want || got.NextPage != tt.next || fetches != tt.requests {
			t.Errorf("%+v: %q next %d after %d requests, want %q next %d after %d", tt.opts, s, got.NextPage, fetches, tt.want, tt.next, tt.requests)
		}
	}

	if got, err := newestFirst(0, gitoperationslib.ListOptions{}, fetch); err != nil || len(got.Items) != 0 || got.Items == nil {
		t.Errorf("nothing: %+v, %v", got, err)
	}
	boom := errors.New("boom")
	if _, err := newestFirst(total, gitoperationslib.ListOptions{}, func(gh.ListOptions) ([]*gh.RepositoryCommit, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	// Fewer commits than announced (a force-push meanwhile) must not panic.
	short := func(gh.ListOptions) ([]*gh.RepositoryCommit, error) { return nil, nil }
	if got, err := newestFirst(total, gitoperationslib.ListOptions{PerPage: 3}, short); err != nil || len(got.Items) != 0 {
		t.Errorf("short list: %+v, %v", got, err)
	}
}

func commitsJSON(shas ...string) string {
	var items []string
	for _, s := range shas {
		items = append(items, fmt.Sprintf(`{"sha": %q, "commit": {"message": "m"}}`, s))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestCommitsExcludeGitHub(t *testing.T) {
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/v3/repos/acme/widget":
			io.WriteString(w, `{"default_branch": "develop"}`)
		case "/api/v3/repos/acme/widget/compare/main...topic", "/api/v3/repos/acme/widget/compare/main...develop":
			if r.URL.Query().Get("page") == "" { // the request for the total
				io.WriteString(w, `{"total_commits": 2, "commits": `+commitsJSON("old")+`}`)
				return
			}
			io.WriteString(w, `{"total_commits": 2, "commits": `+commitsJSON("old", "new")+`}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	page, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "topic", Exclude: "main"})
	if err != nil || len(page.Items) != 2 || page.Items[0].SHA != "new" || page.Items[1].SHA != "old" {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if !strings.Contains(requests[0], "per_page=1") {
		t.Errorf("first request = %s", requests[0])
	}

	// No ref: the default branch.
	requests = nil
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Exclude: "main"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(requests[1], "/api/v3/repos/acme/widget/compare/main...develop") {
		t.Errorf("requests = %q", requests)
	}

	// A comparison has no such filters.
	requests = nil
	for _, opts := range []gitoperationslib.ListCommitsOptions{{Exclude: "main", Path: "a"}, {Exclude: "main", Author: "jf"}} {
		if _, err := c.Commits().List(ctx, "acme", "widget", opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%+v: err = %v, want ErrNotSupported", opts, err)
		}
	}
	if len(requests) != 0 {
		t.Errorf("requests sent: %q", requests)
	}
}

func TestCommitsAuthorGitHub(t *testing.T) {
	var author string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		author = r.URL.Query().Get("author")
		io.WriteString(w, "[]")
	})
	if _, err := c.Commits().List(context.Background(), "acme", "widget", gitoperationslib.ListCommitsOptions{Author: "jf"}); err != nil || author != "jf" {
		t.Errorf("author = %q, %v", author, err)
	}
}

func TestRawDiffsGitHub(t *testing.T) {
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+" "+r.Header.Get("Accept"))
		if strings.Contains(r.URL.Path, "gone") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message": "No commit found for SHA: gone"}`)
			return
		}
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message": "something else"}`)
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
	read(c.CommitDiff(ctx, "acme", "widget", "abc", gitoperationslib.DiffUnified))
	read(c.PullRequestDiff(ctx, "acme", "widget", 5, gitoperationslib.DiffPatch))
	if !strings.Contains(requests[0], "/commits/abc") || !strings.Contains(requests[0], "diff") || !strings.Contains(requests[1], "/pulls/5") || !strings.Contains(requests[1], "patch") {
		t.Errorf("requests = %q", requests)
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
	// GitHub answers 422 for an unknown commit; the library reports a 404.
	if _, err := c.CommitDiff(ctx, "acme", "widget", "gone", gitoperationslib.DiffUnified); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown commit: err = %v, want 404", err)
	}
	if _, err := c.Commits().Get(ctx, "acme", "widget", "gone"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(unknown commit): err = %v, want 404", err)
	}
	if _, err := c.CommitDiff(ctx, "acme", "widget", "bad", gitoperationslib.DiffUnified); gitoperationslib.StatusCode(err) != http.StatusUnprocessableEntity {
		t.Errorf("other refusal: err = %v, want 422", err)
	}
}

func TestPullRequestFilesAndCommitsGitHub(t *testing.T) {
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/acme/widget/pulls/5/files":
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
			io.WriteString(w, `[{"filename": "new.txt", "previous_filename": "old.txt", "status": "renamed", "additions": 1, "deletions": 2}]`)
		case "/api/v3/repos/acme/widget/pulls/5":
			io.WriteString(w, `{"number": 5, "commits": 300}`)
		case "/api/v3/repos/acme/widget/pulls/5/commits":
			// GitHub lists 250 of the 300: the page must be taken from there.
			if got := r.URL.Query().Get("page"); got != "9" && got != "8" {
				t.Errorf("page = %q, want the last pages of 250 commits", got)
			}
			io.WriteString(w, commitsJSON(slices.Repeat([]string{"c"}, 30)...))
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
	page, err := c.PullRequestCommits(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 30 || page.NextPage != 2 {
		t.Errorf("commits: %d items, next %d, %v", len(page.Items), page.NextPage, err)
	}
}

func TestReviewCommentsGitHub(t *testing.T) {
	var created gh.CreatePullRequestCommentRequest
	var requests []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/repos/acme/widget/pulls/5":
			io.WriteString(w, `{"number": 5, "head": {"sha": "headsha"}}`)
		case "GET /api/v3/repos/acme/widget/pulls/5/comments":
			io.WriteString(w, `[
				{"id": 1, "body": "new", "path": "a.txt", "line": 2, "side": "RIGHT", "commit_id": "c1", "pull_request_review_id": 7,
				 "html_url": "https://h/1", "user": {"login": "jf"}, "created_at": "2026-11-05T10:00:00Z"},
				{"id": 2, "body": "old", "path": "a.txt", "line": 3, "side": "LEFT"},
				{"id": 3, "body": "outdated", "path": "a.txt", "line": null, "original_line": 4, "side": "RIGHT"}]`)
		case "POST /api/v3/repos/acme/widget/pulls/5/comments":
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id": 9, "body": %q, "path": %q, "line": %d, "side": %q}`, created.Body, created.Path, *created.Line, *created.Side)
		case "DELETE /api/v3/repos/acme/widget/pulls/comments/9":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()
	page, err := c.ListReviewComments(ctx, "acme", "widget", 5, gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.Line != 2 || got.OldLine != 0 || got.Author != "jf" || got.CommitID != "c1" || got.ReviewID != 7 || got.CreatedAt.IsZero() {
		t.Errorf("new line = %+v", got)
	}
	if got := page.Items[1]; got.OldLine != 3 || got.Line != 0 {
		t.Errorf("old line = %+v", got)
	}
	if got := page.Items[2]; got.Line != 4 {
		t.Errorf("outdated = %+v", got)
	}

	// No commit given: the pull request's head, which costs one request.
	rc, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "b", OldLine: 3})
	if err != nil || rc.ID != 9 || rc.OldLine != 3 || rc.Line != 0 {
		t.Fatalf("create = %+v, %v", rc, err)
	}
	if created.CommitID != "headsha" || *created.Side != "LEFT" || *created.Line != 3 {
		t.Errorf("request = %+v", created)
	}
	requests = nil
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt", Body: "b", Line: 2, CommitID: "given"}); err != nil {
		t.Fatal(err)
	}
	if created.CommitID != "given" || *created.Side != "RIGHT" || len(requests) != 1 {
		t.Errorf("with a commit: %+v after %q", created, requests)
	}
	if _, err := c.CreateReviewComment(ctx, "acme", "widget", 5, gitoperationslib.CreateReviewCommentOptions{Path: "a.txt"}); err == nil {
		t.Error("invalid options accepted")
	}
	if err := c.DeleteReviewComment(ctx, "acme", "widget", 5, 9); err != nil {
		t.Error(err)
	}
}
