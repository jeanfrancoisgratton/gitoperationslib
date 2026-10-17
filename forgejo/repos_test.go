package forgejo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fakeForgejo serves /version, /user (login "me") and /orgs/{name} (only
// "acme" exists); every other request goes to handler.
func fakeForgejo(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"version":"15.0.2+gitea-1.22.0"}`)
	})
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"login":"me"}`)
	})
	mux.HandleFunc("GET /api/v1/orgs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "acme" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"username":"acme"}`)
	})
	mux.HandleFunc("/", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(context.Background(), srv.URL, Token("pat"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const repoResponse = `{
	"id": 7, "owner": {"login": "acme"}, "name": "widget", "full_name": "acme/widget",
	"description": "d", "website": "https://w", "private": true, "internal": false,
	"default_branch": "main", "fork": false, "mirror": false, "archived": false,
	"template": true, "empty": false, "html_url": "https://h/acme/widget",
	"clone_url": "https://h/acme/widget.git", "ssh_url": "git@h:acme/widget.git",
	"created_at": "2026-10-17T10:00:00Z", "updated_at": "2026-10-17T11:00:00Z"
}`

func TestRepoConversion(t *testing.T) {
	var j repoJSON
	if err := json.Unmarshal([]byte(repoResponse), &j); err != nil {
		t.Fatal(err)
	}
	r := j.toRepo()
	if r.ID != 7 || r.Owner != "acme" || r.Name != "widget" || r.FullName != "acme/widget" ||
		r.Description != "d" || r.Website != "https://w" || r.Visibility != gitoperationslib.VisibilityPrivate ||
		r.DefaultBranch != "main" || !r.Template || r.Empty || r.CloneURL != "https://h/acme/widget.git" ||
		r.SSHURL != "git@h:acme/widget.git" || r.HTMLURL != "https://h/acme/widget" ||
		r.CreatedAt.Hour() != 10 || r.UpdatedAt.Hour() != 11 {
		t.Errorf("got %+v", r)
	}

	for _, tt := range []struct {
		private, internal bool
		want              gitoperationslib.Visibility
	}{
		{false, false, gitoperationslib.VisibilityPublic},
		{true, false, gitoperationslib.VisibilityPrivate},
		{false, true, gitoperationslib.VisibilityInternal},
	} {
		j := repoJSON{Private: tt.private, Internal: tt.internal}
		if got := j.toRepo().Visibility; got != tt.want {
			t.Errorf("private=%v internal=%v: %q, want %q", tt.private, tt.internal, got, tt.want)
		}
	}
}

func TestCreateRepoEndpoint(t *testing.T) {
	tests := []struct {
		owner, wantPath string
	}{
		{"", "/api/v1/user/repos"},
		{"me", "/api/v1/user/repos"},
		{"ME", "/api/v1/user/repos"},
		{"acme", "/api/v1/orgs/acme/repos"},
		{"someone", "/api/v1/admin/users/someone/repos"},
	}
	for _, tt := range tests {
		t.Run(tt.owner, func(t *testing.T) {
			var gotPath string
			c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.Method + " " + r.URL.Path
				w.WriteHeader(http.StatusCreated)
				io.WriteString(w, repoResponse)
			})
			if _, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{Owner: tt.owner, Name: "widget"}); err != nil {
				t.Fatal(err)
			}
			if gotPath != "POST "+tt.wantPath {
				t.Errorf("request = %q, want POST %s", gotPath, tt.wantPath)
			}
		})
	}
}

func TestCreateRepoBody(t *testing.T) {
	var got map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		got = nil
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, repoResponse)
	})
	_, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{
		Name: "widget", Description: "d", Visibility: gitoperationslib.VisibilityPrivate,
		AutoInit: true, DefaultBranch: "main", Gitignores: []string{"Go", "JetBrains"}, License: "MIT",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "widget", "description": "d", "private": true, "auto_init": true,
		"default_branch": "main", "gitignores": "Go,JetBrains", "license": "MIT",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("body = %v\nwant   %v", got, want)
	}

	// Empty visibility leaves the choice to the server.
	if _, err := c.Repos().Create(context.Background(), gitoperationslib.CreateRepoOptions{Name: "widget"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["private"]; ok || len(got) != 1 {
		t.Errorf("minimal body = %v, want only name", got)
	}
}

func TestCreateRepoRejected(t *testing.T) {
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s %s", r.Method, r.URL) })
	ctx := context.Background()

	_, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", Visibility: gitoperationslib.VisibilityInternal})
	if !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("internal visibility: err = %v", err)
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{Name: "x", Visibility: "secret"}); err == nil {
		t.Error("invalid visibility accepted")
	}
	if _, err := c.Repos().Create(ctx, gitoperationslib.CreateRepoOptions{}); err == nil {
		t.Error("empty name accepted")
	}
	vis := gitoperationslib.VisibilityInternal
	if _, err := c.Repos().Edit(ctx, "acme", "x", gitoperationslib.EditRepoOptions{Visibility: &vis}); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("edit internal visibility: err = %v", err)
	}
}

func TestGetEditDeleteRepo(t *testing.T) {
	var requests []string
	var editBody map[string]any
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		switch r.Method {
		case http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&editBody)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		}
		io.WriteString(w, repoResponse)
	})
	ctx := context.Background()

	repo, err := c.Repos().Get(ctx, "acme", "my widget")
	if err != nil || repo.Name != "widget" {
		t.Fatalf("Get = %+v, %v", repo, err)
	}
	vis := gitoperationslib.VisibilityPublic
	_, err = c.Repos().Edit(ctx, "acme", "widget", gitoperationslib.EditRepoOptions{
		Description: gitoperationslib.Ptr(""), Visibility: &vis, Archived: gitoperationslib.Ptr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(editBody) != fmt.Sprint(map[string]any{"description": "", "private": false, "archived": true}) {
		t.Errorf("edit body = %v", editBody)
	}
	if err := c.Repos().Delete(ctx, "acme", "widget"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /api/v1/repos/acme/my%20widget",
		"PATCH /api/v1/repos/acme/widget",
		"DELETE /api/v1/repos/acme/widget",
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Errorf("requests = %v\nwant       %v", requests, want)
	}
}

func TestGetRepoNotFound(t *testing.T) {
	c := fakeForgejo(t, http.NotFound)
	if _, err := c.Repos().Get(context.Background(), "acme", "nope"); !gitoperationslib.IsNotFound(err) {
		t.Errorf("err = %v, want 404", err)
	}
}

func TestListReposEndpoint(t *testing.T) {
	for owner, want := range map[string]string{
		"":        "/api/v1/user/repos",
		"acme":    "/api/v1/orgs/acme/repos",
		"someone": "/api/v1/users/someone/repos",
	} {
		var got string
		c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
			io.WriteString(w, "[]")
		})
		if _, err := c.Repos().List(context.Background(), owner, gitoperationslib.ListOptions{}); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("owner %q: path = %s, want %s", owner, got, want)
		}
	}
}

func TestListReposPaging(t *testing.T) {
	const total = 5
	c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page = max(page, 1)
		var items []string
		for id := (page-1)*limit + 1; id <= min(page*limit, total); id++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"r%d"}`, id, id))
		}
		if page*limit < total {
			w.Header().Set("Link", fmt.Sprintf(`<http://h%s?limit=%d&page=%d>; rel="next"`, r.URL.Path, limit, page+1))
		}
		fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
	})

	var ids []int64
	fetch := func(ctx context.Context, o gitoperationslib.ListOptions) (gitoperationslib.Page[gitoperationslib.Repo], error) {
		return c.Repos().List(ctx, "acme", o)
	}
	for repo, err := range gitoperationslib.All(context.Background(), gitoperationslib.ListOptions{PerPage: 2}, fetch) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, repo.ID)
	}
	if fmt.Sprint(ids) != "[1 2 3 4 5]" {
		t.Errorf("ids = %v", ids)
	}
}

func TestClientInterface(t *testing.T) {
	c := fakeForgejo(t, http.NotFound)
	if c.Backend() != gitoperationslib.BackendForgejo || !c.Capabilities().Has(gitoperationslib.CapRepos) {
		t.Errorf("Backend=%v Capabilities=%v", c.Backend(), c.Capabilities())
	}
}
