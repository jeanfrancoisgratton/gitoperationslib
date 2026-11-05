package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

const bitbucketRepo = `{"slug":"widget","name":"Widget","full_name":"acme/widget","is_private":true,"mainbranch":{"name":"main"}}`

func TestEditRepoBitbucket(t *testing.T) {
	var bodies []map[string]any
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/2.0/repositories/acme/widget" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		io.WriteString(w, bitbucketRepo)
	})
	ctx := context.Background()
	public, private := gitoperationslib.VisibilityPublic, gitoperationslib.VisibilityPrivate
	empty := gitoperationslib.Visibility("")

	repo, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{
		Name: gitoperationslib.Ptr("New"), Description: gitoperationslib.Ptr(""), Website: gitoperationslib.Ptr("https://w"),
		DefaultBranch: gitoperationslib.Ptr("dev"), Visibility: &public,
	})
	if err != nil || repo.DefaultBranch != "main" {
		t.Fatalf("Edit = %+v, %v", repo, err)
	}
	want := map[string]any{"name": "New", "description": "", "website": "https://w", "mainbranch": map[string]any{"name": "dev"}, "is_private": false}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Errorf("body = %v\nwant   %v", bodies[0], want)
	}
	if _, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{Visibility: &private}); err != nil || bodies[1]["is_private"] != true {
		t.Errorf("private: %v, body %v", err, bodies[1])
	}
	// An empty visibility means no change.
	if _, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{Visibility: &empty}); err != nil {
		t.Error(err)
	} else if _, ok := bodies[2]["is_private"]; ok {
		t.Errorf("empty visibility: body %v", bodies[2])
	}

	n := len(bodies)
	internal, bad := gitoperationslib.VisibilityInternal, gitoperationslib.Visibility("bad")
	for name, opts := range map[string]gitoperationslib.EditRepoOptions{
		"archiving":           {Archived: gitoperationslib.Ptr(true)},
		"template":            {Template: gitoperationslib.Ptr(true)},
		"internal visibility": {Visibility: &internal},
	} {
		if _, err := c.Repos().Edit(ctx, "acme", "widget", opts); !errors.Is(err, gitoperationslib.ErrNotSupported) {
			t.Errorf("%s: err = %v, want ErrNotSupported", name, err)
		}
	}
	if _, err := c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{Visibility: &bad}); err == nil || errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("invalid visibility: err = %v", err)
	}
	if len(bodies) != n {
		t.Errorf("refused calls sent %v", bodies[n:])
	}
}

func TestDeleteRepoBitbucket(t *testing.T) {
	var requests []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.Repos().Delete(context.Background(), "acme", "widget"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(requests) != "[DELETE /2.0/repositories/acme/widget]" {
		t.Errorf("requests = %v", requests)
	}
}

func TestListReposOneWorkspaceBitbucket(t *testing.T) {
	var queries []string
	c := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		io.WriteString(w, `{"values":[`+bitbucketRepo+`],"page":1,"next":"https://x/?page=2"}`)
	})
	ctx := context.Background()

	// With one workspace, an empty owner pages like naming it.
	page, err := c.Repos().List(ctx, "", gitoperationslib.ListOptions{PerPage: 10})
	if err != nil || len(page.Items) != 1 || page.NextPage != 2 || page.Items[0].Visibility != gitoperationslib.VisibilityPrivate {
		t.Fatalf("List = %+v, %v", page, err)
	}
	if queries[0] != "/2.0/repositories/acme?pagelen=10" {
		t.Errorf("request = %s", queries[0])
	}
	if _, err := c.Repos().List(ctx, "acme", gitoperationslib.ListOptions{}); err != nil {
		t.Error(err)
	}
}

// With several workspaces, an empty owner gathers them all in a single page.
func TestListReposSeveralWorkspacesBitbucket(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2.0/user/workspaces", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"values":[{"workspace":{"slug":"one"}},{"workspace":{"slug":"two"}}]}`)
	})
	for _, ws := range []string{"one", "two"} {
		mux.HandleFunc("GET /2.0/repositories/"+ws, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"values":[{"slug":"r","full_name":"%s/r"}]}`, ws)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(Token("tok"), WithBaseURL(srv.URL+"/2.0/"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	page, err := c.Repos().List(context.Background(), "", gitoperationslib.ListOptions{})
	if err != nil || len(page.Items) != 2 || page.NextPage != 0 {
		t.Errorf("List = %+v, %v", page, err)
	}
}
