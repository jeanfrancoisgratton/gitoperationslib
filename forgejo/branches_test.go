package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestTagMessage(t *testing.T) {
	light := tagJSON{ID: "c1", Name: "v1", Message: "Initial commit"}
	light.Commit.SHA = "c1"
	annotated := tagJSON{ID: "t9", Name: "v2", Message: "Release 2"}
	annotated.Commit.SHA = "c1"
	if got := light.toTag(); got.Message != "" || got.SHA != "c1" {
		t.Errorf("lightweight = %+v (the commit message must not be reported)", got)
	}
	if got := annotated.toTag(); got.Message != "Release 2" || got.SHA != "c1" {
		t.Errorf("annotated = %+v", got)
	}
}

func TestBranchesAndTagsRequests(t *testing.T) {
	var requests []string
	var bodies []map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch {
		case r.Method == http.MethodDelete, r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/repos/acme/widget":
			io.WriteString(w, `{"name":"widget","default_branch":"main"}`)
		case r.URL.Path == "/api/v1/repos/acme/widget/branches" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"name":"main","commit":{"id":"c1"},"protected":true},{"name":"dev","commit":{"id":"c2"}}]`)
		case strings.Contains(r.URL.Path, "/branches"):
			io.WriteString(w, `{"name":"dev","commit":{"id":"c2"}}`)
		case r.URL.Path == "/api/v1/repos/acme/widget/tags" && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":"c1","name":"v1","message":"m","commit":{"sha":"c1"}}]`)
		default:
			io.WriteString(w, `{"id":"t9","name":"v2","message":"Release","commit":{"sha":"c1"}}`)
		}
	})
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	list, err := c.Branches().List(ctx, "acme", "widget", gitoperationslib.ListOptions{PerPage: 5})
	check(err)
	if len(list.Items) != 2 || !list.Items[0].IsDefault || !list.Items[0].Protected || list.Items[1].IsDefault || list.Items[1].SHA != "c2" {
		t.Errorf("List = %+v", list.Items)
	}
	got, err := c.Branches().Get(ctx, "acme", "widget", "dev")
	check(err)
	if got.Name != "dev" || got.IsDefault {
		t.Errorf("Get = %+v", got)
	}
	_, err = c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{Name: "dev", From: "v1"})
	check(err)
	_, err = c.Branches().Rename(ctx, "acme", "widget", "dev", "develop")
	check(err)
	check(c.Branches().Delete(ctx, "acme", "widget", "develop"))
	tags, err := c.Tags().List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	check(err)
	if len(tags.Items) != 1 || tags.Items[0].Message != "" {
		t.Errorf("tags = %+v", tags.Items)
	}
	tag, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{Name: "v2", Target: "dev", Message: "Release"})
	check(err)
	if tag.Message != "Release" {
		t.Errorf("created tag = %+v", tag)
	}
	_, err = c.Tags().Get(ctx, "acme", "widget", "v2")
	check(err)
	check(c.Tags().Delete(ctx, "acme", "widget", "v2"))

	wantRequests := []string{
		"GET /api/v1/repos/acme/widget?",
		"GET /api/v1/repos/acme/widget/branches?limit=5",
		"GET /api/v1/repos/acme/widget/branches/dev?",
		"GET /api/v1/repos/acme/widget?",
		"POST /api/v1/repos/acme/widget/branches?",
		"PATCH /api/v1/repos/acme/widget/branches/dev?",
		"GET /api/v1/repos/acme/widget/branches/develop?",
		"GET /api/v1/repos/acme/widget?",
		"DELETE /api/v1/repos/acme/widget/branches/develop?",
		"GET /api/v1/repos/acme/widget/tags?",
		"POST /api/v1/repos/acme/widget/tags?",
		"GET /api/v1/repos/acme/widget/tags/v2?",
		"DELETE /api/v1/repos/acme/widget/tags/v2?",
	}
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("requests = %v\nwant       %v", requests, wantRequests)
	}
	wantBodies := map[int]map[string]any{
		4:  {"new_branch_name": "dev", "old_ref_name": "v1"},
		5:  {"name": "develop"},
		10: {"tag_name": "v2", "target": "dev", "message": "Release"},
	}
	for i, want := range wantBodies {
		if fmt.Sprint(bodies[i]) != fmt.Sprint(want) {
			t.Errorf("%s body = %v\nwant %v", requests[i], bodies[i], want)
		}
	}
}

func TestBranchesAndTagsRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()
	if _, err := c.Branches().Create(ctx, "acme", "widget", gitoperationslib.CreateBranchOptions{}); err == nil {
		t.Error("empty branch name accepted")
	}
	if _, err := c.Branches().Rename(ctx, "acme", "widget", "dev", ""); err == nil {
		t.Error("empty new name accepted")
	}
	if _, err := c.Tags().Create(ctx, "acme", "widget", gitoperationslib.CreateTagOptions{}); err == nil {
		t.Error("empty tag name accepted")
	}
}

func TestBranchLastCommitAndCompare(t *testing.T) {
	var requests []string
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.EscapedPath())
		switch r.URL.EscapedPath() {
		case "/api/v1/repos/acme/widget":
			io.WriteString(w, `{"name":"widget","default_branch":"main"}`)
		case "/api/v1/repos/acme/widget/branches":
			io.WriteString(w, `[{"name":"main","commit":{"id":"c1","message":"Fix the build\n\nDetails","author":{"name":"Alice"},
				"timestamp":"2026-10-20T10:00:00-04:00"}},{"name":"bare","commit":{"id":"c2"}}]`)
		case "/api/v1/repos/acme/widget/compare/main...feature%2Fx":
			io.WriteString(w, `{"total_commits":3}`)
		case "/api/v1/repos/acme/widget/compare/feature%2Fx...main":
			io.WriteString(w, `{"total_commits":1}`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	list, err := c.Branches().List(ctx, "acme", "widget", gitoperationslib.ListOptions{})
	if err != nil || len(list.Items) != 2 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	lc := list.Items[0].LastCommit
	if lc == nil || lc.Author != "Alice" || lc.Message != "Fix the build\n\nDetails" || !lc.Date.Equal(time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("main's last commit = %+v", lc)
	}
	if list.Items[1].LastCommit != nil {
		t.Errorf("a commit without a date must leave LastCommit nil: %+v", list.Items[1].LastCommit)
	}

	// A branch name with a slash stays one path segment
	cmp, err := c.Branches().Compare(ctx, "acme", "widget", "main", "feature/x")
	if err != nil || *cmp != (gitoperationslib.Comparison{Base: "main", Head: "feature/x", Ahead: 3, Behind: 1}) {
		t.Fatalf("Compare = %+v, %v (requests %v)", cmp, err, requests)
	}
	if _, err := c.Branches().Compare(ctx, "acme", "widget", "main", "nope"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("unknown head: err = %v, want 404", err)
	}
	if _, err := c.Branches().Compare(ctx, "acme", "widget", "", "main"); err == nil {
		t.Error("empty base accepted")
	}
}
