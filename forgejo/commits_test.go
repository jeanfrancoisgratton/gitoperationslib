package forgejo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const forgejoCommit = `{"sha": "34b982dd", "html_url": "https://git.example/acme/widget/commit/34b982dd",
	"commit": {"message": "modify, delete\n",
		"author": {"name": "Admin", "email": "a@example.com", "date": "2026-10-30T17:12:07Z"},
		"committer": {"name": "Bot", "email": "b@example.com", "date": "2026-10-30T17:13:00Z"}},
	"author": {"login": "admin"}, "committer": null,
	"parents": [{"sha": "2fd845e8"}],
	"stats": {"total": 4, "additions": 2, "deletions": 2},
	"files": [{"filename": "a.txt", "status": "modified"}, {"filename": "dir/b.txt", "status": "removed"}]}`

func TestCommitsForgejo(t *testing.T) {
	var queries []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.EscapedPath() {
		case "/api/v1/repos/acme/widget/commits":
			w.Header().Set("Link", `<https://git.example/api/v1/repos/acme/widget/commits?page=2>; rel="next"`)
			io.WriteString(w, "["+forgejoCommit+"]")
		case "/api/v1/repos/acme/widget/git/commits/feature%2Flog":
			io.WriteString(w, forgejoCommit)
		default:
			t.Errorf("unexpected %s", r.URL.EscapedPath())
		}
	})
	ctx := context.Background()

	page, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Ref: "develop", Path: "a.txt"})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	got := page.Items[0]
	if got.SHA != "34b982dd" || got.Author.Login != "admin" || got.Committer.Name != "Bot" || got.Committer.Login != "" ||
		got.Parents[0] != "2fd845e8" || got.Files != nil || got.Stats != nil || !got.Author.Date.Equal(time.Date(2026, 10, 30, 17, 12, 7, 0, time.UTC)) {
		t.Errorf("listed commit = %+v", got)
	}
	if q := queries[0]; q != "/api/v1/repos/acme/widget/commits?files=false&path=a.txt&sha=develop&stat=false&verification=false" {
		t.Errorf("List query %s", q)
	}

	commit, err := c.Commits().Get(ctx, "acme", "widget", "feature/log")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Stats == nil || commit.Stats.Additions != 2 || len(commit.Files) != 2 ||
		commit.Files[1] != (gitoperationslib.ChangedFile{Path: "dir/b.txt", Status: gitoperationslib.FileRemoved}) {
		t.Errorf("Get = %+v", commit)
	}

	n := len(queries)
	if _, err := c.Commits().List(ctx, "acme", "widget", gitoperationslib.ListCommitsOptions{Since: time.Now()}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Since: err = %v, want ErrNotSupported", err)
	}
	if _, err := c.Commits().Get(ctx, "acme", "widget", ""); err == nil {
		t.Error("empty ref accepted")
	}
	if len(queries) != n {
		t.Errorf("refused calls sent %v", queries[n:])
	}
}
