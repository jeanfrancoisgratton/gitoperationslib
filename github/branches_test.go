package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// gitServer fakes a repository with default branch main (commit c1), branch
// dev (c2), lightweight tag v1 (c1) and annotated tag v2 (tag object t9 on
// c2); writes are recorded.
func gitServer(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var writes []string
	c := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Method != http.MethodGet {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, fmt.Sprintf("%s %s %v", r.Method, p, body))
		}
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case p == "/api/v3/repos/acme/widget":
			io.WriteString(w, `{"name":"widget","default_branch":"main"}`)
		case p == "/api/v3/repos/acme/widget/git/ref/heads/main":
			io.WriteString(w, `{"ref":"refs/heads/main","object":{"type":"commit","sha":"c1"}}`)
		case p == "/api/v3/repos/acme/widget/git/ref/heads/dev":
			io.WriteString(w, `{"ref":"refs/heads/dev","object":{"type":"commit","sha":"c2"}}`)
		case p == "/api/v3/repos/acme/widget/git/ref/tags/v1":
			io.WriteString(w, `{"ref":"refs/tags/v1","object":{"type":"commit","sha":"c1"}}`)
		case p == "/api/v3/repos/acme/widget/git/ref/tags/v2":
			io.WriteString(w, `{"ref":"refs/tags/v2","object":{"type":"tag","sha":"t9"}}`)
		case p == "/api/v3/repos/acme/widget/git/tags/t9":
			io.WriteString(w, `{"sha":"t9","tag":"v2","message":"Release 2\n","object":{"type":"commit","sha":"c2"}}`)
		case p == "/api/v3/repos/acme/widget/git/commits/c3":
			io.WriteString(w, `{"sha":"c3"}`)
		case p == "/api/v3/repos/acme/widget/git/tags" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"sha":"t9"}`)
		case p == "/api/v3/repos/acme/widget/git/refs" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"ref":"refs/x"}`)
		case p == "/api/v3/repos/acme/widget/branches/main":
			io.WriteString(w, `{"name":"main","commit":{"sha":"c1","commit":{"message":"Fix the build","author":{"name":"Alice","date":"2026-10-20T14:00:00Z"}}},"protected":true}`)
		case p == "/api/v3/repos/acme/widget/compare/main...feature/x":
			if r.URL.Query().Get("per_page") != "1" {
				t.Errorf("compare asked for per_page=%q", r.URL.Query().Get("per_page"))
			}
			io.WriteString(w, `{"status":"diverged","ahead_by":3,"behind_by":1,"total_commits":3}`)
		case p == "/api/v3/repos/acme/widget/branches/old":
			w.Header().Set("Location", "/api/v3/repos/acme/widget/branches/new")
			w.WriteHeader(http.StatusMovedPermanently)
		case p == "/api/v3/repos/acme/widget/branches":
			io.WriteString(w, `[{"name":"main","commit":{"sha":"c1"}},{"name":"dev","commit":{"sha":"c2"}}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	})
	return c, &writes
}

func TestResolveCommit(t *testing.T) {
	c, _ := gitServer(t)
	ctx := context.Background()
	for ref, want := range map[string]string{"": "c1", "dev": "c2", "v1": "c1", "v2": "c2", "c3": "c3"} {
		got, err := c.resolveCommit(ctx, "acme", "widget", ref)
		if err != nil || got != want {
			t.Errorf("resolveCommit(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	if _, err := c.resolveCommit(ctx, "acme", "widget", "nope"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown ref: err = %v, want 404", err)
	}
}

func TestBranchesGitHub(t *testing.T) {
	c, writes := gitServer(t)
	ctx := context.Background()

	list, err := c.Branches().List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil || len(list.Items) != 2 || !list.Items[0].IsDefault || list.Items[1].IsDefault {
		t.Fatalf("List = %+v, %v", list, err)
	}
	main, err := c.Branches().Get(ctx, "acme", "widget", "main")
	if err != nil || !main.IsDefault || !main.Protected || main.SHA != "c1" {
		t.Errorf("Get = %+v, %v", main, err)
	}
	// The list carries the commit SHA alone; Get has the commit itself
	if list.Items[0].LastCommit != nil {
		t.Errorf("List's LastCommit = %+v, want nil", list.Items[0].LastCommit)
	}
	if lc := main.LastCommit; lc == nil || lc.Author != "Alice" || lc.Message != "Fix the build" || lc.Date.Year() != 2026 {
		t.Errorf("Get's LastCommit = %+v", lc)
	}
	cmp, err := c.Branches().Compare(ctx, "acme", "widget", "main", "feature/x")
	if err != nil || *cmp != (gitoperationslib.Comparison{Base: "main", Head: "feature/x", Ahead: 3, Behind: 1}) {
		t.Errorf("Compare = %+v, %v", cmp, err)
	}
	if _, err := c.Branches().Compare(ctx, "acme", "widget", "main", "nope"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown head: err = %v, want 404", err)
	}
	if _, err := c.Branches().Get(ctx, "acme", "widget", "missing"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("missing branch: err = %v, want 404 APIError", err)
	}
	if _, err := c.Branches().Get(ctx, "acme", "widget", "old"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("renamed branch: err = %v, want 404 APIError", err)
	}

	br, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{Name: "feature", From: "v2"})
	if err != nil || br.SHA != "c2" {
		t.Fatalf("Create = %+v, %v", br, err)
	}
	if err := c.Branches().Delete(ctx, "acme", "widget", "feature"); err != nil {
		t.Fatal(err)
	}
	want := "[POST /api/v3/repos/acme/widget/git/refs map[ref:refs/heads/feature sha:c2] DELETE /api/v3/repos/acme/widget/git/refs/heads/feature map[]]"
	if fmt.Sprint(*writes) != want {
		t.Errorf("writes = %v\nwant     %v", *writes, want)
	}
}

func TestTagsGitHub(t *testing.T) {
	c, writes := gitServer(t)
	ctx := context.Background()

	v2, err := c.Tags().Get(ctx, "acme", "widget", "v2")
	if err != nil || v2.SHA != "c2" || v2.Message != "Release 2\n" || v2.TarballURL == "" {
		t.Errorf("Get annotated = %+v, %v", v2, err)
	}
	v1, err := c.Tags().Get(ctx, "acme", "widget", "v1")
	if err != nil || v1.SHA != "c1" || v1.Message != "" {
		t.Errorf("Get lightweight = %+v, %v", v1, err)
	}

	// Creating returns the result of Get on the new name; the fake answers
	// for v2, so only the writes are checked here.
	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v2", Target: "dev", Message: "Release 2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Tags().Delete(ctx, "acme", "widget", "v1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /api/v3/repos/acme/widget/git/tags map[message:Release 2 object:c2 tag:v2 type:commit]",
		"POST /api/v3/repos/acme/widget/git/refs map[ref:refs/tags/v2 sha:t9]",
		"POST /api/v3/repos/acme/widget/git/refs map[ref:refs/tags/v1 sha:c1]",
		"DELETE /api/v3/repos/acme/widget/git/refs/tags/v1 map[]",
	}
	if fmt.Sprint(*writes) != fmt.Sprint(want) {
		t.Errorf("writes = %v\nwant     %v", *writes, want)
	}
}
