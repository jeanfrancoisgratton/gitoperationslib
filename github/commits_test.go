package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestCommitsGitHub(t *testing.T) {
	var queries []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		const head = `"sha": "c2", "html_url": "https://github.com/acme/widget/commit/c2",
			"commit": {"message": "rename", "author": {"name": "Jean", "email": "j@example.com", "date": "2026-10-30T12:00:00Z"},
				"committer": {"name": "GitHub", "email": "noreply@github.com", "date": "2026-10-30T12:01:00Z"}},
			"author": {"login": "jean"}, "committer": {"login": "web-flow"}, "parents": [{"sha": "c1"}]`
		switch {
		case r.URL.Path == "/api/v3/repos/acme/widget/commits":
			io.WriteString(w, "[{"+head+"}]")
		case r.URL.Path == "/api/v3/repos/acme/widget/commits/feature/log" && r.URL.Query().Get("page") == "":
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=2&per_page=300>; rel="next"`, "http://"+r.Host+r.URL.Path))
			io.WriteString(w, "{"+head+`, "stats": {"additions": 3, "deletions": 1, "total": 4},
				"files": [{"filename": "new.txt", "previous_filename": "old.txt", "status": "renamed", "additions": 1, "deletions": 1}]}`)
		case r.URL.Path == "/api/v3/repos/acme/widget/commits/feature/log":
			io.WriteString(w, "{"+head+`, "files": [{"filename": "b.txt", "status": "added", "additions": 2}]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	page, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "develop", Path: "a.txt", Since: since})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.Author.Login != "jean" || got.Committer.Login != "web-flow" || got.Parents[0] != "c1" || got.Files != nil {
		t.Errorf("listed commit = %+v", got)
	}
	if q := queries[0]; q != "/api/v3/repos/acme/widget/commits?path=a.txt&sha=develop&since=2026-10-01T00%3A00%3A00Z" {
		t.Errorf("List query %s", q)
	}

	commit, err := c.Commits().Get(ctx, "acme", "widget", "feature/log")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.ChangedFile{
		{Path: "new.txt", PreviousPath: "old.txt", Status: gitoperationslib.FileRenamed, Additions: 1, Deletions: 1},
		{Path: "b.txt", Status: gitoperationslib.FileAdded, Additions: 2},
	}
	if len(commit.Files) != 2 || commit.Files[0] != want[0] || commit.Files[1] != want[1] || commit.Stats.Additions != 3 {
		t.Errorf("Get = %+v (files from both pages wanted)", commit)
	}
}
