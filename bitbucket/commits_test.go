package bitbucket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const bitbucketCommit = `{"hash": "024004fe", "message": "rename\n", "date": "2026-10-30T17:12:44+00:00",
	"author": {"raw": "Jean-François Gratton <jf@example.com>", "user": {"nickname": "jfg"}},
	"parents": [{"hash": "a7723785"}], "links": {"html": {"href": "https://bitbucket.org/acme/widget/commits/024004fe"}}}`

func TestCommitsBitbucket(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch r.URL.EscapedPath() {
		case "/2.0/repositories/acme/widget":
			io.WriteString(w, `{"slug": "widget", "full_name": "acme/widget", "workspace": {"slug": "acme"}, "mainbranch": {"name": "main"}}`)
		case "/2.0/repositories/acme/widget/commits/main", "/2.0/repositories/acme/widget/commits/feature%2Flog":
			io.WriteString(w, `{"values": [`+bitbucketCommit+`], "next": "https://api.bitbucket.org/2.0/x?page=2&ctx=abc"}`)
		case "/2.0/repositories/acme/widget/commit/feature%2Flog":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"type": "error", "error": {"message": "Commit not found"}}`)
		case "/2.0/repositories/acme/widget/diffstat/024004fe":
			io.WriteString(w, `{"values": [
				{"status": "renamed", "lines_added": 1, "lines_removed": 1, "old": {"path": "old.txt"}, "new": {"path": "new.txt"}},
				{"status": "removed", "lines_added": 0, "lines_removed": 3, "old": {"path": "gone.txt"}, "new": null}]}`)
		default:
			t.Errorf("unexpected %s", r.URL.EscapedPath())
		}
	})
	ctx := context.Background()

	// No ref: the default branch, not every branch.
	page, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Path: "a.txt"})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	got := page.Items[0]
	if got.Author.Name != "Jean-François Gratton" || got.Author.Email != "jf@example.com" || got.Author.Login != "jfg" ||
		got.Parents[0] != "a7723785" || !got.Author.Date.Equal(time.Date(2026, 10, 30, 17, 12, 44, 0, time.UTC)) {
		t.Errorf("listed commit = %+v", got)
	}
	if requests[1] != "/2.0/repositories/acme/widget/commits/main?path=a.txt" {
		t.Errorf("List request %s", requests[1])
	}

	// A branch with a slash: /commit 404s, the list resolves it.
	commit, err := c.Commits().Get(ctx, "acme", "widget", "feature/log")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitoperationslib.ChangedFile{
		{Path: "new.txt", PreviousPath: "old.txt", Status: gitoperationslib.FileRenamed, Additions: 1, Deletions: 1},
		{Path: "gone.txt", Status: gitoperationslib.FileRemoved, Deletions: 3},
	}
	if commit.SHA != "024004fe" || len(commit.Files) != 2 || commit.Files[0] != want[0] || commit.Files[1] != want[1] ||
		*commit.Stats != (gitoperationslib.CommitStats{Additions: 1, Deletions: 4}) {
		t.Errorf("Get = %+v", commit)
	}

	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Until: time.Now()}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Until: err = %v, want ErrNotSupported", err)
	}
}

func TestCommitAuthorWithoutAddress(t *testing.T) {
	j := commitJSON{Hash: "h"}
	j.Author.Raw = "just a name"
	if a := j.toCommit().Author; a.Name != "just a name" || a.Email != "" {
		t.Errorf("Author = %+v", a)
	}
}
