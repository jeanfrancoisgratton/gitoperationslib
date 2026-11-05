package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// refServer has branch main (the default), tag v1 and commit abc123. A branch
// named "secret" answers 403. Created refs are recorded in *posted.
func refServer(t *testing.T) (*Client, *[]map[string]any, *[]string) {
	t.Helper()
	var posted []map[string]any
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		const base = "/2.0/repositories/acme/widget"
		switch r.Method + " " + strings.TrimPrefix(r.URL.Path, base) {
		case "GET ":
			io.WriteString(w, `{"slug":"widget","mainbranch":{"name":"main"}}`)
		case "GET /refs/branches/main":
			io.WriteString(w, `{"name":"main","target":{"hash":"branchhash","message":"Fix","date":"2026-10-17T10:00:00Z","author":{"raw":"Alice Doe <a@example.com>"}}}`)
		case "GET /refs/branches":
			io.WriteString(w, `{"values":[{"name":"main","target":{"hash":"branchhash"}},{"name":"dev","target":{"hash":"devhash"}}],"page":1,"next":"https://x/?page=2"}`)
		case "GET /refs/branches/secret":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":{"message":"forbidden"}}`)
		case "GET /refs/tags/v1":
			io.WriteString(w, `{"name":"v1","message":"Added tag v1 for changeset abc","target":{"hash":"taghash"}}`)
		case "GET /refs/tags":
			io.WriteString(w, `{"values":[{"name":"v1","message":"Release one\n","target":{"hash":"taghash"}}],"page":1}`)
		case "GET /commit/abc123":
			io.WriteString(w, `{"hash":"abc123fullhash"}`)
		case "POST /refs/branches", "POST /refs/tags":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			posted = append(posted, body)
			target, _ := body["target"].(map[string]any)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"name":%q,"message":%q,"target":{"hash":%q}}`, body["name"], fmt.Sprint(body["message"]), target["hash"])
		case "DELETE /refs/tags/release/1.0":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"not found"}}`)
		}
	})
	return c, &posted, &requests
}

func TestResolveCommitBitbucket(t *testing.T) {
	c, posted, _ := refServer(t)
	ctx := context.Background()
	for from, want := range map[string]string{
		"":       "branchhash", // the default branch
		"main":   "branchhash",
		"v1":     "taghash",
		"abc123": "abc123fullhash",
	} {
		*posted = nil
		br, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{Name: "new", From: from})
		if err != nil {
			t.Errorf("From %q: %v", from, err)
			continue
		}
		target := (*posted)[0]["target"].(map[string]any)
		if target["hash"] != want || br.SHA != want || br.Name != "new" {
			t.Errorf("From %q: posted %v, branch %+v, want %s", from, *posted, br, want)
		}
	}

	_, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{Name: "new", From: "nothing"})
	if !gitoperationslib.IsNotFound(err) || !strings.Contains(err.Error(), `"nothing" is not a branch, tag or commit`) {
		t.Errorf("unknown ref: err = %v", err)
	}
	// Only a 404 moves on to the next kind of ref.
	if _, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{Name: "new", From: "secret"}); !gitoperationslib.IsForbidden(err) {
		t.Errorf("forbidden ref: err = %v, want 403", err)
	}
}

func TestBranchesBitbucket(t *testing.T) {
	c, _, _ := refServer(t)
	ctx := context.Background()

	page, err := c.Branches().List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 2 || !page.Items[0].IsDefault || page.Items[1].IsDefault || page.NextPage != 2 {
		t.Errorf("List = %+v, %v", page, err)
	}
	br, err := c.Branches().Get(ctx, "acme", "widget", "main")
	if err != nil || !br.IsDefault || br.SHA != "branchhash" || br.LastCommit == nil || br.LastCommit.Author != "Alice Doe" || br.LastCommit.Message != "Fix" {
		t.Errorf("Get = %+v, %v", br, err)
	}
	if _, err := c.Branches().Get(ctx, "acme", "widget", "none"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("Get(none): err = %v, want 404", err)
	}
	if _, err := c.Branches().Rename(ctx, "acme", "widget", "main", "x"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Rename: err = %v, want ErrNotSupported", err)
	}
	if _, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{}); err == nil {
		t.Error("Create without a name accepted")
	}
}

func TestToBranchAuthor(t *testing.T) {
	date := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct{ raw, display, want string }{
		"git author":       {"Alice Doe <a@example.com>", "ignored", "Alice Doe"},
		"no address":       {"Alice Doe", "", "Alice Doe"},
		"only the account": {"", "Alice D.", "Alice D."},
	} {
		var r refJSON
		r.Target.Date = date
		r.Target.Author.Raw = tc.raw
		r.Target.Author.User.DisplayName = tc.display
		if got := toBranch(&r, "").LastCommit; got == nil || got.Author != tc.want {
			t.Errorf("%s: LastCommit = %+v, want author %q", name, got, tc.want)
		}
	}
	if got := toBranch(&refJSON{Name: "x"}, "").LastCommit; got != nil {
		t.Errorf("no commit date: LastCommit = %+v, want nil", got)
	}
}

func TestTagsBitbucket(t *testing.T) {
	c, posted, requests := refServer(t)
	ctx := context.Background()

	page, err := c.Tags().List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 1 || page.Items[0].SHA != "taghash" || !strings.Contains(page.Items[0].Message, "Release one") {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if got := page.Items[0].TarballURL; got != "https://bitbucket.org/acme/widget/get/v1.tar.gz" {
		t.Errorf("TarballURL = %s", got)
	}
	if got := page.Items[0].ZipballURL; got != "https://bitbucket.org/acme/widget/get/v1.zip" {
		t.Errorf("ZipballURL = %s", got)
	}

	// Bitbucket's generated annotation counts as no message.
	tag, err := c.Tags().Get(ctx, "acme", "widget", "v1")
	if err != nil || tag.Message != "" {
		t.Errorf("Get = %+v, %v, want an empty message", tag, err)
	}

	created, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v2", Target: "main", Message: "Two"})
	if err != nil || created.SHA != "branchhash" || created.Message != "Two" {
		t.Errorf("Create = %+v, %v", created, err)
	}
	if got := (*posted)[0]; got["message"] != "Two" || got["target"].(map[string]any)["hash"] != "branchhash" {
		t.Errorf("posted %v", got)
	}
	*posted = nil
	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v3"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*posted)[0]["message"]; ok {
		t.Errorf("no message: posted %v", (*posted)[0])
	}

	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{}); err == nil {
		t.Error("Create without a name accepted")
	}
	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v4", Target: "nothing"}); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown target: err = %v, want 404", err)
	}

	// A slash in a name stays inside one path segment.
	if err := c.Tags().Delete(ctx, "acme", "widget", "release/1.0"); err != nil {
		t.Error(err)
	}
	if !slices.Contains(*requests, "DELETE /2.0/repositories/acme/widget/refs/tags/release%2F1.0") {
		t.Errorf("requests = %v", *requests)
	}
}
